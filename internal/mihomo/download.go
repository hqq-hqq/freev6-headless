package mihomo

import (
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/yourname/freev6/internal/stallhttp"
)

// mihomoVersion 是锁定下载的核心版本；各平台资产 URL 与 digest 必须与它
// 配对升级（digest 取自 GitHub release API 公布的资产 digest 字段）。
const mihomoVersion = "v1.19.31"

const (
	formatZip  = "zip"
	formatGzip = "gzip"
)

// mihomoAsset 描述当前平台要下载的内核资产：GitHub release 资产 URL、
// 官方公布的 SHA256（downloadRuntimeMirror 按它拒收篡改载荷）、解压后的
// 内核文件名（与 runtime.go RuntimeCandidates 的候选名保持一致）与压缩格式。
type mihomoAsset struct {
	archiveURL string
	sha256     string
	outputName string
	format     string
}

// currentRuntimeAsset 按运行平台选择要下载的资产。
//
// linux amd64 发行版按 x86-64 微架构等级分 v1/v2/v3：v3 需要 AVX2（2014 年
// 以后的桌面/笔记本 CPU 基本都有，与 Windows 版的 v3 选择一致）；老机器
// （或部分 NAS/虚拟机 CPU）可用 FREEV6_MIHOMO_AMD64_VARIANT=v2 降级。
func currentRuntimeAsset() (mihomoAsset, error) {
	base := "https://github.com/MetaCubeX/mihomo/releases/download/" + mihomoVersion
	switch {
	case runtime.GOOS == "windows" && runtime.GOARCH == "amd64":
		return mihomoAsset{
			archiveURL: base + "/mihomo-windows-amd64-v3-" + mihomoVersion + ".zip",
			sha256:     "f1a81678517dee871b51858f2a62d190d014b389057c67db575d1ddba892d19b",
			outputName: "mihomo-windows-amd64-v3.exe",
			format:     formatZip,
		}, nil
	case runtime.GOOS == "linux" && runtime.GOARCH == "amd64":
		variant := strings.ToLower(strings.TrimSpace(os.Getenv("FREEV6_MIHOMO_AMD64_VARIANT")))
		if variant == "" {
			variant = "v3"
		}
		switch variant {
		case "v3":
			return mihomoAsset{
				archiveURL: base + "/mihomo-linux-amd64-v3-" + mihomoVersion + ".gz",
				sha256:     "4e8808e79f1e452a0300ce1ee89fcaf2cccd5249a100f2238877214e5ca316b3",
				outputName: "mihomo-linux-amd64-v3",
				format:     formatGzip,
			}, nil
		case "v2":
			return mihomoAsset{
				archiveURL: base + "/mihomo-linux-amd64-v2-" + mihomoVersion + ".gz",
				sha256:     "560a14ba51482e85e90b6c9b141f3b7b1543795f9ecd82eb9a6c5153a1b7960b",
				outputName: "mihomo-linux-amd64-v2",
				format:     formatGzip,
			}, nil
		default:
			return mihomoAsset{}, fmt.Errorf("不支持的 FREEV6_MIHOMO_AMD64_VARIANT %q（可选 v2/v3）", variant)
		}
	case runtime.GOOS == "linux" && runtime.GOARCH == "arm64":
		return mihomoAsset{
			archiveURL: base + "/mihomo-linux-arm64-" + mihomoVersion + ".gz",
			sha256:     "9e0f11afbf38426b8bd88fdc594678f8161c57eccb4e1b77acb12b493904f1d4",
			outputName: "mihomo-linux-arm64",
			format:     formatGzip,
		}, nil
	default:
		return mihomoAsset{}, fmt.Errorf("mihomo 内核自动下载暂不支持 %s/%s：请手动放置 mihomo 到 runtime/ 目录（候选名见 runtime.go）", runtime.GOOS, runtime.GOARCH)
	}
}

// mustCurrentAsset 供包级变量初始化使用。不支持的平台不能在 init 阶段
// panic——CLI/helper 仍可能只用本地已放置的内核；下载真正被调用时
// DownloadLatestWithProgress 会通过 currentRuntimeAsset 报出可读错误。
func mustCurrentAsset() mihomoAsset {
	asset, err := currentRuntimeAsset()
	if err != nil {
		return mihomoAsset{sha256: "unavailable-on-this-platform"}
	}
	return asset
}

// expectedRuntimeSHA256 是 GitHub release API 对该资产公布的 digest。
// 多镜像意味着多个不受控分发方，下载的又是会被执行的文件——哈希必须
// 与固定值比对，不匹配按该线路失败处理（投毒/截断/换包都到此为止）。
// var 形式便于测试注入本地构造的载荷摘要。
var expectedRuntimeSHA256 = mustCurrentAsset().sha256

// buildMirrorList 按优先级排列：gh-proxy 家族是本项目核心/更新下载
// 的大陆加速通道（geodata.go 同源），按 2026-10 校园网实测 1MB 首段延迟
// 排序（v6/v4 最快，现役 gh-proxy.com 保留前三）；github 直连兜底给
// 国际可达的网络。
func buildMirrorList(archiveURL string) []string {
	return []string{
		"https://v6.gh-proxy.org/" + archiveURL,
		"https://v4.gh-proxy.org/" + archiveURL,
		"https://gh-proxy.com/" + archiveURL,
		"https://axisnow.gh-proxy.org/" + archiveURL,
		"https://cdn.gh-proxy.org/" + archiveURL,
		archiveURL,
	}
}

// runtimeDownloadURLs 保留包级变量形式，便于测试注入本地镜像链。
var runtimeDownloadURLs = buildMirrorList(mustCurrentAsset().archiveURL)

// 单镜像预算：停滞 10s 内无字节进展立即切线路（挂死的 gh-proxy 只烧掉
// 10 秒，慢而有进展的下载不被误杀）；硬上限兜住持续蠕动的镜像。总预算
// 仍由调用方 context 控制（helper 3 分钟），镜像间互不共享份额——这正是
// geodata 镜像链踩过的坑（共享预算被首镜像吃光）。
var (
	runtimeStallTimeout      = 10 * time.Second
	runtimeMirrorHardTimeout = 60 * time.Second
	// 巡检粒度；测试压缩时长时一并调小。
	runtimeStallCheckInterval = time.Second
)

const maxRuntimeDownload = 150 << 20

type DownloadResult struct {
	Version string
	Path    string
	SHA256  string
}

// progressSink 接收下载进度：received 为当前镜像已传输字节数，total 为响应
// Content-Length（未知时为 -1）。回调在下载 goroutine 内随数据流入触发，
// 实现必须线程安全且廉价（helper 用原子计数器承载）。切换镜像时 received
// 会归零重算，前端进度条可能回跳，属预期行为。
type progressSink func(received, total int64)

func DownloadLatest(ctx context.Context, root string) (DownloadResult, error) {
	return DownloadLatestWithProgress(ctx, root, nil)
}

// DownloadLatestWithProgress 与 DownloadLatest 相同，额外通过 progress
// 上报下载字节进度（progress 为 nil 时不报）。
func DownloadLatestWithProgress(ctx context.Context, root string, progress progressSink) (DownloadResult, error) {
	asset, assetErr := currentRuntimeAsset()
	if assetErr != nil {
		return DownloadResult{}, assetErr
	}
	client := &http.Client{}
	var failures []string
	skipped := 0
	for index, source := range runtimeDownloadURLs {
		if ctx.Err() != nil {
			skipped = len(runtimeDownloadURLs) - index
			break
		}
		digest, temporaryPath, downloadErr := downloadRuntimeMirror(ctx, client, source, progress)
		if downloadErr == nil {
			runtimeDir := filepath.Join(root, "runtime")
			if mkdirErr := os.MkdirAll(runtimeDir, 0o700); mkdirErr != nil {
				_ = os.Remove(temporaryPath)
				return DownloadResult{}, mkdirErr
			}
			outputPath, extractErr := extractRuntime(temporaryPath, runtimeDir, asset)
			_ = os.Remove(temporaryPath)
			if extractErr == nil {
				return DownloadResult{Version: mihomoVersion, Path: outputPath, SHA256: digest}, nil
			}
			// 包体已通过 SHA256 校验仍解不开 → 环境问题而非线路问题，
			// 但换线路重试无害且可能掩盖不了磁盘错误；按线路失败记录。
			downloadErr = extractErr
		} else {
			_ = os.Remove(temporaryPath)
		}
		failures = append(failures, fmt.Sprintf("%s: %v", hostOf(source), downloadErr))
	}
	if skipped > 0 {
		failures = append(failures, fmt.Sprintf("剩余 %d 个镜像因总超时未尝试", skipped))
	}
	if len(failures) == 0 && ctx.Err() != nil {
		return DownloadResult{}, fmt.Errorf("下载 mihomo runtime 超时: %w", ctx.Err())
	}
	return DownloadResult{}, fmt.Errorf("下载 mihomo runtime 失败（已尝试 %d/%d 个镜像: %s），请检查网络后重试",
		len(failures), len(runtimeDownloadURLs), strings.Join(failures, "; "))
}

// downloadRuntimeMirror 拉取单个镜像到临时文件并校验固定 SHA256。返回
// 摘要与临时文件路径（调用方负责删除）。停滞 watchdog 与硬上限都通过
// 取消 mirrorCtx 中断在途读取：连接挂死、首字节不来、传输中断流都会在
// runtimeStallTimeout 内让位给下一条线路。
func downloadRuntimeMirror(ctx context.Context, client *http.Client, source string, progress progressSink) (digest string, temporaryPath string, err error) {
	mirrorCtx, cancel := context.WithTimeout(ctx, runtimeMirrorHardTimeout)
	defer cancel()

	// watchdog 必须在 client.Do 之前启动：连接挂死/响应头不来的镜像会把
	// Do 本身阻塞住，等返回再巡检就只能靠硬上限兜底了。
	var received atomic.Int64
	stopWatchdog := stallhttp.StartWatchdog(mirrorCtx, cancel, &received, runtimeStallTimeout, runtimeStallCheckInterval)
	defer stopWatchdog()

	request, requestErr := http.NewRequestWithContext(mirrorCtx, http.MethodGet, source, nil)
	if requestErr != nil {
		return "", "", requestErr
	}
	request.Header.Set("User-Agent", "freev6")

	response, doErr := client.Do(request)
	if doErr != nil {
		return "", "", fmt.Errorf("download mihomo runtime: %w", doErr)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("mihomo runtime download returned HTTP %s", response.Status)
	}

	temporary, createErr := os.CreateTemp("", "freev6-mihomo-*")
	if createErr != nil {
		return "", "", createErr
	}
	// 捕获到局部变量：失败路径的显式 return 会把 named return 清空，
	// defer 若引用 named path 就删不掉临时文件。
	temporaryPath = temporary.Name()
	cleanupPath := temporaryPath
	keep := false
	defer func() {
		_ = temporary.Close()
		if !keep {
			_ = os.Remove(cleanupPath)
		}
	}()

	body := &stallhttp.CountingReader{Reader: response.Body, Counter: &received}
	hash := sha256.New()
	reader := io.Reader(io.TeeReader(body, hash))
	if progress != nil {
		reader = &progressReportingReader{r: reader, received: &received, total: response.ContentLength, onProgress: progress}
	}
	limited := io.LimitReader(reader, maxRuntimeDownload)
	if _, copyErr := io.Copy(temporary, limited); copyErr != nil {
		return "", "", fmt.Errorf("save mihomo runtime download: %w", copyErr)
	}
	if closeErr := temporary.Close(); closeErr != nil {
		return "", "", closeErr
	}
	digest = hex.EncodeToString(hash.Sum(nil))
	if digest != expectedRuntimeSHA256 {
		return "", "", fmt.Errorf("sha256 与固定值不符（got %s…），镜像内容不可信", digest[:16])
	}
	keep = true
	return digest, temporaryPath, nil
}

// progressReportingReader 在每次 Read 后上报当前字节进度。received 复用
// stall 计数器（CountingReader 已统计真正从网络读到的字节量），避免二次
// 计数与 watchdog 看到的字节数不一致。
type progressReportingReader struct {
	r          io.Reader
	received   *atomic.Int64
	total      int64
	onProgress progressSink
}

func (p *progressReportingReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if p.onProgress != nil && n > 0 {
		p.onProgress(p.received.Load(), p.total)
	}
	return n, err
}

func extractRuntime(archivePath, runtimeDir string, asset mihomoAsset) (string, error) {
	outputPath := filepath.Join(runtimeDir, asset.outputName)

	if asset.format == formatZip {
		archive, err := zip.OpenReader(archivePath)
		if err != nil {
			return "", fmt.Errorf("open mihomo archive: %w", err)
		}
		defer archive.Close()
		for _, file := range archive.File {
			lower := strings.ToLower(file.Name)
			if strings.HasSuffix(lower, ".exe") {
				input, err := file.Open()
				if err != nil {
					return "", err
				}
				output, err := os.OpenFile(outputPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o700)
				if err == nil {
					_, err = io.Copy(output, io.LimitReader(input, maxRuntimeDownload))
				}
				_ = input.Close()
				_ = output.Close()
				if err != nil {
					return "", fmt.Errorf("extract mihomo runtime: %w", err)
				}
				return outputPath, nil
			}
		}
		return "", fmt.Errorf("mihomo zip contains no executable")
	}

	input, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}
	defer input.Close()
	reader, err := gzip.NewReader(input)
	if err != nil {
		return "", fmt.Errorf("open mihomo gzip: %w", err)
	}
	defer reader.Close()
	output, err := os.OpenFile(outputPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o700)
	if err != nil {
		return "", err
	}
	_, copyErr := io.Copy(output, io.LimitReader(reader, maxRuntimeDownload))
	closeErr := output.Close()
	if copyErr != nil {
		return "", fmt.Errorf("extract mihomo gzip: %w", copyErr)
	}
	if closeErr != nil {
		return "", closeErr
	}
	return outputPath, nil
}
<div align="center">

<h1>FreeV6 Headless</h1>
<p><strong>教育网通用 IPv6 免流 · 无头 Linux 移植版</strong></p>
<p>CLI / deb / rpm · 双模式（TUN 全局接管 / 纯代理）· 为无桌面与 SSH 服务器而生</p>

<a href="https://github.com/hqq-hqq/freev6-headless/releases/latest"><img alt="Release" src="https://img.shields.io/github/v/release/hqq-hqq/freev6-headless?style=flat-square&include_prereleases"></a>
<a href="https://github.com/evansrrr/free-v6"><img alt="Upstream" src="https://img.shields.io/badge/upstream-evansrrr%2Ffree--v6-blue?style=flat-square"></a>
<a href="LICENSE"><img alt="License" src="https://img.shields.io/github/license/hqq-hqq/freev6-headless?style=flat-square"></a>

</div>

---

> [!IMPORTANT]
> 本仓库是 [evansrrr/free-v6](https://github.com/evansrrr/free-v6)（Windows 桌面版，MIT）的**非官方移植**。免流核心逻辑与上游一致：校园网（CERNET2）IPv6 流量不计费 → 用 mihomo 把 IPv4 流量封装进 28 个 Cloudflare WARP（MASQUE）IPv6 接入点出去。移植仅替换了 Windows 专属的内核下载、网络快照与权限路径，并自动化了直连白名单，详见[与上游的差异](#与上游的差异)。

## 特性

- **双模式随时切换**：`TUN 全局接管`（整机免流）↔ `纯代理`（零权限，给需要的程序配 `127.0.0.1:7890`）
- **权限极简**：TUN 模式只需要 `CAP_NET_ADMIN` 一项（`setcap` 或 systemd `AmbientCapabilities`），全程不需要以 root 运行免流组件
- **无头友好**：纯 CLI + systemd 服务 + SSH 隧道 Web 管理面板（等效上游"接入点"页的节点切换与测延迟）
- **内核自动下载**：GitHub 多镜像链 + 固定 SHA256 校验，支持 linux amd64 v3/v2（老 CPU 用 v2）与 arm64
- **发行版原生包**：deb / rpm（amd64 + arm64），装完一条 `freev6-setup` 完成全部配置
- **直连白名单自动化**：安装向导内置保留段基线（云元数据 / CGNAT / 组播 / 教育网 IPv6）+ 自动探测本机网卡段（`202.*` 校园公网段放大到 /16）+ `cpolar.com` 穿透域名直连——SSH、内网、穿透服务默认不受 TUN 影响

## 安装

从 [Releases](https://github.com/hqq-hqq/freev6-headless/releases/latest) 下载对应架构的包：

```bash
# Debian / Ubuntu / Deepin
sudo apt install ./freev6_0.1.0_amd64.deb          # arm64 用 freev6_0.1.0_arm64.deb

# RHEL / Fedora / openEuler / 麒麟
sudo dnf install ./freev6-0.1.0.x86_64.rpm         # arm64 用 freev6-0.1.0.aarch64.rpm

# 任意发行版：便携 tarball
tar xzf free-v6-linux-headless.tar.gz && cd free-v6-linux
sudo sh install.sh --service
```

装完跑一次配置向导（自动完成 WARP 注册、内核下载、配置渲染、setcap、systemd 启动）：

```bash
sudo freev6-setup --campus-cidr "10.0.0.0/8,192.168.0.0/16" --service   # TUN 模式（强烈建议带校园网段，防 SSH 断连）
sudo freev6-setup --no-tun                                              # 或纯代理零权限模式
```

## 使用

```bash
freev6 start / stop / status          # 手动模式（与 --service 二选一）
freev6 fetch-core                     # 单独下载/更新 mihomo 内核
freev6 render-config -no-tun ...      # 切换纯代理模式，去掉 -no-tun 切回 TUN
curl -x http://127.0.0.1:7890 https://www.cloudflare.com/cdn-cgi/trace   # 验证出口（warp=on 即生效）
```

远程管理面板（无桌面机器）：

```bash
ssh -L 9090:127.0.0.1:9090 user@host
# 浏览器打开 https://metacubexd.pages.dev → API http://127.0.0.1:9090
# secret 见 /var/lib/freev6/state/mihomo.yaml
```

完整使用说明（权限矩阵、模式切换、SSH 断连防护、故障排查）见 **[docs/linux-port.md](docs/linux-port.md)**。

## 与上游的差异

| 文件 | 改动 |
|---|---|
| `internal/mihomo/download.go` | 内核资产按 GOOS/GOARCH 选择（windows zip / linux gz），哈希表补 linux-amd64 v3/v2 与 linux-arm64（值取自 GitHub Release API digest 并实际下载复核） |
| `internal/mihomo/runtime.go` | 内核候选名去掉硬编码 `.exe`，Linux 识别 `mihomo-linux-<arch>[-v2|-v3]` |
| `internal/network/snapshot_other.go` / `restore_other.go` | 非 Windows 平台快照/还原降级 no-op（mihomo TUN 路由随进程生命周期自回收） |
| `internal/mihomo/config.go` | `RenderOptions.DisableTUN` → 渲染 `tun.enable: false` |
| `cmd/freev6/main.go` | `render-config`/`start` 增加 `-no-tun`（纯代理模式跳过管理员检查）；新增 `fetch-core` 子命令 |
| `scripts/linux/` | 安装/向导脚本：自动直连白名单（保留段基线 + 网卡段探测、`202.*` → /16、`cpolar.com` 直连）+ nfpm deb/rpm 打包配置 |

桌面版功能（Tauri GUI、ICS 热点共享、NSIS 自更新、开机自启计划任务）不在本仓库范围——上游原版请访问 [evansrrr/free-v6](https://github.com/evansrrr/free-v6) 与 [FreeV6 官网](https://freev6.cc.cd)。

## 免责声明

本软件仅供学习和研究使用，请勿用于任何非法用途。使用本软件产生的一切后果由用户自行承担。

- 开发者不对本软件适用性、稳定性作任何明示或暗示的保证
- 部分功能依赖第三方 API 与服务（Cloudflare），使用者须自行确保其使用符合相关法律法规及服务协议
- 用户应在遵守校园网及相关网络工具使用条款的前提下使用本软件，因违规使用导致的账号限制、设备隔离等后果自行承担

## 许可证

本项目与上游均采用 [MIT](LICENSE) 许可。上游桌面版：[evansrrr/free-v6](https://github.com/evansrrr/free-v6) · 官网：[freev6.cc.cd](https://freev6.cc.cd)

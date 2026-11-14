# FreeV6 无头 Linux 版（非官方移植）

基于 [evansrrr/free-v6](https://github.com/evansrrr/free-v6)（MIT）改造的**无桌面 / CLI** 发行：免流核心逻辑（Cloudflare WARP over MASQUE over IPv6 + mihomo TUN）原样保留，仅替换了 Windows 专属的内核下载、网络快照与系统权限路径。改动清单见文末。

> 上游免责声明同样适用：仅供学习研究，遵守校园网及 Cloudflare 服务条款，后果自负。

## 这是什么

校园网（CERNET2）IPv6 流量不计费 → mihomo 以 TUN（或纯代理）方式把 IPv4 流量封装进 28 个 Cloudflare WARP 的 IPv6 接入点出去，实现"免流"。

## 权限需求

| 操作 | 权限 |
|---|---|
| register / render-config / fetch-core | 无 |
| **纯代理模式**（`--no-tun`） | **零权限** |
| **TUN 全局接管** | 仅 `CAP_NET_ADMIN` 一项（setcap 或 systemd AmbientCapabilities，非整 root） |

## 安装方式

**方式 A — 发行版原生包（Debian/Ubuntu/RHEL 系）**

```bash
# Debian / Ubuntu / Deepin（amd64）
sudo apt install ./freev6_0.1.0_amd64.deb
# RHEL / Fedora / openEuler / 麒麟（x86_64）
sudo dnf install ./freev6-0.1.0.x86_64.rpm
# arm64 对应 freev6_0.1.0_arm64.deb / freev6-0.1.0.aarch64.rpm

# 装完跑配置向导（TUN 模式，建议带校园网段防 SSH 断连）
sudo freev6-setup --campus-cidr "10.0.0.0/8,192.168.0.0/16" --service
# 或纯代理零权限模式
sudo freev6-setup --no-tun

# 卸载
sudo apt remove freev6          # 或 sudo dnf remove freev6
sudo rm -rf /var/lib/freev6     # 数据目录（WARP 凭据与配置）
```

包内容：`/usr/bin/freev6`（CLI）、`/usr/bin/freev6-setup`（配置向导）、`/usr/share/freev6/`（地理数据）。mihomo 内核在向导运行时按平台下载并做 SHA256 校验。

> **直连白名单自动生成**：向导始终内置保留段基线（`127/8` 回环、`169.254/16` 云元数据、`100.64/10` CGNAT/Tailscale、`224/4` 组播、`2001:da8::/32` 教育网 IPv6——直连零损失）；未传 `--campus-cidr` 时再自动探测本机网卡段（私网段放大到整 /8|/12|/16，`202.*` 公网段——高校/CERNET 常见——放大到 /16，其余公网段取 /24，IPv6 全局段按 /64）；显式传参在保留段之上叠加。SSH 与内网访问默认安全。

**方式 B — 便携 tarball**（任何发行版）

```bash
# TUN 全局接管（推荐先加校园网段防 SSH 断连）
sudo FREEV6_CAMPUS_CIDR="10.0.0.0/8,192.168.0.0/16" sh install.sh --service

# 或纯代理模式：零权限、零风险，适合先验证节点可用
sh install.sh --no-tun
```

装完即自动完成：WARP 设备注册 → 下载 mihomo 内核（多镜像 + SHA256 校验）→ 渲染配置 →（TUN 模式）setcap →（`--service`）systemd 启动。

### 手动分步（不用脚本）

```bash
./freev6-linux-amd64 fetch-core -root .        # 下载内核到 runtime/
./freev6-linux-amd64 register -state state/warp.json
./freev6-linux-amd64 render-config -state state/warp.json -out state/mihomo.yaml
sudo setcap cap_net_admin,cap_net_bind_service=+ep runtime/mihomo-linux-amd64-v3
./freev6-linux-amd64 start && ./freev6-linux-amd64 status
```

## 两种模式与切换

| | TUN 全局接管 | 纯代理（`-no-tun`） |
|---|---|---|
| 免流范围 | 整机所有流量 | 仅走代理的程序 |
| 权限 | CAP_NET_ADMIN | 无 |
| 风险 | 路由接管，远程机器注意 SSH | 无 |

切换（改一行再重启进程即可，配置文件复用同一份状态）：

```bash
freev6 render-config -state state/warp.json -out state/mihomo.yaml -no-tun   # 切纯代理
freev6 render-config -state state/warp.json -out state/mihomo.yaml           # 切回 TUN
```

纯代理模式下给程序配 `127.0.0.1:7890`：

```bash
export https_proxy=http://127.0.0.1:7890 http_proxy=http://127.0.0.1:7890 all_proxy=socks5://127.0.0.1:7890
```

## 远程管理（无桌面）

```bash
ssh -L 9090:127.0.0.1:9090 user@host
# 浏览器打开 https://metacubexd.pages.dev
# API 填 http://127.0.0.1:9090，secret 填 state/mihomo.yaml 里的 secret 字段
```

面板里可切节点、测延迟（即上游"接入点"页的等效功能）。

## 验证免流是否生效

```bash
curl -x http://127.0.0.1:7890 https://www.cloudflare.com/cdn-cgi/trace
# 输出 warp=on 表示出口已走 WARP；在校网流量监测里看 IPv4 计费是否不再增长
```

## SSH 断连防护（TUN 模式必读）

远程机器开 TUN 后路由被接管，若你的 SSH 客户端 IP 没进直连规则，会话可能被切断：

1. 未传 `--campus-cidr` 时安装向导**自动探测本机网段并加入直连白名单**（默认防护，`202.*` 校园公网段自动放大到 /16）；SSH 来源超出 /16 等更大范围时，显式传 `--campus-cidr` 补充；
2. `--service` 配置了 `Restart=on-failure`，mihomo 崩溃 5 秒内自动拉起；
3. 最坏情况走物理控制台/IPMI `systemctl stop freev6`。

## 其他

- **老 CPU（无 AVX2）**：`FREEV6_MIHOMO_AMD64_VARIANT=v2 freev6 fetch-core -root .`
- **日志**：手动模式看 `state/mihomo.log`；服务模式 `journalctl -u freev6 -f`
- **卸载**：`sudo systemctl disable --now freev6 && sudo rm -rf /opt/freev6 /etc/systemd/system/freev6.service`
- **systemd 服务与 `freev6 start/stop` 二选一**，不要同时跑（端口冲突）
- Windows 版桌面程序（Tauri GUI、热点共享、开机自启等）不在本发行范围

## 相对上游的改动（均为 CLI 兼容扩展，Windows 行为不变）

| 文件 | 改动 |
|---|---|
| `internal/mihomo/download.go` | 内核资产按 GOOS/GOARCH 选择（windows zip / linux gz），哈希表补 linux-amd64 v3/v2 与 linux-arm64（值取自 GitHub Release API digest 并实际下载复核）；解压产物名随平台 |
| `internal/mihomo/runtime.go` | 内核候选名去掉硬编码 `.exe`，Linux 识别 `mihomo-linux-<arch>[-v2|-v3]` |
| `internal/network/snapshot_other.go` / `restore_other.go` | 非 Windows 平台快照/还原降级为 no-op（mihomo TUN 路由随进程生命周期自回收） |
| `internal/mihomo/config.go` | `RenderOptions.DisableTUN` → 渲染 `tun.enable: false` |
| `cmd/freev6/main.go` | `render-config`/`start` 增加 `-no-tun`；新增 `fetch-core` 子命令 |

交叉编译：`GOOS=linux GOARCH=amd64 go build -trimpath -ldflags '-s -w' ./cmd/freev6`（Go ≥ 1.25）。

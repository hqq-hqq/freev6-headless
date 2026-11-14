#!/bin/sh
# FreeV6 无头 Linux 安装脚本（非官方移植，基于 evansrrr/free-v6，MIT）
#
# 用法:
#   sudo sh install.sh                                # TUN 全局接管（默认）
#   sh install.sh --no-tun                            # 纯代理模式（零权限）
#   sudo sh install.sh --service                      # 追加注册 systemd 开机自启
#   sudo sh install.sh --campus-cidr "10.0.0.0/8"     # 校园网段直连（防 SSH 断连）
#   FREEV6_PREFIX=$HOME/.local/share/freev6 sh install.sh --no-tun  # 装到家目录
#
# 环境变量: FREEV6_PREFIX（安装根目录，默认 /opt/freev6）、
#           FREEV6_CAMPUS_CIDR（默认校园网直连段，逗号分隔）

set -e

PREFIX="${FREEV6_PREFIX:-/opt/freev6}"
CAMPUS_CIDR="${FREEV6_CAMPUS_CIDR:-}"
NO_TUN=0
SERVICE=0

while [ $# -gt 0 ]; do
  case "$1" in
    --prefix) PREFIX="$2"; shift 2 ;;
    --campus-cidr) CAMPUS_CIDR="$2"; shift 2 ;;
    --no-tun) NO_TUN=1; shift ;;
    --service) SERVICE=1; shift ;;
    -h|--help) sed -n '3,11p' "$0"; exit 0 ;;
    *) echo "未知参数: $1"; exit 1 ;;
  esac
done

ARCH="$(uname -m)"
case "$ARCH" in
  x86_64)  GOARCH=amd64 ;;
  aarch64) GOARCH=arm64 ;;
  *) echo "不支持的架构: $ARCH（仅支持 x86_64 / aarch64）"; exit 1 ;;
esac

BINDIR="$PREFIX/bin"
RUNTIME="$PREFIX/runtime"
STATE="$PREFIX/state"
SRC="$(cd "$(dirname "$0")" && pwd)"

echo "==> 安装目录: $PREFIX"
mkdir -p "$BINDIR" "$RUNTIME" "$STATE"

echo "==> 安装 CLI..."
install -m 0755 "$SRC/freev6-linux-$GOARCH" "$BINDIR/freev6"

echo "==> 预置地理数据种子..."
for f in GeoSite.dat geoip.dat country.mmdb; do
  if [ -f "$SRC/$f" ]; then
    install -m 0644 "$SRC/$f" "$PREFIX/$f"
  fi
done

echo "==> 准备 mihomo 内核..."
if ls "$RUNTIME"/mihomo-linux-* >/dev/null 2>&1; then
  echo "    已存在，跳过"
elif ls "$SRC"/runtime/mihomo-linux-"$GOARCH"* >/dev/null 2>&1; then
  install -m 0755 "$SRC"/runtime/mihomo-linux-"$GOARCH"* "$RUNTIME/"
else
  echo "    从 GitHub 多镜像下载（约 22MB，带 SHA256 校验）..."
  "$BINDIR/freev6" fetch-core -root "$PREFIX"
fi

if [ ! -s "$STATE/warp.json" ]; then
  echo "==> 注册 Cloudflare WARP 设备..."
  "$BINDIR/freev6" register -state "$STATE/warp.json" -name freev6-linux
else
  echo "==> WARP 设备已注册，跳过"
fi

# ===== 直连白名单：SSH 防断连 + 保留段防误拦截 =====
# 白名单由三部分合成：保留段基线（始终包含）→ 用户显式 --campus-cidr →
# 自动探测（未传参时）。TUN 接管路由后，SSH 进本机的来源网段必须走
# DIRECT，否则会话会被隧道接管而断连。
# 保留段直连无免流损失（均为不可路由公网的保留空间或免费教育网 IPv6）：
#   127/8 回环；169.254/16 云元数据/链路本地（云 agent 拿凭据用）；
#   100.64/10 CGNAT（Tailscale 等）；224/4 组播（mDNS/SSDP/打印发现）；
#   2001:da8::/32 CERNET2 教育网 IPv6（直连本来免费且更快）。
RESERVED="127.0.0.0/8,169.254.0.0/16,100.64.0.0/10,224.0.0.0/4,2001:da8::/32"
DETECTED=""
if [ -z "$CAMPUS_CIDR" ]; then
  for cidr in $(ip -o -4 addr show scope global 2>/dev/null | awk '{print $4}'); do
    net="${cidr%/*}"
    case "$net" in
      10.*) DETECTED="$DETECTED 10.0.0.0/8" ;;
      172.1[6-9].*|172.2[0-9].*|172.3[01].*) DETECTED="$DETECTED 172.16.0.0/12" ;;
      192.168.*) DETECTED="$DETECTED 192.168.0.0/16" ;;
      202.*)
        # 202/8 集中了大量高校/CERNET 公网段（如 202.204.x）：放大到 /16
        # （校级网块），同校跨楼宇的 SSH 来源与校内服务都能直连。
        o1="${net%%.*}"; _rest="${net#*.}"; o2="${_rest%%.*}"
        DETECTED="$DETECTED $o1.$o2.0.0/16" ;;
      *) DETECTED="$DETECTED $net/24" ;;
    esac
  done
  for cidr in $(ip -o -6 addr show scope global 2>/dev/null | awk '{print $4}'); do
    DETECTED="$DETECTED $cidr"
  done
  CAMPUS_CIDR=$(echo $DETECTED | tr ' ' '\n' | sort -u | paste -sd, -)
  if [ -n "$CAMPUS_CIDR" ]; then
    echo "==> 自动探测网卡段: $CAMPUS_CIDR"
  else
    echo "!! 未探测到活动网卡网段，仅使用保留段白名单"
  fi
fi
CAMPUS_CIDR="$RESERVED,$CAMPUS_CIDR"
echo "==> 直连白名单（保留段 + 网卡段）: $CAMPUS_CIDR"
echo "    以上网段将 DIRECT 直连，SSH 与内网访问不受 TUN 影响"

echo "==> 渲染 mihomo 配置..."
RENDER_ARGS="render-config -state $STATE/warp.json -out $STATE/mihomo.yaml"
if [ -n "$CAMPUS_CIDR" ]; then
  RENDER_ARGS="$RENDER_ARGS -campus-cidr $CAMPUS_CIDR"
fi
if [ "$NO_TUN" = 1 ]; then
  RENDER_ARGS="$RENDER_ARGS -no-tun"
fi
"$BINDIR/freev6" $RENDER_ARGS

if [ "$NO_TUN" = 0 ]; then
  if [ "$(id -u)" = 0 ] && command -v setcap >/dev/null 2>&1; then
    setcap cap_net_admin,cap_net_bind_service=+ep "$RUNTIME"/mihomo-linux-*
    echo "==> 已授予 mihomo CAP_NET_ADMIN（setcap，普通用户即可拉起 TUN）"
  else
    echo "!! 未以 root 运行或缺少 setcap：TUN 模式需要以下任一授权方式"
    echo "   a) sudo setcap cap_net_admin,cap_net_bind_service=+ep $RUNTIME/mihomo-linux-*"
    echo "   b) 使用 --service（systemd AmbientCapabilities 自动配置）"
    echo "   c) sudo 运行 freev6 start"
  fi
fi

if [ "$SERVICE" = 1 ]; then
  if [ "$(id -u)" != 0 ]; then
    echo "!! --service 需要 root"; exit 1
  fi
  BIN="$(ls "$RUNTIME"/mihomo-linux-* | head -n 1)"
  {
    echo "[Unit]"
    echo "Description=FreeV6 mihomo core (CERNET2 IPv6 tunnel)"
    echo "After=network-online.target"
    echo "Wants=network-online.target"
    echo ""
    echo "[Service]"
    echo "Type=simple"
    echo "ExecStart=$BIN -d $STATE -f $STATE/mihomo.yaml"
    if [ "$NO_TUN" = 1 ]; then
      echo "# 纯代理模式：无需任何特权"
    else
      echo "AmbientCapabilities=CAP_NET_ADMIN"
      echo "CapabilityBoundingSet=CAP_NET_ADMIN"
      echo "NoNewPrivileges=true"
    fi
    echo "Restart=on-failure"
    echo "RestartSec=5"
    echo "LimitNOFILE=65535"
    echo ""
    echo "[Install]"
    echo "WantedBy=multi-user.target"
  } > /etc/systemd/system/freev6.service
  systemctl daemon-reload
  systemctl enable --now freev6.service
  echo "==> 服务已启动：systemctl status freev6 / journalctl -u freev6 -f"
fi

cat <<EOF

──────────────────────────────────────────────────────
安装完成（模式: $( [ "$NO_TUN" = 1 ] && echo 纯代理 || echo TUN全局 )）
  配置:     $STATE/mihomo.yaml
  代理端口:  127.0.0.1:7890 (HTTP/SOCKS)
  控制台:    127.0.0.1:9090（secret 见配置文件 secret 字段）

手动模式（未用 --service 时）:
  $BINDIR/freev6 start / stop / status
注意: systemd 服务与 freev6 start/stop 二选一，不要同时使用。

切换 TUN <-> 纯代理:
  $BINDIR/freev6 render-config -state $STATE/warp.json -out $STATE/mihomo.yaml -no-tun
  （去掉 -no-tun 即切回 TUN；随后重启服务或重新 start）

远程管理面板:
  ssh -L 9090:127.0.0.1:9090 user@host
  浏览器打开 https://metacubexd.pages.dev → API http://127.0.0.1:9090
  secret 填 $STATE/mihomo.yaml 里的 secret 字段值

验证出口:
  curl -x http://127.0.0.1:7890 https://www.cloudflare.com/cdn-cgi/trace
──────────────────────────────────────────────────────
EOF

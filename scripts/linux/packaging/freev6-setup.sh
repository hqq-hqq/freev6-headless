#!/bin/sh
# freev6-setup — FreeV6 deb/rpm 安装后的配置向导（无头 Linux，非官方移植）
#
# 用法:
#   sudo freev6-setup                          # TUN 全局接管（默认）
#   sudo freev6-setup --no-tun                 # 纯代理模式（零权限）
#   sudo freev6-setup --campus-cidr "10.0.0.0/8,192.168.0.0/16"   # 校园网段直连（防 SSH 断连）
#   sudo freev6-setup --service                # 追加注册 systemd 开机自启
#   FREEV6_ROOT=$HOME/.local/share/freev6 freev6-setup --no-tun   # 免 root 装到家目录
#
# 数据目录: /var/lib/freev6（state/ 配置与凭据、runtime/ mihomo 内核）
# 程序文件: /usr/bin/freev6（CLI）、/usr/share/freev6/（地理数据）

set -e

ROOT="${FREEV6_ROOT:-/var/lib/freev6}"
SHARE=/usr/share/freev6
NO_TUN=0
SERVICE=0
CAMPUS_CIDR=""

while [ $# -gt 0 ]; do
  case "$1" in
    --no-tun) NO_TUN=1; shift ;;
    --service) SERVICE=1; shift ;;
    --campus-cidr) CAMPUS_CIDR="$2"; shift 2 ;;
    -h|--help) sed -n '3,12p' "$0"; exit 0 ;;
    *) echo "未知参数: $1"; exit 1 ;;
  esac
done

STATE="$ROOT/state"
RUNTIME="$ROOT/runtime"
mkdir -p "$STATE" "$RUNTIME"

echo "==> 数据目录: $ROOT"

for f in GeoSite.dat geoip.dat country.mmdb; do
  if [ -f "$SHARE/$f" ]; then
    cp -f "$SHARE/$f" "$ROOT/$f"
  fi
done

echo "==> 准备 mihomo 内核..."
if ls "$RUNTIME"/mihomo-linux-* >/dev/null 2>&1; then
  echo "    已存在，跳过"
else
  echo "    从 GitHub 多镜像下载（约 22MB，带 SHA256 校验）..."
  /usr/bin/freev6 fetch-core -root "$ROOT"
fi

if [ ! -s "$STATE/warp.json" ]; then
  echo "==> 注册 Cloudflare WARP 设备..."
  /usr/bin/freev6 register -state "$STATE/warp.json" -name freev6-linux
else
  echo "==> WARP 设备已注册，跳过"
fi

echo "==> 渲染 mihomo 配置..."
RENDER_ARGS="render-config -state $STATE/warp.json -out $STATE/mihomo.yaml"
if [ -n "$CAMPUS_CIDR" ]; then
  RENDER_ARGS="$RENDER_ARGS -campus-cidr $CAMPUS_CIDR"
fi
if [ "$NO_TUN" = 1 ]; then
  RENDER_ARGS="$RENDER_ARGS -no-tun"
fi
/usr/bin/freev6 $RENDER_ARGS

if [ "$NO_TUN" = 0 ]; then
  if [ "$(id -u)" = 0 ] && command -v setcap >/dev/null 2>&1; then
    setcap cap_net_admin,cap_net_bind_service=+ep "$RUNTIME"/mihomo-linux-*
    echo "==> 已授予 mihomo CAP_NET_ADMIN（setcap）"
  else
    echo "!! TUN 模式需要以下任一授权方式："
    echo "   a) sudo setcap cap_net_admin,cap_net_bind_service=+ep $RUNTIME/mihomo-linux-*"
    echo "   b) 使用 --service（systemd AmbientCapabilities 自动配置）"
    echo "   c) sudo /usr/bin/freev6 start"
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
配置完成（模式: $( [ "$NO_TUN" = 1 ] && echo 纯代理 || echo TUN全局 )）
  配置:     $STATE/mihomo.yaml
  代理端口:  127.0.0.1:7890 (HTTP/SOCKS)
  控制台:    127.0.0.1:9090（secret 见配置文件 secret 字段）

手动模式（未用 --service 时）:
  /usr/bin/freev6 start / stop / status

切换 TUN <-> 纯代理:
  /usr/bin/freev6 render-config -state $STATE/warp.json -out $STATE/mihomo.yaml -no-tun
  （去掉 -no-tun 即切回 TUN；随后重启服务或重新 start）

远程管理面板:
  ssh -L 9090:127.0.0.1:9090 user@host
  浏览器打开 https://metacubexd.pages.dev → API http://127.0.0.1:9090

验证出口:
  curl -x http://127.0.0.1:7890 https://www.cloudflare.com/cdn-cgi/trace
──────────────────────────────────────────────────────
EOF

#!/bin/bash
# 先启动 Clash/代理，再运行此脚本打开 Cursor

PROXY_PORT="${CURSOR_PROXY_PORT:-7897}"
PROXY="http://127.0.0.1:${PROXY_PORT}"

if ! nc -z 127.0.0.1 "$PROXY_PORT" 2>/dev/null; then
  echo "错误: 127.0.0.1:${PROXY_PORT} 未监听，请先启动代理并确认 Mixed/HTTP 端口。"
  echo "Clash Verge 一般在 设置 → 端口 里查看 Mixed Port。"
  exit 1
fi

export HTTP_PROXY="$PROXY"
export HTTPS_PROXY="$PROXY"
export ALL_PROXY="socks5://127.0.0.1:7890"
export NO_PROXY="localhost,127.0.0.1"

echo "代理端口 ${PROXY_PORT} 正常，正在启动 Cursor..."
open -a Cursor

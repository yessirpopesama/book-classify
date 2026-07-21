#!/bin/bash
# 同时启动前端和后端服务

cd "$(dirname "$0")"

# 检查 config.yaml
if [ ! -f config.yaml ]; then
  echo "错误: 请先创建 config.yaml 并配置 deepseek_api_key"
  echo "可复制 config.yaml.example 并修改"
  exit 1
fi

# 释放被占用的端口
kill_port() {
  local port=$1
  local pids
  pids=$(lsof -ti :"$port" 2>/dev/null)
  if [ -n "$pids" ]; then
    echo "端口 $port 被占用 (PID: $pids)，正在释放..."
    echo "$pids" | xargs kill -9 2>/dev/null
    sleep 1
  fi
}

kill_port 8080
kill_port 8887

# 启动后端
echo "启动后端服务 (端口 8080)..."
go run ./server &
BACKEND_PID=$!

# 等待后端启动
sleep 2

# 检查前端依赖
if [ ! -d frontend/node_modules ]; then
  echo "安装前端依赖..."
  (cd frontend && npm install)
fi

# 启动前端
echo "启动前端服务 (端口 8887)..."
(cd frontend && npm run dev) &
FRONTEND_PID=$!

echo ""
echo "=========================================="
echo "  图书工具服务已启动"
echo "  前端: http://localhost:8887"
echo "  后端: http://localhost:8080"
echo "=========================================="
echo "按 Ctrl+C 停止所有服务"
echo ""

# 捕获退出信号
trap "kill $BACKEND_PID $FRONTEND_PID 2>/dev/null; exit" INT TERM

wait

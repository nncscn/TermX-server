#!/usr/bin/env bash
# 密钥管家 · 单文件打包脚本
# 产物：backend/termx-server（前端页面内嵌，无需 Node/浏览器环境即可运行）
# 用法：./build.sh [目标平台]，如 ./build.sh 或 GOOS=linux GOARCH=amd64 ./build.sh
set -euo pipefail
cd "$(dirname "$0")"

echo "==> [1/3] 构建前端..."
(cd frontend && npm run build)

echo "==> [2/3] 拷贝产物到内嵌目录..."
rm -rf backend/internal/web/dist
cp -r frontend/dist backend/internal/web/dist

echo "==> [3/3] 编译后端（CGO 关闭，静态单文件）..."
(cd backend && CGO_ENABLED=0 go build -o termx-server ./cmd/server)

echo "==> 打包完成：backend/termx-server（$(du -h backend/termx-server | cut -f1)）"
echo "    运行：把 termx-server 放到任意目录直接执行，浏览器访问启动日志提示的地址"

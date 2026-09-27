# TermX

自托管的 SSH 凭据管理服务，配合 TermX 桌面客户端使用。

## 功能

- **凭据管理** — 服务器地址、账号、密码、SSH 私钥的集中存储
- **客户端加密** — 浏览器端 AES-256-GCM 加密后入库，服务端不解开
- **TermX 桌面客户端同步** — 桌面端与 Web 端凭据双向同步
- **多数据库支持** — SQLite / MySQL / PostgreSQL / SQL Server，安装引导自动建库迁移
- **单文件部署** — 前端页面内嵌到 Go 二进制，一个可执行文件跑起来
- **安装引导** — 浏览器五步向导完成初始化，自动转入后台运行

## 快速开始

```bash
# 构建（需要 Go 1.21+ 和 Node.js 18+）
./build.sh

# 运行
./termx-server
```

启动后终端会显示访问地址（默认 `http://<本机IP>:18080`），浏览器打开即可进入安装引导。

## 项目结构

```
├── backend/           Go 后端
│   ├── cmd/server/    入口（启动、CLI 命令）
│   ├── internal/
│   │   ├── handler/   HTTP 处理层
│   │   ├── service/   业务逻辑
│   │   ├── repository/ 数据访问
│   │   ├── model/     数据模型
│   │   ├── middleware/ 鉴权、限流、CORS
│   │   ├── pkg/       工具包（加密、控制台输出、重启）
│   │   └── web/       内嵌前端静态资源
│   └── go.mod
├── frontend/          Vue 3 前端
│   └── src/
│       ├── views/     页面组件
│       ├── api/       接口封装
│       └── utils/     工具（加密、格式化）
├── build.sh           构建脚本
├── LICENSE            开源许可证
└── docs/              文档
```

## 技术栈

| 层 | 技术 |
|---|---|
| 后端 | Go 1.21+ / Gin / GORM |
| 前端 | Vue 3 / Element Plus / Vite |
| 数据库 | SQLite（默认）/ MySQL / PostgreSQL / SQL Server |
| 加密 | AES-256-GCM / PBKDF2 / HMAC-SHA256 |

## 命令

```bash
./termx-server              # 启动（安装完成后自动转后台）
./termx-server -f           # 前台运行（调试用）
./termx-server --stop       # 停止后台服务
./termx-server --show-key   # 查看 TermX 接入密钥
./termx-server --uninstall  # 卸载向导
```

## 许可证

本项目采用 [TermX 开源许可证](LICENSE)，仅供学习与技术交流。二次开发须获作者书面授权，禁止商业使用。

## TermX 桌面客户端

桌面客户端是独立项目，通过同步接口与本服务端对接：
- 服务器地址填 `http://<服务端IP>:<端口>/api`
- 接入密钥在服务端执行 `./termx-server --show-key` 查看

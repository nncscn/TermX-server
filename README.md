# TermX

**中文** | [English](#english)

自托管的 SSH 凭据管理服务，配合 TermX 桌面客户端使用。

服务器地址、账号、密码、SSH 私钥集中保管——**数据在浏览器里端到端加密后才入库，服务端也看不到明文**。整个服务编译成一个可执行文件，上传到 Linux 服务器即可运行，浏览器完成五步安装引导后自动转入后台。

## 功能特性

- **单文件部署** — 前端页面通过 `go:embed` 嵌入二进制（约 48M），运行零依赖：不需要安装数据库、Node 或运行时
- **端到端加密** — 凭据连接信息（主机/账号/密码/私钥）在浏览器端 AES-256-GCM 加密后才发往服务端；服务端只存密文，不持有解密能力
- **五步安装引导** — 欢迎与许可证 → 数据库 → 服务设置 → 安全设置 → 初始化，全程浏览器完成，自动建库建表
- **多数据库** — SQLite 开箱即用；MySQL / PostgreSQL / SQL Server 自动建库，运行期可互切并自动迁移数据
- **TermX 桌面客户端同步** — 桌面端与 Web 端凭据互通，客户端侧同样加密
- **回收站 / 自动锁定 / 剪贴板自动清除** — 删除可恢复，空闲可锁定，复制敏感信息后可定时清空剪贴板
- **内置安全机制** — 登录失败递增冷却（5 次 60 秒 / 10 次 5 分钟 / 15 次 30 分钟）、接口限流、请求体大小限制、安全响应头、安装接口用后即毁

## 代码亮点

**1. 零知识加密架构**
加密全部发生在浏览器：`frontend/src/utils/crypto.js` 内置一套纯 JS 实现的 AES-256-GCM 与 SHA-256（不依赖 WebCrypto，因为 HTTP 非安全上下文里 `crypto.subtle` 不可用），启动时用 NIST 官方测试向量自检。服务端登录走 PBKDF2 的 AuthHash 比对；用户不存在时也执行一次同代价的 dummy PBKDF2，抹平时序差异防账户枚举（`internal/service/auth.go`）。

**2. 安装引导的原子化与"用后即毁"**
`internal/service/setup.go` 把完成动作串成不可分割的链：验证 → 建库 → AutoMigrate → 创建账户 → 写配置 → 原地重启。任一环失败都不会留下半初始化状态。安装完成后，`setup/test-connection`、`setup/complete` 等路由从路由表整个摘除（`internal/router/router.go`），彻底堵死"重装接管"攻击面。

**3. 自愈能力**
配置永远双写 `data/config.yaml` + `data/config.yaml.bak`。HTTPS 证书加载失败或数据库切换失败时，程序自动用备份回滚配置并通过 `syscall.Exec` 原地重启（`internal/pkg/restart/restart.go`），不会把管理员锁在门外。

**4. 自后台化与单实例守护**
终端里直接启动时自动 ForkExec + Setsid 转入后台，PID 写入 `data/termx-server.pid`；再次启动会检测 pidfile 拒绝双开。配置端口被占时自动从 10000 以上寻找空闲端口并写回配置（`internal/pkg/portprobe/`）。

**5. 数据库无关层**
GORM 模型不硬编码字段类型，让各方言（SQLite/MySQL/PostgreSQL/SQL Server）自适应映射。运行期切库时密文原样搬运、账户与登录态保持；目标库撞名自动避让并弹窗告知；外部数据库主机强制私网地址（`internal/database/`）。

**6. TermX 同步协议**
桌面客户端走 `X-Server-Key`（服务器接入密钥）+ `X-Vault-Token`（保险库会话令牌）双令牌鉴权，独立限流；两种密文格式在服务端透明翻译，Web 端与客户端互不干扰（`internal/handler/termx.go`、`internal/pkg/termxcrypt/`）。

**7. HTTP 层加固**
10MB `MaxBytesReader` 请求体上限、`X-Frame-Options: DENY` / `nosniff` / `no-referrer` / API 禁缓存等安全头、按 IP 固定窗口限流并周期清理、引导接口同源绑定（`internal/middleware/`）。敏感文件（配置、密钥、日志）统一 0600 权限，TermX 接入密钥加密落盘。

## 项目结构

后端是清晰的四层架构：**router → handler → service → repository**，依赖单向向下；`cmd/` 只做接线和 CLI，`internal/pkg/` 放与业务无关的工具包。

```
backend/
├── cmd/server/                 # 入口与 CLI
│   ├── main.go                 #   启动流程：配置→端口守卫→建库→路由→后台化→HTTP(S)
│   ├── stop.go                 #   --stop：pidfile + /proc 校验，SIGTERM→SIGKILL
│   ├── showkey.go              #   --show-key：查看 TermX 接入密钥
│   └── uninstall.go            #   --uninstall：交互式一键卸载向导
├── internal/
│   ├── config/config.go        # 配置加载/保存/备份自愈（data/config.yaml）
│   ├── database/               # 多引擎连接探测、自动建库、迁移、回滚
│   ├── handler/                # HTTP 处理层（auth/setup/credential/sshkey/trash/termx/…）
│   ├── middleware/             # 鉴权、限流、安全头、跨域、TermX 令牌
│   ├── model/                  # GORM 模型（Account/Credential/SshKey/…）
│   ├── pkg/
│   │   ├── console/            #   终端横幅、彩色输出、密钥展示框
│   │   ├── restart/            #   原地重启（Exec）与自后台化（ForkExec+Setsid）
│   │   ├── keyring/            #   AES 登录密钥文件管理
│   │   ├── vault/              #   数据主密钥生成与包裹
│   │   ├── termxkey/           #   TermX 接入密钥（加密落盘）
│   │   ├── termxcrypt/         #   TermX 客户端密文格式翻译
│   │   ├── termxtoken/         #   TermX 会话令牌
│   │   ├── portprobe/          #   端口占用探测
│   │   ├── captcha/            #   登录点选验证码
│   │   └── response/           #   统一响应结构
│   ├── repository/             # 数据访问层
│   ├── router/router.go        # 路由装配、限流挂载、安装态摘除
│   ├── service/                # 业务逻辑（setup/auth/termx/dbconfig/…）
│   └── web/web.go              # go:embed 内嵌前端 + SPA fallback + 缓存头
└── frontend（构建产物拷入 internal/web/dist 后嵌入）

frontend/
├── src/
│   ├── views/                  # 页面：Setup 安装向导 / Login（含翻转找回）/ Dashboard
│   │                           #       / Credentials 凭据 / Trash 回收站 / Settings
│   ├── api/                    # 按域拆分的请求封装（client.js 统一 baseURL）
│   ├── utils/crypto.js         # 纯 JS AES-256-GCM + SHA-256（NIST 向量自检）
│   ├── layouts/AppLayout.vue   # 侧栏 + 顶栏骨架
│   └── router/index.js         # 路由与守卫（未初始化强制进 /setup）
└── dist/                       # vite 构建产物（打包时拷入后端）

build.sh                        # 三步打包脚本（见下）
LICENSE                         # 限制性许可证（禁止未授权二次开发与商用）
```

## 快速开始

**方式一：直接用发布包**（[Releases](https://github.com/nncscn/TermX-server/releases) 提供 linux-amd64 / linux-arm64）

```bash
tar xzf termx-server_v0.1.0_linux_amd64.tar.gz
chmod +x termx-server
./termx-server
```

**方式二：源码构建**（需要 Go 1.21+ 和 Node.js 18+）

```bash
git clone https://github.com/nncscn/TermX-server.git
cd TermX-server
./build.sh
cd backend && ./termx-server
```

启动后终端打印访问地址（默认 `http://<本机IP>:18080`），浏览器打开进入安装引导。远程访问需在引导里把监听地址设为 `0.0.0.0` 并放行防火墙端口。

**引导最后会展示两把密钥，务必分清：**

| | ① 恢复密钥（8 组 XXXX-XXXX-…） | ② TermX 接入密钥（64 位十六进制） |
|---|---|---|
| 用途 | 忘记主密码时自救重置，唯一手段 | 给 TermX 桌面客户端连接本服务器 |
| 丢失后果 | 主密码永远找不回，数据等于丢失 | 无损失，`--show-key` 随时可再看 |
| 保存建议 | 离线抄写或下载 `termx-recovery.key` | 不用桌面客户端可忽略 |

主密码本身既是登录口令又参与加密密钥库，请用 12 位以上强密码。全部数据在程序旁的 `data/` 目录，**备份就是复制这个目录**。

**常用命令：**

| 命令 | 作用 |
|---|---|
| `./termx-server` | 启动（终端里自动转后台） |
| `./termx-server -f` | 前台运行，调试用 |
| `./termx-server --stop` | 停止后台服务 |
| `./termx-server --show-key` | 查看 TermX 接入密钥 |
| `./termx-server --uninstall` | 一键卸载向导 |
| `./termx-server --help` | 帮助 |

## 打包说明

`build.sh` 一共三步：

```text
==> [1/3] 构建前端...        vite build（产出 frontend/dist）
==> [2/3] 拷贝产物到内嵌目录... frontend/dist → backend/internal/web/dist
==> [3/3] 编译后端...        CGO_ENABLED=0 go build（静态单文件）
==> 打包完成：backend/termx-server（48M）
```

- **单文件原理**：`backend/internal/web/web.go` 用 `go:embed all:dist` 把前端产物编译进二进制，运行时自带 SPA fallback（前端路由刷新不 404）与静态资源缓存头
- **静态编译**：`CGO_ENABLED=0` 让二进制不依赖系统 glibc，任意 Linux 发行版直接跑
- **交叉编译**：`GOOS=linux GOARCH=arm64 ./build.sh` 即可产出 ARM64 包；日常发布见 [Releases](https://github.com/nncscn/TermX-server/releases)
- 改了前端只需重跑 `./build.sh`，前端与后端会一起打进同一个文件

## 二次修改指南

> 本项目许可证要求二次开发须获作者书面授权（见 [LICENSE](LICENSE)）。下表帮助获得授权后的定制开发快速定位。

| 想改什么 | 动哪里 |
|---|---|
| 默认端口 / 监听地址 / 同步前缀 | `backend/internal/config/config.go` 的 `Default()` |
| 启动横幅、品牌名、版本号 | `backend/internal/pkg/console/console.go`（`Version` 常量） |
| 新增一个后端接口 | `internal/router/router.go` 注册路由 → `internal/handler/` 新建处理层 → `internal/service/` 业务层 → `internal/repository/` 数据层（四层各一个文件） |
| 登录 / 锁定 / 会话策略 | `backend/internal/service/auth.go` + `internal/middleware/auth.go` |
| 安装引导流程与步骤 | 前端 `frontend/src/views/SetupView.vue` + 后端 `internal/service/setup.go`、`internal/handler/setup.go` |
| 加密算法与参数 | `frontend/src/utils/crypto.js`（注意：改动会破坏与既有密文及 TermX 客户端的兼容） |
| 页面与交互 | `frontend/src/views/*.vue`、`frontend/src/layouts/AppLayout.vue` |
| 数据模型增删字段 | `backend/internal/model/` 对应模型（启动时 AutoMigrate 自动建表） |
| 新增数据库引擎 | `backend/internal/database/ensure.go` + `internal/service/dbconfig.go` |
| TermX 同步协议 | `backend/internal/handler/termx.go` + `internal/pkg/termxcrypt/` |
| CLI 命令 | `backend/cmd/server/` 下新建文件 + `main.go` 参数接线 + `printUsage` 补帮助 |

改完验证：`cd backend && go build ./... && go vet ./...`，前端 `cd frontend && npm run build`，最后 `./build.sh` 出包。

## 许可证

本项目使用限制性许可证：**仅供学习与技术交流，二次开发须获作者书面授权，禁止任何形式的商业使用**。完整条款见 [LICENSE](LICENSE)。

---

<a id="english"></a>

# TermX (English)

**[中文](#termx)** | **English**

A self-hosted SSH credential management service, designed to work with the TermX desktop client.

Server addresses, accounts, passwords and SSH private keys are stored in one place — **encrypted end-to-end in the browser before anything reaches the server; the server never sees plaintext**. The whole service compiles into a single executable: drop it on a Linux box, run it, complete the five-step setup wizard in your browser, and it daemonizes itself.

## Features

- **Single-file deployment** — the frontend is embedded into the binary via `go:embed` (~48M). No database, Node, or runtime dependencies.
- **End-to-end encryption** — connection secrets (host / user / password / private key) are AES-256-GCM encrypted in the browser; the server stores ciphertext only and cannot decrypt it.
- **Five-step setup wizard** — welcome & license → database → server settings → security → initialize. Fully browser-driven, creates database and tables automatically.
- **Multiple databases** — SQLite out of the box; MySQL / PostgreSQL / SQL Server with automatic database creation, runtime switching, and data migration.
- **TermX desktop client sync** — credentials flow between desktop and web, encrypted on the client side as well.
- **Trash / auto-lock / clipboard auto-clear** — deletions are recoverable, idle sessions lock, sensitive clipboard copies clear on a timer.
- **Built-in security** — escalating login cooldowns (5 fails → 60s, 10 → 5min, 15 → 30min), rate limiting, request body caps, security headers, and setup routes that are removed after installation.

## Code Highlights

**1. Zero-knowledge encryption architecture**
All encryption happens in the browser. `frontend/src/utils/crypto.js` contains a pure-JS AES-256-GCM and SHA-256 implementation (WebCrypto is unavailable in non-secure HTTP contexts) that self-checks against official NIST test vectors on startup. Login uses PBKDF2 AuthHash comparison; a dummy PBKDF2 runs on unknown-user paths to equalize timing against account enumeration (`internal/service/auth.go`).

**2. Atomic setup wizard with self-destructing routes**
`internal/service/setup.go` chains the finish action indivisibly: validate → create database → AutoMigrate → create account → write config → restart in place. Any failure leaves no half-initialized state. After installation, the `setup/complete` family of routes is removed from the router entirely (`internal/router/router.go`), closing the "re-install takeover" attack surface.

**3. Self-healing**
Config is always double-written to `data/config.yaml` + `data/config.yaml.bak`. If the HTTPS certificate fails to load or a database switch fails, the program rolls back to the backup and restarts itself in place via `syscall.Exec` (`internal/pkg/restart/restart.go`) instead of locking the admin out.

**4. Self-daemonization and single-instance guard**
Started from a terminal, the process forks itself into the background (ForkExec + Setsid) and records its PID in `data/termx-server.pid`; a second launch refuses to run. If the configured port is taken, it finds a free one ≥10000 and writes it back to the config (`internal/pkg/portprobe/`).

**5. Database-agnostic layer**
GORM models avoid hardcoded column types so each dialect (SQLite/MySQL/PostgreSQL/SQL Server) maps natively. Runtime database switching migrates ciphertext as-is while accounts and sessions survive; name collisions auto-avoid with a dialog; external database hosts are restricted to private networks (`internal/database/`).

**6. TermX sync protocol**
Desktop clients authenticate with dual tokens — `X-Server-Key` (server access key) plus `X-Vault-Token` (vault session token) — under separate rate limits; the two ciphertext formats are translated transparently on the server (`internal/handler/termx.go`, `internal/pkg/termxcrypt/`).

**7. HTTP hardening**
10MB `MaxBytesReader` body cap, security headers (`X-Frame-Options: DENY`, `nosniff`, `no-referrer`, no-store on APIs), per-IP fixed-window rate limiting with periodic cleanup, and same-origin binding on wizard endpoints (`internal/middleware/`). Sensitive files (config, keys, logs) are chmod 0600; the TermX access key is stored encrypted.

## Project Structure

The backend follows a strict four-layer architecture — **router → handler → service → repository** — with one-way dependencies; `cmd/` only wires things up, and `internal/pkg/` holds business-agnostic utilities.

```
backend/
├── cmd/server/                 # entrypoint & CLI
│   ├── main.go                 #   startup: config → port guard → DB → routes → daemonize → HTTP(S)
│   ├── stop.go                 #   --stop: pidfile + /proc check, SIGTERM → SIGKILL
│   ├── showkey.go              #   --show-key: display the TermX access key
│   └── uninstall.go            #   --uninstall: interactive uninstall wizard
├── internal/
│   ├── config/config.go        # config load/save/backup self-healing (data/config.yaml)
│   ├── database/               # multi-engine probing, auto-create, migration, rollback
│   ├── handler/                # HTTP handlers (auth/setup/credential/sshkey/trash/termx/…)
│   ├── middleware/             # auth, rate limit, security headers, CORS, TermX tokens
│   ├── model/                  # GORM models (Account/Credential/SshKey/…)
│   ├── pkg/
│   │   ├── console/            #   terminal banner, colored output, key display box
│   │   ├── restart/            #   in-place restart (Exec) & daemonize (ForkExec+Setsid)
│   │   ├── keyring/            #   AES login key file management
│   │   ├── vault/              #   master key generation & wrapping
│   │   ├── termxkey/           #   TermX access key (encrypted at rest)
│   │   ├── termxcrypt/         #   TermX client ciphertext format translation
│   │   ├── termxtoken/         #   TermX session tokens
│   │   ├── portprobe/          #   port availability probing
│   │   ├── captcha/            #   login click-captcha
│   │   └── response/           #   unified response envelope
│   ├── repository/             # data access layer
│   ├── router/router.go        # route assembly, rate limiting, post-install route removal
│   ├── service/                # business logic (setup/auth/termx/dbconfig/…)
│   └── web/web.go              # go:embed frontend + SPA fallback + cache headers
└── frontend (built assets copied into internal/web/dist and embedded)

frontend/
├── src/
│   ├── views/                  # Setup wizard / Login (flip-card recovery) / Dashboard
│   │                           # / Credentials / Trash / Settings
│   ├── api/                    # per-domain API wrappers (client.js holds the baseURL)
│   ├── utils/crypto.js         # pure-JS AES-256-GCM + SHA-256 (NIST self-check)
│   ├── layouts/AppLayout.vue   # sidebar + topbar skeleton
│   └── router/index.js         # routes & guards (uninitialized → /setup)
└── dist/                       # vite build output (copied into the backend when packaging)

build.sh                        # three-step packaging script (see below)
LICENSE                         # restrictive license (no unauthorized forks, no commercial use)
```

## Getting Started

**Option 1: use a release package** (linux-amd64 / linux-arm64 on the [Releases](https://github.com/nncscn/TermX-server/releases) page)

```bash
tar xzf termx-server_v0.1.0_linux_amd64.tar.gz
chmod +x termx-server
./termx-server
```

**Option 2: build from source** (requires Go 1.21+ and Node.js 18+)

```bash
git clone https://github.com/nncscn/TermX-server.git
cd TermX-server
./build.sh
cd backend && ./termx-server
```

The terminal prints the access URL (default `http://<server-ip>:18080`). Open it in a browser to start the wizard. For remote access, set the listen address to `0.0.0.0` during setup and open the firewall port.

**The wizard's final page shows two keys — know the difference:**

| | ① Recovery key (8 groups, XXXX-XXXX-…) | ② TermX access key (64 hex chars) |
|---|---|---|
| Purpose | The only way to reset a forgotten master password | For the TermX desktop client to connect |
| If lost | The master password is unrecoverable; data is effectively lost | No harm — `--show-key` shows it anytime |
| Storage | Write it down offline or download `termx-recovery.key` | Ignorable if you don't use the desktop client |

The master password itself is both your login and part of the encryption key material — use 12+ characters. All data lives in the `data/` directory next to the binary; **backups are just copies of that directory**.

**Common commands:**

| Command | Purpose |
|---|---|
| `./termx-server` | start (auto-daemonizes from a terminal) |
| `./termx-server -f` | run in foreground, for debugging |
| `./termx-server --stop` | stop the background service |
| `./termx-server --show-key` | display the TermX access key |
| `./termx-server --uninstall` | interactive uninstall wizard |
| `./termx-server --help` | help |

## Packaging

`build.sh` runs three steps:

```text
==> [1/3] build frontend...    vite build (outputs frontend/dist)
==> [2/3] copy assets...       frontend/dist → backend/internal/web/dist
==> [3/3] build backend...     CGO_ENABLED=0 go build (static single file)
==> done: backend/termx-server (48M)
```

- **Single-file principle**: `backend/internal/web/web.go` embeds the frontend with `go:embed all:dist`, with SPA fallback and static caching headers built in.
- **Static build**: `CGO_ENABLED=0` removes the glibc dependency — runs on any Linux distro.
- **Cross-compile**: `GOOS=linux GOARCH=arm64 ./build.sh` produces an ARM64 build; published builds are on the [Releases](https://github.com/nncscn/TermX-server/releases) page.
- After changing the frontend, just re-run `./build.sh`; both ends land in the same single file.

## Modification Guide

> The license requires written authorization from the author before making derivative works (see [LICENSE](LICENSE)). The table below helps authorized customization find the right files quickly.

| What you want to change | Where |
|---|---|
| Default port / listen address / sync prefix | `Default()` in `backend/internal/config/config.go` |
| Startup banner, branding, version | `backend/internal/pkg/console/console.go` (the `Version` constant) |
| A new backend API | register in `internal/router/router.go` → new file in `internal/handler/` → `internal/service/` → `internal/repository/` (one file per layer) |
| Login / lockout / session policy | `backend/internal/service/auth.go` + `internal/middleware/auth.go` |
| Setup wizard flow | `frontend/src/views/SetupView.vue` + `internal/service/setup.go` / `internal/handler/setup.go` |
| Crypto algorithm & parameters | `frontend/src/utils/crypto.js` (beware: breaks compatibility with existing ciphertext and the TermX client) |
| Pages & interactions | `frontend/src/views/*.vue`, `frontend/src/layouts/AppLayout.vue` |
| Model fields | models in `backend/internal/model/` (AutoMigrate handles schema on startup) |
| New database engine | `backend/internal/database/ensure.go` + `internal/service/dbconfig.go` |
| TermX sync protocol | `backend/internal/handler/termx.go` + `internal/pkg/termxcrypt/` |
| CLI commands | new file under `backend/cmd/server/` + wiring in `main.go` + `printUsage` |

Verify after changes: `cd backend && go build ./... && go vet ./...`, then `cd frontend && npm run build`, and finally `./build.sh`.

## License

This project uses a restrictive license: **for study and technical exchange only; derivative works require written authorization from the author; commercial use is prohibited**. See [LICENSE](LICENSE) for the full terms.

<div align="center">

# JoyCode2Api

**JoyCode → Anthropic / OpenAI 协议翻译器**

让 Claude Code、Cursor、Codex 直接用上 JoyCode 背后的模型

`JoyAI-Code` · `Claude-Opus-4.7` · `GLM-5.1` · `Kimi-K2.6` · `MiniMax-M2.7` · `Doubao-Seed-2.0-pro`

[![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![React](https://img.shields.io/badge/React-19-61DAFB?style=flat&logo=react)](https://react.dev/)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue?style=flat)](./LICENSE)
[![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen?style=flat)](.)

[快速开始](#快速开始) · [配置](#配置) · [部署](#部署) · [API 参考](#api-参考) · [FAQ](#faq) · [贡献](#贡献)

</div>

---

## 概述

JoyCode（京东 AI 编程助手）背后挂了 GLM、Kimi、MiniMax、Doubao、Claude-Opus 等模型，但它的 API 是私有协议，主流编程工具接不上。JoyCode2Api 在中间做协议翻译，对外同时暴露两套标准协议：

```
Claude Code ─┐
Cursor    ───┼──→  JoyCode2Api  ──→  JoyCode API (jd.com)
Codex     ───┘    (协议翻译层)
```

- **Anthropic Messages API** (`/v1/messages`) — Claude Code 走这个
- **OpenAI Chat Completions API** (`/v1/chat/completions`) — Cursor / Codex 走这个

工具调用（tool use）、流式输出（SSE）、上下文截断全部完整翻译，使用体验和原生 API 一致。

> ⚠️ **免责声明**：本项目仅供**个人学习和技术研究**使用。禁止用于商业转售、API 中转服务（**中转站属于违法行为**）、大规模薅号或任何违法违规活动。因不当使用造成的一切后果由使用者自行承担，与项目作者无关。本项目不是 JoyCode 官方产品。

---

## 功能特性

| 特性 | 说明 |
|------|------|
| **双协议兼容** | 同时实现 Anthropic Messages + OpenAI Chat Completions，Claude Code 和 Cursor 各走各的通道 |
| **Tool Use 完整翻译** | Claude Code 的工具调用（读写文件、执行命令等）完整映射，不影响正常使用 |
| **SSE 流式输出** | 实时流式返回，打字机效果 |
| **多模型可选** | JoyAI-Code、Claude-Opus-4.7、GLM-5.1/5/4.7、Kimi-K2.6/2.5、MiniMax-M2.7、Doubao-Seed-2.0-pro |
| **多账号管理** | Dashboard 扫码 / OAuth / 手动添加多个 JD 账号，每个账号独立 API Key |
| **智能上下文截断** | 对话过长时自动截断早期消息，`/compact` 正常工作 |
| **自带 Dashboard** | Web 界面管理账号、查看用量、模型分布、请求记录、系统设置 |
| **凭据保活** | 后台定时刷新过期账号凭据，避免长时间不用失效 |
| **单文件部署** | 前端打包进 Go 二进制，丢一个文件就能跑，也支持 Docker / 系统服务 |

---

## 界面预览

<div align="center">
<table>
<tr>
<td><img src="data/imgs/dashboard.png" alt="Dashboard" width="360" /></td>
<td><img src="data/imgs/accounts.png" alt="账号管理" width="360" /></td>
</tr>
<tr>
<td align="center"><sub>数据概览 — 请求量、Token、延迟、模型分布</sub></td>
<td align="center"><sub>账号管理 — 多 JD 账号，扫码 / OAuth / 手动</sub></td>
</tr>
<tr>
<td><img src="data/imgs/account-detail.png" alt="账号详情" width="360" /></td>
<td><img src="data/imgs/settings.png" alt="系统设置" width="360" /></td>
</tr>
<tr>
<td align="center"><sub>账号详情 — 单账号用量与请求记录</sub></td>
<td align="center"><sub>系统设置 — 默认模型、超时、日志保留</sub></td>
</tr>
</table>
</div>

---

## 快速开始

> **TL;DR** — 编译 → `serve --skip-validation` → 浏览器加账号 → 配环境变量 → `claude`。

### 第 1 步：编译

需要 **Go 1.25+**。项目是纯 Go（SQLite 用 `modernc` 纯 Go 驱动），**无需任何 C 工具链**：

```bash
git clone https://github.com/vibe-coding-labs/JoyCode2Api.git
cd JoyCode2Api
CGO_ENABLED=0 go build -o JoyCode2Api ./cmd/JoyCode2Api/
```

前端已随仓库提交（`cmd/JoyCode2Api/static/`），不装 Node.js 也能编出带界面的二进制。只有要改前端时才需要 `cd web && npm install && npm run build`。

> 不想编译？去 [Releases](https://github.com/vibe-coding-labs/JoyCode2Api/releases) 下载预编译二进制，跳到第 2 步。

### 第 2 步：首次启动

> ⚠️ **两个必看的坑**
>
> 1. `./JoyCode2Api`（不带子命令）**只会打印帮助然后退出**，不会启动服务。必须用 `./JoyCode2Api serve`。
> 2. 首次启动时本机**还没有任何 JoyCode 凭据**（没装 JoyCode IDE / 非 macOS / Docker），`serve` 默认会去本地找凭据，找不到就直接报错退出。**第一次启动必须加 `--skip-validation`**，让服务先跑起来，凭据后面在 Dashboard 里加。

```bash
./JoyCode2Api serve --skip-validation --tls=false
```

看到下面这段 banner 就说明起来了：

```
  JoyCode Proxy 0.6.1
  ─────────────────────────────────────────────────
  Endpoints:
    POST /v1/chat/completions  — Chat (OpenAI format)
    POST /v1/messages          — Chat (Anthropic/Claude Code format)
    ...
  Dashboard:
    http://0.0.0.0:34891 — Web UI
```

> **macOS 且已装 JoyCode IDE**：可以不加 `--skip-validation`，程序会自动从 `~/Library/Application Support/JoyCode/User/globalStorage/state.vscdb` 读取已登录凭据。其他平台首次启动一律加 `--skip-validation`。

### 第 3 步：配置 Dashboard

浏览器打开 <http://localhost:34891>：

1. **首次访问进入初始化页面**，设置 root 密码（≥ 6 位）。这是 Dashboard 登录密码，跟 JoyCode 账号无关。
2. 用刚设置的密码登录。

### 第 4 步：添加 JD 账号

在「账号管理」里添加 JoyCode 账号，三种方式任选：

| 方式 | 操作 | 适用场景 |
|------|------|----------|
| **扫码登录**（推荐） | 点「扫码添加」，用**京东 App**（不是 JoyCode）扫二维码，手机确认后自动入库 | 有京东 App、最简单 |
| **OAuth 授权** | 点「OAuth授权登录」，跳转 JoyCode 页面完成授权 | 浏览器能访问 JoyCode |
| **手动添加** | 直接填 `pt_key` + `user_id` | 已有凭据，从 `state.vscdb` 或 OAuth 回调 URL 取 |

**OAuth 在 Docker / 远程部署时**：浏览器会跳转到一个打不开的 `localhost` 页面，这是正常的。把地址栏里完整的 URL（形如 `http://127.0.0.1:34891/?pt_key=xxx&...`）复制下来，粘进弹窗输入框，点「提交授权」。

添加成功后，每个账号会生成独立的 API Key（形如 `sk-joy-xxxx`），在账号列表里能看到。

### 第 5 步：接到编程工具

加完账号后，配置环境变量即可。**后续启动继续加 `--skip-validation`**（DB 里已有账号，但启动时的本地凭据检测仍可能失败，不影响请求走 DB 账号）：

```bash
./JoyCode2Api serve --skip-validation
```

**Claude Code：**

```bash
# 推荐：用某个账号的 API Key（多账号时各自隔离）
export ANTHROPIC_BASE_URL=http://localhost:34891
export ANTHROPIC_API_KEY=sk-joy-xxxx   # 替换成 Dashboard 里显示的 API Key

# 或偷懒走默认账号
export ANTHROPIC_BASE_URL=http://localhost:34891
export ANTHROPIC_API_KEY=joycode

claude
```

**Cursor / Codex（OpenAI 协议）：**

```bash
export OPENAI_BASE_URL=http://localhost:34891/v1
export OPENAI_API_KEY=sk-joy-xxxx

cursor   # 或 codex
```

---

## 配置

### 启动参数

`joycode-proxy serve` 支持以下参数：

| 参数 | 默认 | 说明 |
|------|------|------|
| `-H, --host` | `0.0.0.0` | 绑定地址 |
| `-p, --port` | `34891` | 监听端口 |
| `--tls` | `true` | 启用 HTTPS（自签名证书），同时仍接受 HTTP |
| `--skip-validation` | `false` | 跳过本地凭据检测，**非 macOS 首次启动必加** |
| `-k, --ptkey` | _空_ | 手动指定 JoyCode ptKey（留空则自动检测） |
| `-u, --userid` | _空_ | 手动指定 JoyCode userID（留空则自动检测） |
| `-v, --verbose` | `false` | 启用调试日志 |

### 环境变量

| 变量 | 说明 |
|------|------|
| `JOYCODE_STATE_DB` | 指定 JoyCode `state.vscdb` 路径（Docker 挂载场景用） |

### Dashboard 设置项

在 Dashboard「系统设置」页面配置，改完立即生效：

| 分组 | 设置项 | 默认 | 说明 |
|------|--------|------|------|
| **模型配置** | `default_model` | `JoyAI-Code` | 客户端未指定模型且账号未配置时的兜底模型 |
| | `default_max_tokens` | `8192` | 客户端未指定 `max_tokens` 时的默认值 |
| **连接优化** | `max_retries` | `3` | 请求失败自动重试次数 |
| | `request_timeout` | `120` | 与 JoyCode 后端通信超时（秒），低于 60 自动调到 60 |
| | `max_connections` | `20` | 与后端最大并发 HTTP 连接数，10 秒内生效 |
| **日志与监控** | `enable_request_logging` | `true` | 记录每个请求详情（模型、延迟、状态码），关闭后「数据概览」无数据 |
| | `log_retention_days` | `30` | 请求日志保留天数，每小时自动清理，`0` 永久保留 |

每个账号还可以单独设置 `default_model`，优先级高于全局默认。

---

## 部署

### 后台运行

`./JoyCode2Api serve` 默认是前台进程，关掉终端就停。长期运行的方式：

```bash
# 1. nohup（最简单）
nohup ./JoyCode2Api serve --skip-validation > joycode.log 2>&1 &

# 2. 守护进程模式（崩溃自动重启）
./JoyCode2Api daemon --skip-validation

# 3. 装成系统服务（macOS launchd / Linux systemd）
./JoyCode2Api service install
./JoyCode2Api service status
./JoyCode2Api service uninstall
```

### Docker

```bash
docker build -t joycode2api .
docker run -d -p 34891:34891 --name joycode2api joycode2api serve --skip-validation
```

> ⚠️ **必须显式加 `serve --skip-validation`**。镜像默认 `CMD ["serve"]` 不带 `--skip-validation`，而干净容器里没有本地 JoyCode 凭据，不加会直接报错退出。

**构建时连不上 Alpine 源？** `docker build` 卡在 `apk add` 报 `ca-certificates`/`gcc`/`musl-dev` "no such package"，是网络连不上 `dl-cdn.alpinelinux.org`（国内常见）。用 `ALPINE_MIRROR` 切国内镜像：

```bash
docker build \
  --build-arg ALPINE_MIRROR=https://mirrors.aliyun.com/alpine \
  -t joycode2api .
```

可选镜像源（写到 `/alpine` 为止）：
- 阿里云 `https://mirrors.aliyun.com/alpine`
- 清华 `https://mirrors.tuna.tsinghua.edu.cn/alpine`
- 中科大 `https://mirrors.ustc.edu.cn/alpine`

`go mod download` 慢的话构建时设 `GOPROXY=https://goproxy.cn,direct`。

**挂载本地凭据（可选）**：宿主机装了 JoyCode IDE 的话，把状态库挂进容器，Dashboard 的「一键导入」就能用：

```bash
docker run -p 34891:34891 \
  -e JOYCODE_STATE_DB=/data/state.vscdb \
  -v /path/to/JoyCode/state.vscdb:/data/state.vscdb:ro \
  joycode2api serve --skip-validation
```

### 系统服务

```bash
./JoyCode2Api service install      # 安装（macOS launchd / Linux systemd）
./JoyCode2Api service status       # 查看状态
./JoyCode2Api service uninstall    # 卸载
```

---

## API 参考

### 代理端点

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/v1/messages` | Anthropic Messages API（Claude Code） |
| `POST` | `/v1/chat/completions` | OpenAI Chat Completions API（Cursor / Codex） |
| `POST` | `/v1/web-search` | 网页搜索 |
| `POST` | `/v1/rerank` | 文档重排序 |
| `GET` | `/v1/models` | 可用模型列表 |
| `GET` | `/health` | 健康检查 |
| `GET` | `/` | Dashboard 管理界面 |

### 鉴权

请求代理端点时，通过 `x-api-key` 头或 `Authorization: Bearer <key>` 传 API Key：

- `sk-joy-xxxx` — Dashboard 添加账号时生成的独立 Key，路由到对应账号
- `joycode` — 走默认账号

### 命令一览

```
joycode-proxy serve           启动代理服务器（核心命令）
joycode-proxy daemon          守护进程模式（崩溃自动重启）
joycode-proxy service         管理系统服务（install/uninstall/status）
joycode-proxy check           检查代理是否在运行
joycode-proxy models          列出可用模型
joycode-proxy whoami          查看当前认证用户
joycode-proxy config          显示当前配置
joycode-proxy chat            发送一条聊天消息
joycode-proxy search          网页搜索
joycode-proxy reset-password  重置 Dashboard root 密码
joycode-proxy version         显示版本信息
```

加 `-h` 看每个命令的详细参数，例如 `joycode-proxy serve -h`。

---

## 项目结构

```
cmd/JoyCode2Api/        CLI 入口 + HTTP 服务器
├─ main.go              cobra 根命令 + daemon supervisor
├─ serve.go             核心启动逻辑、中间件链、路由注册、TLS
├─ daemon.go            守护进程模式
├─ service_*.go         macOS launchd / Linux systemd 服务
├─ static/              前端构建产物（go:embed 打进二进制）
└─ *.go                 各子命令（check/models/chat/search/...）

pkg/
├─ anthropic/           Anthropic 协议翻译（请求/响应/SSE/工具调用/上下文截断）
├─ openai/              OpenAI 协议翻译（chat/search/rerank/models）
├─ joycode/             JoyCode 上游客户端（color gateway 签名、gzip、流式）
├─ auth/                凭据检测（state.vscdb）、JD 扫码登录、JWT 中间件、密码
├─ store/               SQLite 存储（账号、设置、请求日志、token 用量）
├─ dashboard/           Dashboard 后端 API（/api/* 路由）
├─ proxy/               会话追踪
├─ keepalive/           凭据保活（定时刷新过期账号）
└─ logrot/              日志轮转

web/                    前端源码（React 19 + Ant Design 6 + Vite 8）
└─ src/
   ├─ pages/            Dashboard / Accounts / AccountDetail / Settings / Login / Setup
   ├─ components/       QRLoginModal / 图标 / Tooltip
   ├─ layouts/          MainLayout
   └─ api.ts            前端 API 封装
```

---

## FAQ

<details>
<summary><b>启动没有任何输出，服务也没运行</b></summary>

你直接执行了 `./JoyCode2Api`，但**没加 `serve` 子命令**——根命令只会打印帮助然后退出（exit 0）。正确启动：

```bash
./JoyCode2Api serve --skip-validation --tls=false
```
</details>

<details>
<summary><b>启动报 "cannot auto-detect credentials" 然后退出</b></summary>

`serve` 默认会去本地找 JoyCode 凭据，非 macOS / 没装 JoyCode IDE 的环境找不到就报错。加 `--skip-validation` 跳过本地凭据检测，凭据在 Dashboard 里加：

```bash
./JoyCode2Api serve --skip-validation
```
</details>

<details>
<summary><b>Dashboard 打不开 / 要求登录</b></summary>

首次访问 <http://localhost:34891> 会进入初始化页面，让你设置 root 密码（≥ 6 位）。这是 Dashboard 登录密码，跟 JoyCode 账号无关。忘了密码可以用 `./JoyCode2Api reset-password` 重置。
</details>

<details>
<summary><b>OAuth 登录跳转到一个打不开的 localhost 页面</b></summary>

这是 Docker / 远程部署的正常现象。把地址栏里完整的 URL（形如 `http://127.0.0.1:34891/?pt_key=xxx&...`）复制下来，粘进 Dashboard 弹窗的输入框，点「提交授权」。
</details>

<details>
<summary><b>启动时 TLS 证书生成失败</b></summary>

`--tls` 默认 `true`，自签名证书生成失败会自动 fallback 到 HTTP。想强制纯 HTTP 就加 `--tls=false`。
</details>

<details>
<summary><b>端口被占用</b></summary>

换端口：`./JoyCode2Api serve -p 34892`。
</details>

<details>
<summary><b>关闭终端后服务就停了</b></summary>

`serve` 是前台进程。用 `nohup ./JoyCode2Api serve --skip-validation > joycode.log 2>&1 &` 后台跑，或装成系统服务 `./JoyCode2Api service install`。
</details>

<details>
<summary><b>macOS 上能自动读取凭据，其他平台为什么不行</b></summary>

程序通过读取 JoyCode IDE 的 `state.vscdb`（SQLite）获取已登录凭据。macOS 上路径固定在 `~/Library/Application Support/JoyCode/User/globalStorage/state.vscdb`，能自动找到。其他平台需要：
- 用 `JOYCODE_STATE_DB` 环境变量指定 `state.vscdb` 路径，或
- 直接在 Dashboard 扫码 / OAuth 添加账号（推荐）
</details>

---

## 使用限制

- 每个用户最多配置 **10 个账号**，超出限制将无法添加或导入
- 使用前请确保你已了解并遵守 JoyCode 的服务条款
- 如果觉得 JoyCode 的模型好用，建议去 [JoyCode 官方](https://joycode.jd.com/) 支持正版

---

## 贡献

欢迎提 Issue 和 PR。提 PR 前请：

1. Fork 仓库并拉取最新 `main`
2. 新建分支：`git checkout -b feat/your-feature` 或 `fix/your-bugfix`
3. 改代码，确保 `go build ./...` 和 `go test ./...` 通过
4. 改前端的话需要 `cd web && npm run build` 重新构建产物
5. 提交时遵循 [Conventional Commits](https://www.conventionalcommits.org/) 规范

## 许可证

[Apache 2.0](./LICENSE)

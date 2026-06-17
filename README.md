<div align="center">

# JoyCodeProxy

**一个不太正经的协议翻译器**

让 Claude Code、Cursor 这类工具能直接用上 JoyCode 的模型

JoyAI-Code · GLM-5.1 · Kimi-K2.6 · MiniMax-M2.7 · Doubao-Seed-2.0-pro

[![Go](https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![React](https://img.shields.io/badge/React-19-61DAFB?style=flat&logo=react)](https://react.dev/)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue?style=flat)](./LICENSE)

</div>

---

> **免责声明：** 本项目仅供**个人学习和技术研究**使用。禁止用于商业转售、API 中转服务（**中转站属于违法行为**）、大规模薅号或任何黑灰产/违法违规活动。因不当使用造成的一切后果由使用者**自行承担**，与项目作者无关。本项目不是 JoyCode 官方产品。

---

## 起因

事情是这样的：JoyCode（京东的 AI 编程助手）里面有一些不错的模型，GLM、Kimi、MiniMax、Doubao 这些都有。但它的 API 协议跟 Anthropic 和 OpenAI 的不一样，所以 Claude Code、Cursor 这些主流编程工具接不上。

JoyCodeProxy 就是在中间做了一个翻译层，把协议对齐了。改两个环境变量，Claude Code 就能直接用 JoyCode 的模型了。

```
Claude Code / Cursor / Windsurf  →  JoyCodeProxy  →  JoyCode API
                                    (协议翻译)
```

说白了就这点事，没有多复杂。做这个东西初衷是学习 Go 和了解 API 协议的差异，顺便给自己用着方便。

## 界面

自带一个管理后台，账号、用量、配置都能在上面看和改。

<div align="center">
<img src="data/imgs/dashboard.png" alt="Dashboard" width="720" />
<p><sub>数据概览 — 请求量、Token 消耗、延迟统计、模型分布</sub></p>
</div>

<div align="center">
<img src="data/imgs/accounts.png" alt="账号管理" width="720" />
<p><sub>账号管理 — 支持多个 JD 账号，扫码添加</sub></p>
</div>

<div align="center">
<img src="data/imgs/account-detail.png" alt="账号详情" width="720" />
<p><sub>账号详情 — 单个账号的用量、模型分布、请求记录</sub></p>
</div>

<div align="center">
<img src="data/imgs/settings.png" alt="系统设置" width="720" />
<p><sub>系统设置 — 默认模型、超时时间、日志保留，改完马上生效</sub></p>
</div>

## 能做什么

- **Anthropic + OpenAI 双协议** — 同时兼容 Anthropic Messages API 和 OpenAI Chat Completions API，Claude Code 和 Cursor 各走各的通道
- **Tool Use 完整翻译** — Claude Code 的工具调用（读写文件、执行命令等）完整映射，不影响正常使用
- **SSE 流式输出** — 实时流式返回，打字机效果
- **多模型可选** — JoyAI-Code、GLM-5.1、GLM-5、GLM-4.7、Kimi-K2.6、Kimi-K2.5、MiniMax-M2.7、Doubao-Seed-2.0-pro
- **多账号管理** — Dashboard 上扫码添加多个 JD 账号，每个账号有独立的 API Key
- **智能上下文截断** — 对话过长时自动截断早期消息，不会卡死，`/compact` 正常工作
- **单文件部署** — 前端打包进 Go 二进制，丢一个文件就能跑，也支持 Docker

## 怎么跑起来

### 构建

需要 Go 1.22+ 和 Node.js 18+。

```bash
# 先构建前端
cd web && npm install && npm run build && cd ..

# 再构建后端（前端会自动嵌入）
go build -o joycode_proxy_bin ./cmd/JoyCodeProxy/
```

或者用 Docker：

```bash
docker build -t joycode-proxy .
docker run -p 34891:34891 joycode-proxy
```

> **构建时连不上 Alpine 源?** 如果 `docker build` 卡在 `apk add` 并报 `ca-certificates`/`gcc`/`musl-dev` "no such package"，根因通常是网络连不上官方源 `dl-cdn.alpinelinux.org`（国内常见）。用 `ALPINE_MIRROR` 构建参数切到国内镜像即可：
>
> ```bash
> docker build \
>   --build-arg ALPINE_MIRROR=https://mirrors.aliyun.com/alpine \
>   -t joycode-proxy .
> ```
>
> 镜像源任选其一（写到 `/alpine` 为止）：阿里云 `https://mirrors.aliyun.com/alpine`、清华 `https://mirrors.tuna.tsinghua.edu.cn/alpine`、中科大 `https://mirrors.ustc.edu.cn/alpine`。若 `go mod download` 也慢，可在构建环境设 `GOPROXY=https://goproxy.cn,direct`。

### 启动

```bash
./joycode_proxy_bin serve
```

默认监听 `0.0.0.0:34891`。macOS 首次启动会自动从本地 JoyCode 客户端读取凭据，不需要手动配。

### 接到 Claude Code

改两个环境变量就行：

```bash
export ANTHROPIC_BASE_URL=http://localhost:34891
export ANTHROPIC_API_KEY=joycode

claude
```

### 多账号

打开 `http://localhost:34891`，用 JD App 扫码添加账号。每个账号会生成一个独立的 API Key：

```bash
export ANTHROPIC_API_KEY=sk-joy-xxxx
claude
```

### Docker / 远程部署登录

非 macOS（尤其是 Docker）环境拿不到本地 JoyCode 客户端凭据，登录方式如下：

1. **OAuth 授权（推荐）**：在 Dashboard 点「OAuth授权登录」，在打开的 JoyCode 页面完成授权。
   - 本地直接部署时，回调会自动检测并添加账号。
   - **Docker / 远程部署时，浏览器会跳转到一个无法访问的 `localhost` 页面，这是正常现象**。把该页面地址栏里的完整 URL（形如 `http://127.0.0.1:34891/?pt_key=xxx&...`）复制下来，粘贴进弹窗的输入框，点「提交授权」即可。弹窗里的粘贴框现在一打开就可见，不用再等。
2. **手动添加**：若你已经有 `pt_key`，可在「手动添加」里直接填。
   - `pt_key`：来自上面 OAuth 回调 URL 的 `pt_key` 参数，或本地 JoyCode IDE 的 `state.vscdb`。
   - `user_id`：JoyCode 客户端 → 设置 → 个人信息。
3. **挂载本地凭据**：如果宿主机装了 JoyCode IDE，可把其状态库挂进容器，让「一键导入」可用：
   ```bash
   docker run -p 34891:34891 \
     -e JOYCODE_STATE_DB=/data/state.vscdb \
     -v /path/to/JoyCode/state.vscdb:/data/state.vscdb:ro \
     joycode-proxy
   ```

## API 端点

| 路径 | 说明 |
|------|------|
| `POST /v1/messages` | Anthropic Messages API，Claude Code 走这个 |
| `POST /v1/chat/completions` | OpenAI Chat Completions API，Cursor 走这个 |
| `POST /v1/web-search` | 网页搜索 |
| `POST /v1/rerank` | 文档重排序 |
| `GET /v1/models` | 拉取可用模型列表 |
| `GET /health` | 健康检查 |
| `GET /` | Dashboard 管理界面 |

## 项目结构

```
cmd/JoyCodeProxy/    入口，HTTP 服务器
pkg/anthropic/       Anthropic 协议翻译（请求、响应、SSE 流式）
pkg/openai/          OpenAI 协议翻译
pkg/joycode/         JoyCode API 客户端
pkg/auth/            凭据读取、JD 扫码登录
pkg/store/           SQLite 存储（账号、设置、请求日志）
pkg/dashboard/       Dashboard API
web/                 前端（React + Ant Design）
```

## 使用限制

- 每个用户最多配置 **10 个账号**，超出限制将无法添加或导入
- 使用本项目前，请确保你已了解并遵守 JoyCode 的服务条款
- 如果你觉得 JoyCode 的模型好用，建议去 [JoyCode 官方](https://joycode.jd.com/) 支持正版

## 许可证

Apache 2.0

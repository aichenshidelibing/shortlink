# Shortlink - 高安全短链服务

🔗 一个基于 Go 的短链系统，提供公开短链创建、动态管理后台、API Key 接入、通知告警、举报处理和多层安全防护。

## 特性

- **公开创建短链**：无需注册即可创建短链，可配置隐私确认、Cap 自托管人机验证、异常访问升级 Cloudflare Turnstile。
- **短链能力**：支持自定义短码、访问密码、过期时间、一次性链接、二维码展示。
- **动态管理后台**：管理入口使用动态后缀，可手动/定时轮换，内部别名禁止直连。
- **管理员安全**：固定管理员账号、bcrypt 密码、TOTP 双因素认证、Session Cookie。
- **API Key 接入**：后台创建 API Key 后，可通过 `/api/v1/links` 创建短链。
- **通知系统**：支持飞书、Telegram、钉钉、企业微信、Bark、Discord、Email、通用 Webhook。
- **用户消息公告**：后台可配置公开页消息，支持 info/success/warn/danger 等级和浅/深色主题。
- **举报与审核**：公开页可举报短链，后台可人工处理或自动处理。
- **安全防护**：AES-GCM 加密 URL/IP，TOTP secret 加密存储，DFA 敏感词检测，IP 限速封禁，Cap + Turnstile 自适应验证码。
- **运行状态**：公开页和后台均可查看服务可用性/系统状态。
- **网络隔离**：Docker Compose 默认仅绑定 `127.0.0.1`，建议通过 Nginx/Caddy 等反代对外暴露。

---

## 部署架构

```text
[Internet]
    ↓
[Nginx / Caddy / Cloudflare]
    ↓  只需要反代 Shortlink 主服务域名
[127.0.0.1:HTTP_PORT] → [Go App:8080]
                           ├─ /cap/*  →  内部反代到 Cap Standalone:3000
                           ├─ MySQL:3306（Docker 内网）
                           └─ Redis:6379（Docker 内网）

[Cap Dashboard]
    └─ 仅绑定宿主机 127.0.0.1:CAP_HTTP_PORT，作为本机管理入口，不建议暴露公网
```

默认 Compose 会把应用端口绑定到宿主机 `127.0.0.1`，避免绕过反代直接暴露后台。生产环境请在前面配置 HTTPS 反向代理。

> Cap 不需要单独准备公网域名。公开页面和 Cap widget/API 统一走 Shortlink 的同源路径 `/cap/`，你的公网反代只需要指向 Shortlink 主服务即可。Cap 控制台端口默认也是 `127.0.0.1` 随机高位端口，只用于本机管理。

---

## 从 GitHub 拉取项目

原始仓库地址：

```bash
git clone https://github.com/aichenshidelibing/shortlink.git
```

加速站地址（使用 gh-proxy）：

```bash
git clone https://gh-proxy.com/https://github.com/aichenshidelibing/shortlink.git
```

如果你已经克隆过仓库，也可以直接更新：

```bash
git pull origin main
```

## 部署指南

### 前置条件

- Docker >= 20.10
- Docker Compose v2 或 `docker compose` 插件
- Go 1.26（仅本地开发需要；Docker 构建会使用 Go 1.26.5）
- 建议内存 >= 1GB
- 生产环境建议准备域名和 HTTPS 反代

### 一键部署（推荐）

推荐使用 `deploy.sh`，它会一次性完成应用、MySQL、Redis、Cap Standalone 和 Cap Valkey 的安装配置，不需要你手动去 Cap 后台一步一步创建密钥。

```bash
# 1. 进入项目目录
cd shortlink

# 2. 运行一键部署脚本
bash deploy.sh
```

脚本会依次处理：

1. **检查 Docker / Docker Compose**
   - 如果本机没有 Docker，脚本会尝试自动安装。
   - 如果 Docker Compose 不可用，会提示你安装 Compose v2。

2. **预拉取镜像**
   - Go 构建镜像：`golang:1.26.5-alpine3.24`
   - 运行基础镜像：`alpine:3.24`
   - 数据库：`mysql:8.4`
   - 缓存：`redis:8.8.0-alpine`
   - 验证码：`tiago2/cap:latest`
   - Cap 存储：`valkey/valkey:9-alpine`

3. **生成安全密钥**
   - 管理员密码由你输入。
   - `ENCRYPTION_KEY`、`ADMIN_SESSION_SECRET`、数据库密码、Redis 密码、Hashids Salt、Cap Admin Key 等会自动生成。
   - 如果已经存在 `.env`，脚本会安全解析并复用其中的密钥，避免重复部署时破坏已有数据。

4. **填写必要配置**
   - 管理员密码：用户名固定为 `admin`。
   - 对外访问端口：例如 `8080` 或 `18080`，默认只绑定 `127.0.0.1`。
   - 数据库用户名和数据库名。
   - Cap 本地管理端口：默认随机高位端口，并且只绑定 `127.0.0.1`，不要暴露公网。

5. **选择通知渠道**
   - 支持飞书、Telegram、钉钉、企业微信、Bark、Discord、Email、通用 Webhook。
   - 通知渠道会用于发送管理后缀变更、登录提醒等安全消息。
   - 如果暂时不想配置，可以选择 `0` 跳过，并确认跳过。

6. **写入本地配置**
   - `.env`：Docker Compose 使用的环境变量，包含真实密钥，已被 `.gitignore` 和 `.dockerignore` 排除。
   - `configs/config.yaml`：本地可读配置备份，同样包含敏感值，不要提交到公开仓库。

7. **启动容器**
   - `shortlink-app`：Go 主服务。
   - `shortlink-mysql`：MySQL。
   - `shortlink-redis`：Redis。
   - `shortlink-cap`：Cap Standalone 验证码服务。
   - `shortlink-cap-valkey`：Cap 使用的 Valkey 存储。

8. **自动初始化 Cap**
   - 脚本会登录 Cap Dashboard API。
   - 自动创建 Shortlink 使用的 Cap Site Key / Secret Key。
   - 自动写回 `.env` 和 `configs/config.yaml`。
   - 如果旧的 Cap Site Key 已失效，脚本会检测到并自动重新创建。
   - 应用侧使用 `/cap/<siteKey>/` 同源路径访问 Cap，不需要单独的 Cap 公网域名。

部署成功后会看到类似输出：

```text
访问地址: http://127.0.0.1:18080
管理后台: 查看 docker compose logs app 中的 Admin suffix，然后访问 http://127.0.0.1:18080/{suffix}/
Cap 控制台: http://127.0.0.1:xxxxx（本机随机端口，不暴露公网）
Cap 公网页面/组件统一走 Shortlink 同域反代: /cap/
```

通知渠道测试会发送一条包含 6 位验证码的测试消息，必须输入正确验证码后才会继续部署。若你选择跳过通知，后续可以通过日志查看管理后缀。

#### 反向代理示例

生产环境建议只把 Shortlink 主服务暴露给公网，例如：

```nginx
server {
    listen 443 ssl http2;
    server_name s.example.com;

    ssl_certificate     /path/fullchain.pem;
    ssl_certificate_key /path/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:18080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto https;
    }
}
```

不需要额外给 Cap 配置 `cap.example.com`。`/cap/` 已由 Shortlink 主服务内部代理到 Cap 容器。

### 手动部署

```bash
# 1. 准备 .env 或 configs/config.yaml
# 至少需要 ADMIN_PASSWORD 和 ENCRYPTION_KEY

# 2. 构建并启动
docker compose up --build -d
```

### 常用命令

```bash
# 查看日志
docker compose logs -f app

# 查看管理后缀
docker compose logs app | grep "Admin suffix"

# 停止服务
docker compose down
```

---

## 首次启动流程

### 1. 绑定 TOTP

首次初始化管理员后，服务日志只会提示 TOTP 待绑定，不会输出包含密钥的 `otpauth://` URI。

请先登录后台，再到设置/TOTP 页面查看绑定信息，并添加到 Google Authenticator、Microsoft Authenticator、Authy 等验证器。

### 2. 获取管理入口

管理后台入口为：

```text
http://your-domain/{admin_suffix}/
```

后缀来源：

- 部署脚本配置的通知渠道会收到后缀变更通知；
- 或查看日志中的 `Admin suffix` 字段。

> 当前代码没有实现“通过机器人命令查询后缀”的入站 Bot 功能。Telegram/飞书等只作为出站通知渠道。

### 3. 登录后台

访问 `/{admin_suffix}/`，使用：

- 用户名：`admin`
- 密码：部署时设置的管理员密码
- TOTP：启用 TOTP 后登录时需要

### 4. 完成后台配置

建议检查：

- TOTP 是否已验证启用
- 通知渠道是否正确
- Cloudflare Turnstile（可选）
- 公开页背景/图标/用户消息
- 短链默认长度
- 限速、白名单、敏感词和举报处理策略

---

## 使用指南

### 创建短链

访问首页：

```text
http://your-domain/
```

支持：

| 功能 | 说明 |
|------|------|
| 自定义短码 | 4-32 位，需与已有短码和管理后缀不冲突 |
| 访问密码 | 访问时需要输入密码 |
| 过期时间 | 默认可选永不过期、1 小时、1 天、7 天、30 天；实际选项、默认 TTL、最大 TTL、是否允许永不过期均可在后台动态配置 |
| 一次性链接 | 第一次成功访问后自动禁用 |
| 二维码 | 创建成功后显示短链二维码 |
| 举报 | 用户可提交短链举报 |

### 人机验证：Cap + Turnstile

本项目默认使用 [Cap](https://github.com/tiagozip/cap) 作为日常验证码。Cap 是自托管的轻量 Proof-of-Work 验证方案，适合减少普通用户受到第三方验证码打扰的频率。

当前策略：

1. **普通访问**：使用 Cap。
   - 前端加载 `/cap/assets/widget.js`。
   - 浏览器完成 Cap challenge 后拿到 token。
   - 后端使用 Cap Secret 调用 Cap `siteverify` 校验 token。

2. **异常访问**：可升级到 Cloudflare Turnstile。
   - 例如同一 IP 多次验证码失败时，服务端会进入短期风险窗口。
   - 只有配置了 `CF_ENABLED=true`、`CF_SITE_KEY`、`CF_SECRET_KEY` 后，才会真正要求 Turnstile。
   - 如果没有配置 Turnstile，不会把用户锁死在无法完成的验证流程里。

3. **API Key 接口**：不使用浏览器验证码。
   - `/api/v1/links` 和 `/api/v1/links/batch` 依赖 API Key 权限、额度和域名策略。
   - 这是为了方便程序化调用，避免服务端 API 被浏览器组件限制。

Cap 相关环境变量：

| 变量 | 说明 |
|------|------|
| `CAPTCHA_ENABLED` | 是否启用验证码，部署脚本默认启用 |
| `CAPTCHA_PROVIDER` | 默认 `cap` |
| `CAPTCHA_MODE` | 默认 `adaptive` |
| `CAPTCHA_NORMAL_PROVIDER` | 普通验证提供方，默认 `cap` |
| `CAPTCHA_ESCALATION_PROVIDER` | 异常升级提供方，默认 `turnstile` |
| `CAPTCHA_FAILURE_THRESHOLD` | 连续失败多少次后进入风险窗口 |
| `CAPTCHA_RISK_WINDOW_SECONDS` | 风险窗口持续秒数 |
| `CAP_SITE_KEY` | Cap Site Key，由脚本自动创建 |
| `CAP_SECRET_KEY` | Cap Secret Key，由脚本自动创建，请勿公开 |
| `CAP_API_ENDPOINT` | 前端使用的同源 Cap API 路径，例如 `/cap/xxxx/` |
| `CAP_VERIFY_URL` | 后端容器内访问 Cap 的 siteverify URL |
| `CAP_ADMIN_KEY` | Cap 控制台管理密钥，请勿公开 |
| `CAP_HTTP_PORT` | Cap 控制台本机端口，只绑定 `127.0.0.1` |

如果你不想使用 Cap，可以在 `.env` 中设置：

```env
CAPTCHA_ENABLED=false
```

然后重新启动：

```bash
docker compose up -d app
```

但不建议在公开服务中完全关闭验证码。更推荐保留 Cap，必要时再配置 Turnstile 作为异常升级验证。

### API 接入

详见 [API.md](./API.md)。

公开创建示例：

```bash
curl -X POST http://localhost:8080/api/links \
  -F "url=https://example.com" \
  -F "privacy_agree=true"
```

API Key 创建示例：

```bash
curl -X POST http://localhost:8080/api/v1/links \
  -H "X-API-Key: sl_你的API密钥" \
  -H "Content-Type: application/json" \
  -d '{"url":"https://example.com","ttl":86400,"is_once":false}'
```

> 当前 `/api/v1` 注册了 API Key 单条创建接口 `POST /api/v1/links` 和批量创建接口 `POST /api/v1/links/batch`；列表、删除、统计接口目前通过管理后台提供。

---

## 管理后台功能

| 模块 | 功能 |
|------|------|
| 总览 | 总链接数、总点击量、举报数、封禁数 |
| 短链管理 | 列表查看、删除 |
| API Keys | 创建、查看、删除 API Key |
| 统计 | 查看短链点击统计 |
| 安全 | IP 封禁列表、解封、敏感词重载 |
| 举报 | 举报列表、人工处理、自动处理 |
| 系统状态 | DB/Redis/运行时/容器/宿主资源概览 |
| 设置 | 管理后缀、短链长度、通知渠道、背景、版本、CF、用户消息等 |
| TOTP | 查看绑定信息并验证启用 |

---

## 配置说明

主要环境变量：

| 变量 | 必填 | 默认值 | 说明 |
|------|------|--------|------|
| `ADMIN_USERNAME` | 否 | `admin` | 管理员用户名 |
| `ADMIN_PASSWORD` | 是 | - | 管理员密码 |
| `ENCRYPTION_KEY` | 是 | - | 加密密钥，至少 32 字节随机值 |
| `ADMIN_SESSION_SECRET` | 否 | `ENCRYPTION_KEY` | Session 签名密钥 |
| `HTTP_BIND` | 否 | `127.0.0.1` | 宿主机绑定地址 |
| `HTTP_PORT` | 否 | `8080` | 宿主机访问端口 |
| `SERVER_PORT` | 否 | `8080` | 容器内服务端口 |
| `DB_USER` / `DB_PASSWORD` / `DB_NAME` | 否 | 见 compose | MySQL 配置 |
| `REDIS_PASSWORD` | 否 | 空 | Redis 密码 |
| `HASHIDS_SALT` | 否 | 自动派生 | 短码相关盐值 |
| `CF_ENABLED` / `CF_SITE_KEY` / `CF_SECRET_KEY` | 否 | 关闭 | Cloudflare Turnstile，作为异常访问升级验证 |
| `CAPTCHA_ENABLED` | 否 | `true`（deploy.sh） | 是否启用人机验证 |
| `CAP_SITE_KEY` / `CAP_SECRET_KEY` | 否 | 自动创建 | Cap 站点密钥，Secret 不要公开 |
| `CAP_API_ENDPOINT` | 否 | `/cap/<siteKey>/` | 前端同源 Cap API 路径 |
| `CAP_HTTP_PORT` | 否 | 随机高位端口 | Cap 控制台本机端口，仅绑定 `127.0.0.1` |

通知渠道环境变量包括：

- 飞书：`FEISHU_ENABLED`、`FEISHU_WEBHOOK`、`FEISHU_SECRET`
- Telegram：`TELEGRAM_ENABLED`、`TELEGRAM_BOT_TOKEN`、`TELEGRAM_CHAT_ID`
- 钉钉：`DINGTALK_ENABLED`、`DINGTALK_WEBHOOK`、`DINGTALK_SECRET`
- 企业微信：`WECOM_ENABLED`、`WECOM_WEBHOOK`
- Bark：`BARK_ENABLED`、`BARK_KEY`、`BARK_ENDPOINT`
- Discord：`DISCORD_ENABLED`、`DISCORD_WEBHOOK`
- Email：`EMAIL_ENABLED`、`EMAIL_HOST`、`EMAIL_PORT`、`EMAIL_USER`、`EMAIL_PASS`、`EMAIL_FROM`、`EMAIL_TO`
- 通用 Webhook：`WEBHOOK_ENABLED`、`WEBHOOK_URL`、`WEBHOOK_SECRET`

> 注意：`.env` 和 `configs/config.yaml` 会包含真实密码、Token 和密钥。Docker 构建已通过 `.dockerignore` 排除这些文件，最终镜像只内置 `configs/docker.yaml` 的安全默认值；公开仓库中仍请使用模板文件，不要提交生产值。

---

## 通知与 Telegram 安全说明

当前通知系统只做出站发送：登录事件、管理后缀变更等消息会发到配置的渠道。

Telegram 当前不会接收命令，也没有“通过 Telegram 查询管理后缀”的接口。因此陌生人即使找到你的 Bot，也不能通过本服务主动获取管理入口。

仍需注意：

1. 不要泄露 Bot Token。
2. Telegram 建议使用管理员私聊 Chat ID。
3. 如使用群组，请确保是可信私有群，不要把 Bot 加入公开群。
4. 群内所有可见消息的人都能看到后缀变更通知。
5. 如果未来要实现入站命令，必须加 Chat/User allowlist 和 TOTP 二次校验。

---

## 安全策略

1. **数据加密**
   - 原始 URL 使用 AES-GCM 加密存储。
   - 点击 IP 使用 AES-GCM 加密存储。
   - TOTP secret 使用应用加密封装后存储。
   - API Key 只保存哈希。

2. **访问控制**
   - 管理后台使用动态后缀。
   - `AdminMux` 动态转发当前后缀到内部后台引擎。
   - 内部别名 `/__admin` 禁止外部直连。
   - TOTP 获取和验证接口需要管理员登录后访问。

3. **防护机制**
   - 敏感词 DFA 检测。
   - 根据违规等级封禁 IP。
   - Redis 限速。
   - Cap 自托管验证码默认启用。
   - Cloudflare Turnstile 可选，用于异常访问升级。
   - 安全响应头和 CSP 背景资源白名单。

4. **部署隔离**
   - Go 服务默认只绑定宿主机 `127.0.0.1`。
   - MySQL/Redis 位于 Docker 内部网络。
   - 建议使用反代统一处理 TLS、域名和公网访问。

---

## 项目结构

```text
shortlink/
├── cmd/server/main.go          # 服务入口、路由注册
├── internal/
│   ├── api/                    # 公开/管理/跳转 Handler
│   ├── auth/                   # API Key、Session、TOTP
│   ├── config/                 # 配置加载
│   ├── crypto/                 # 强/弱加密
│   ├── filter/                 # DFA 敏感词引擎
│   ├── middleware/             # 限流、封禁、后缀、认证、安全头
│   ├── model/                  # Gorm 模型
│   ├── notice/                 # 通知渠道 provider
│   ├── repository/             # 数据访问
│   ├── service/                # 业务逻辑
│   └── worker/                 # 点击异步写入
├── web/
│   ├── public/                 # 公开页静态文件
│   └── admin/                  # 管理后台静态文件
├── data/default-words.txt      # 默认敏感词
├── configs/config.yaml         # 本地配置（含敏感值时不要公开）
├── API.md                      # API 文档
├── Dockerfile                  # Go 1.26.5 / Alpine 3.24 多阶段构建，镜像内只复制 configs/docker.yaml
├── docker-compose.yml          # 容器名：shortlink-app、shortlink-mysql、shortlink-redis
└── deploy.sh                   # 一键部署脚本
```


## 新增功能说明

本版本新增/增强了以下能力：

- **智能 URL 标准化**：用户可输入 `example.com/path`，后端会统一规范化为安全的 `https://...`，并拒绝非 HTTP(S)、控制字符、用户信息、内网/本地 IP 等高风险目标。
- **用户自助管理短链**：创建短链后会返回一次性的 `manage_url`，用户可通过该链接修改目标 URL、过期时间、密码、一次性状态、二维码文字或删除短链。
- **TTL 策略**：后台可设置默认 TTL、最大 TTL、是否允许永不过期、前台可选 TTL 列表。
- **风险确认页**：系统会对目标 URL 做风险扫描，可疑链接访问时先显示“即将访问外部链接”确认页，确认后才跳转；一次性链接不会在确认页阶段被消耗。
- **API Key 权限和额度**：API Key 支持用途说明、权限列表和每分钟/每日/每月额度；批量创建按 item 数计入额度。
- **批量创建**：`POST /api/v1/links/batch` 支持一次提交多条短链，逐项返回成功/失败。
- **二维码策略**：后台可配置创建后是否直接显示二维码、是否允许用户自定义文字、默认文字/模板和管理员 Logo URL；用户只能做简单文字定制。
- **增强统计**：点击记录增加 referer host、设备、浏览器、系统、小时分布、匿名访客 hash 等字段，用于后台统计扩展。
- **审计日志**：管理员登录、设置保存、API Key 操作、域名操作、举报处理、解封等关键动作会写入审计日志，不记录密码/token/webhook/TOTP 等敏感值。
- **多域名管理**：后台可维护短链域名列表（`public` / `admin` / `both`、默认域名、HTTPS、启用状态）；当前实现主要提供后台 CRUD 和展示，短链生成仍使用当前请求 Host / `X-Forwarded-Host` 拼接 `short_url`，暂未根据域名表自动选择默认域名、强制 HTTPS 或限制 Host；短码仍全局唯一。

更多接口细节见 [API.md](./API.md)。

---

## 开发命令

```bash
# 构建本地二进制
make build

# 本地运行
make run

# 整理依赖
make tidy

# Docker 开发启动（容器名：shortlink-app、shortlink-mysql、shortlink-redis）
make dev

# 运行测试
go test ./...
```

---

## 性能建议

| 场景 | 配置建议 |
|------|----------|
| 个人/小团队 | 1C2G，Docker Compose 单机部署 |
| 中小项目 | 2C4G，优化 MySQL/Redis 参数，反代加缓存 |
| 高并发 | 多实例横向扩展 Go 服务，外部 MySQL/Redis，前端静态资源 CDN |

---

## 常见问题

### 1. Cap 需要单独反代一个域名吗？

不需要。公网只需要反代 Shortlink 主服务。Cap 的公开组件和 API 走同源 `/cap/` 路径，Shortlink 会在内部把请求代理到 `shortlink-cap:3000`。

### 2. 为什么 Cap 控制台端口是随机的？

Cap 控制台属于管理入口，不建议使用默认 `3000` 暴露到公网。部署脚本会选择随机高位端口，并绑定到 `127.0.0.1`，例如：

```text
http://127.0.0.1:40871
```

如果你需要访问 Cap 控制台，请在服务器本机访问，或使用 SSH 隧道，不建议直接公网开放。

### 3. 部署后怎么确认 Cap 正常？

```bash
# 查看公开配置，确认 captcha_enabled=true 且 cap_api_endpoint 非空
curl http://127.0.0.1:18080/api/config

# 检查 Cap widget 资源
curl -I http://127.0.0.1:18080/cap/assets/widget.js

# Cap challenge 使用 POST
curl -X POST http://127.0.0.1:18080/cap/<siteKey>/challenge \
  -H 'Content-Type: application/json' \
  -d '{}'
```

`<siteKey>` 可以从 `/api/config` 返回的 `cap_api_endpoint` 中看到。

### 4. 管理后台地址在哪里？

管理后台不是固定 `/admin/`。真实入口是动态后缀：

```text
http://127.0.0.1:18080/{admin_suffix}/
```

查看方式：

```bash
docker compose logs app | grep "Admin suffix"
```

如果配置了通知渠道，后缀变更也会发送到通知渠道。

### 5. 可以把 `.env` 和 `configs/config.yaml` 提交到 GitHub 吗？

不可以。它们包含管理员密码、数据库密码、Redis 密码、加密密钥、Cap Secret 等敏感信息。本项目已经在 `.gitignore` 和 `.dockerignore` 中排除了这些文件。

---

## 致谢

本项目的人机验证能力使用并集成了以下优秀开源项目：

- [tiagozip/cap](https://github.com/tiagozip/cap)：自托管、轻量的 Proof-of-Work CAPTCHA。Shortlink 使用 Cap 作为默认日常验证码，并通过同源 `/cap/` 路径集成其 widget、WASM 和验证 API。
- [mortspace/playcaptcha](https://github.com/mortspace/playcaptcha)：一个有趣的交互式验证码项目。本项目调研后认为它更适合作为趣味 UI 灵感，不作为后端可信安全验证主力。

同时感谢 Go、Gin、GORM、Redis、MySQL、Docker 等开源生态。

---

## 许可证

请根据你的实际项目授权补充 LICENSE。

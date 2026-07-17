# Shortlink API 调用指南

本文档按当前代码实现整理。公开接口无需鉴权；`/api/v1` 提供 API Key 创建、批量创建等自动化能力。

## 基础信息

| 项目 | 说明 |
|------|------|
| 基础地址 | `http://your-domain` |
| 数据格式 | `application/json` / `multipart/form-data` |
| API Key Header | `X-API-Key: sl_xxx` |
| 短码规则 | 默认 6 位字母数字，可在后台改为 4-12 位；自定义短码 4-32 位 |
| 错误结构 | `{"code": HTTP状态码, "error": "错误信息"}` |

---

## 公开 API

### 获取公开配置

```http
GET /api/config
```

返回公开页需要的配置，如 CF Turnstile、背景、版本、用户公告等。

**响应示例：**

```json
{
  "code": 200,
  "data": {
    "cf_enabled": false,
    "cf_site_key": "",
    "bg_enabled": false,
    "bg_url": "",
    "bg_type": "image",
    "require_privacy": true,
    "allow_custom": true,
    "version": "1.0.0",
    "favicon_url": "",
    "public_messages": [
      {"title": "公告", "content": "维护通知", "level": "info"}
    ],
    "ttl_default_seconds": 0,
    "ttl_max_seconds": 31536000,
    "ttl_allow_never": true,
    "ttl_options": [0, 3600, 86400, 604800, 2592000],
    "qr_show_direct": true,
    "qr_allow_user_customize": false,
    "qr_default_text": "",
    "qr_default_template": "classic",
    "qr_logo_url": "",
    "qr_logo_enabled": false
  }
}
```

### 获取公开服务状态

```http
GET /api/status
```

用于公开页展示 API、数据库、Redis 综合健康状态。

**响应示例：**

```json
{
  "code": 200,
  "data": {
    "service": "Shortlink",
    "summary": {"available": true, "status": "online", "latency_ms": 5},
    "periods": [
      {
        "key": "1h",
        "label": "1h",
        "available": true,
        "status": "online",
        "latency_ms": 5,
        "uptime_percent": 100,
        "last_checked_min": 0,
        "samples": 60,
        "bars": ["ok", "ok"]
      }
    ]
  }
}
```

### 创建短链

```http
POST /api/links
Content-Type: multipart/form-data 或 application/json
```

**参数：**

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `url` | string | 是 | 原始长链接。支持 `http://` / `https://`，也支持 `example.com/path` 这类无 scheme 输入，后端会补为 `https://...`；会拒绝非 HTTP(S)、用户信息、控制字符、空白字符、内网/本地 IP 等高风险目标 |
| `custom_code` | string | 否 | 自定义短码，长度 4-32 位；需与管理后缀和已有短码不冲突 |
| `password` | string | 否 | 访问密码；设置后访问短链需要输入密码 |
| `ttl` | int | 否 | 过期时间，单位秒；必须大于等于 0；若超过后台配置的最大 TTL 会返回错误 |
| `expires_at` | string | 否 | 过期时间，支持 RFC3339、Go duration 字符串，或在允许永不过期时使用 `"never"`；非空且无法解析时直接返回错误，不会回退到 `ttl` |
| `is_once` | bool | 否 | 一次性链接，成功访问一次后禁用 |
| `privacy_agree` | bool | 视配置 | 开启隐私确认时必须为 `true` |
| `visibility` | int | 否 | 可见性：`0` 私密、`1` 公开、`2` 密码；设置密码后自动为 `2` |
| `qr_text` | string | 否 | 二维码说明文字，最多 120 个字符；仅在后台允许用户自定义二维码时可提交，否则返回 `qr customization is disabled` |
| `qr_template` | string | 否 | 二维码模板，可选 `classic`、`card`、`compact`；未知值会归一化为 `classic` |
| `cf_turnstile_response` | string | 视配置 | 开启 Cloudflare Turnstile 时需要 |

**示例：**

```bash
curl -X POST http://localhost:8080/api/links \
  -F "url=https://example.com/very-long-url" \
  -F "privacy_agree=true"
```

```bash
curl -X POST http://localhost:8080/api/links \
  -F "url=https://example.com/secret-doc" \
  -F "custom_code=mycode" \
  -F "password=secret123" \
  -F "ttl=86400" \
  -F "is_once=true" \
  -F "privacy_agree=true"
```

**响应：**

```json
{
  "code": 200,
  "data": {
    "short_code": "xk3Ab8",
    "short_url": "http://localhost:8080/xk3Ab8",
    "edit_token": "只显示一次的编辑令牌",
    "manage_url": "http://localhost:8080/manage?code=xk3Ab8&token=...",
    "qr_show_direct": true,
    "qr_text": "",
    "qr_template": "classic",
    "qr_logo_enabled": false,
    "qr_logo_url": ""
  }
}
```

### 访问短链

```http
GET /{short_code}
POST /{short_code}
```

- 成功时返回 `302 Found`，跳转到原始 URL。
- 有访问密码时，`GET /{short_code}` 会返回密码输入页；表单提交到 `POST /{short_code}`，验证通过后设置短期访问 Cookie 并跳转。
- 一次性链接在密码校验通过并成功跳转前被消费，之后再次访问返回 404。

### 提交举报

```http
POST /api/report
Content-Type: application/json
```

**请求体：**

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `short_code` | string | 是 | 被举报的短码 |
| `reason` | string | 是 | 举报原因，如 `fraud`、`porn`、`spam`、`other` |
| `custom_text` | string | 否 | 补充说明，最长由前端限制为 500 字符 |
| `cf_turnstile_response` | string | 视配置 | 开启 CF 验证时需要 |

**示例：**

```bash
curl -X POST http://localhost:8080/api/report \
  -H "Content-Type: application/json" \
  -d '{"short_code":"xk3Ab8","reason":"fraud","custom_text":"疑似钓鱼"}'
```

**响应：**

```json
{
  "code": 200,
  "data": {"message": "report submitted, thank you"}
}
```

---

## API Key 鉴权 API

先在管理后台创建 API Key，然后在请求头中携带：

```http
X-API-Key: sl_你的API密钥
```

### 创建短链（API Key）

```http
POST /api/v1/links
Content-Type: application/json
X-API-Key: sl_你的API密钥
```

**请求体：**

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `url` | string | 是 | 原始长链接；同公开创建接口一样会做 URL 标准化和私网/本地域名拦截 |
| `custom_code` | string | 否 | 自定义短码 |
| `password` | string | 否 | 访问密码 |
| `ttl` | int | 否 | 过期秒数 |
| `expires_at` | string | 否 | RFC3339、duration 或允许时的 `"never"` |
| `is_once` | bool | 否 | 一次性链接 |
| `visibility` | int | 否 | 可见性 |
| `qr_text` | string | 否 | 二维码说明文字，最多 120 个字符；仅在后台允许用户自定义二维码时可提交 |
| `qr_template` | string | 否 | 二维码模板，可选 `classic`、`card`、`compact` |

API Key 调用不要求 `privacy_agree`，但必须通过 `X-API-Key` 请求头提交密钥；不支持 URL 查询参数传递密钥。若该 Key 配置了 `allowed_domains` / `denied_domains`，创建和后续管理链接修改目标 URL 时都会检查目标域名。域名列表支持逗号或换行分隔，裸域名会匹配其子域名。

**示例：**

```bash
curl -X POST http://localhost:8080/api/v1/links \
  -H "X-API-Key: sl_abc123def456..." \
  -H "Content-Type: application/json" \
  -d '{
    "url": "https://example.com/very-long-url",
    "custom_code": "myapi",
    "password": "secret",
    "ttl": 604800,
    "is_once": false
  }'
```

**响应：**

```json
{
  "code": 200,
  "data": {
    "short_code": "myapi",
    "short_url": "http://localhost:8080/myapi",
    "edit_token": "只显示一次的编辑令牌",
    "manage_url": "http://localhost:8080/manage?code=myapi&token=...",
    "qr_show_direct": true,
    "qr_text": "",
    "qr_template": "classic",
    "qr_logo_enabled": false,
    "qr_logo_url": ""
  }
}
```

> 注意：API Key 的管理（创建、修改、吊销、删除）在管理后台接口中提供；API Key 调用侧目前支持创建和批量创建。

---

## 管理后台接口概览

管理后台路径为每日/手动轮换的动态后缀：

```text
/{admin_suffix}/
```

后台 API 位于：

```text
/{admin_suffix}/api/...
```

除登录、登出、版本接口外，其余后台接口都需要管理员 Session。

主要接口：

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/{suffix}/api/login` | 管理员登录；TOTP 已启用后需提交验证码 |
| `POST` | `/{suffix}/api/logout` | 登出 |
| `GET` | `/{suffix}/api/version` | 获取版本 |
| `GET` | `/{suffix}/api/totp` | 获取 TOTP 绑定信息（需登录） |
| `POST` | `/{suffix}/api/totp/verify` | 验证并启用 TOTP（需登录） |
| `GET` | `/{suffix}/api/dashboard` | 仪表盘统计 |
| `GET` | `/{suffix}/api/links` | 短链列表 |
| `DELETE` | `/{suffix}/api/links/{code}` | 删除短链 |
| `GET` | `/{suffix}/api/apikeys` | API Key 列表 |
| `POST` | `/{suffix}/api/apikeys` | 创建 API Key |
| `PATCH` | `/{suffix}/api/apikeys/{id}` | 修改 API Key 权限/额度 |
| `POST` | `/{suffix}/api/apikeys/{id}/revoke` | 吊销 API Key |
| `DELETE` | `/{suffix}/api/apikeys/{id}` | 删除 API Key |
| `GET` | `/{suffix}/api/stats/{code}` | 短链统计 |
| `GET` | `/{suffix}/api/settings` | 获取设置 |
| `PUT` | `/{suffix}/api/settings` | 保存设置 |
| `POST` | `/{suffix}/api/settings/rotate-suffix` | 手动轮换管理后缀 |
| `GET` | `/{suffix}/api/bans` | 封禁列表 |
| `DELETE` | `/{suffix}/api/bans/{id}` | 解封 |
| `POST` | `/{suffix}/api/wordfilter/reload` | 重载敏感词 |
| `GET` | `/{suffix}/api/status` | 系统状态 |
| `GET` | `/{suffix}/api/reports` | 举报列表 |
| `POST` | `/{suffix}/api/reports/{id}` | 处理举报 |
| `POST` | `/{suffix}/api/reports/auto-process` | 自动处理举报 |
| `GET` | `/{suffix}/api/audit` | 审计日志 |
| `GET` | `/{suffix}/api/domains` | 域名列表 |
| `POST` | `/{suffix}/api/domains` | 添加域名 |
| `PATCH` | `/{suffix}/api/domains/{id}` | 修改域名 |
| `DELETE` | `/{suffix}/api/domains/{id}` | 删除域名 |


## 新增能力概览

### 创建响应中的管理链接

`POST /api/links` 和 `POST /api/v1/links` 创建成功后会额外返回：

```json
{
  "edit_token": "只显示一次的编辑令牌",
  "manage_url": "http://your-domain/manage?code=abc123&token=..."
}
```

请保存 `manage_url`，后续可用于用户自助修改或删除短链。

### 用户自助管理接口

```http
GET    /api/links/{code}/manage?token=...
PATCH  /api/links/{code}/manage
DELETE /api/links/{code}/manage
```

`PATCH` 请求体示例：

```json
{
  "edit_token": "...",
  "url": "example.org/new",
  "password": "optional",
  "clear_password": false,
  "ttl": 86400,
  "is_once": false,
  "visibility": 1,
  "qr_text": "二维码说明文字",
  "qr_template": "card"
}
```

### API Key 权限与额度

API Key 支持用途、权限和额度字段。当前识别的权限包括：

- `links:create`：创建单条短链
- `links:batch_create`：批量创建短链
- `stats:read_own`：读取自己创建链接的统计（预留/逐步启用）
- `links:delete_own`：删除自己创建的链接（预留/逐步启用）
- `qr:customize`：预留权限；当前实现未单独校验该权限。二维码自定义目前由全局设置 `qr_allow_user_customize` 控制。

额度字段：

- `quota_per_minute`
- `quota_per_day`
- `quota_per_month`

`0` 表示不限。

### 批量创建

```http
POST /api/v1/links/batch
X-API-Key: sl_xxx
Content-Type: application/json
```

请求：

```json
{
  "items": [
    {"url":"example.com/a","custom_code":"a1","ttl":86400,"qr_text":"可选二维码文字","qr_template":"classic"},
    {"url":"example.com/b","password":"secret","expires_at":"never"}
  ],
  "options": {"continue_on_error": true}
}
```

响应逐项返回成功或失败。单批最多 50 条。API Key 额度按 item 数计入；实现会先扣 1 次权限中间件额度，批量处理时再按 `len(items)-1` 补扣，因此总计等于 item 数。

响应示例：

```json
{
  "code": 200,
  "data": {
    "total": 2,
    "created": 1,
    "failed": 1,
    "results": [
      {
        "index": 0,
        "ok": true,
        "short_code": "a1",
        "short_url": "http://localhost:8080/a1",
        "edit_token": "只显示一次的编辑令牌",
        "manage_url": "http://localhost:8080/manage?code=a1&token=...",
        "qr_show_direct": true,
        "qr_text": "可选二维码文字",
        "qr_template": "classic",
        "qr_logo_enabled": false,
        "qr_logo_url": ""
      },
      {"index": 1, "ok": false, "error": "错误信息"}
    ]
  }
}
```

### 风险跳转确认页

系统会在创建时对 URL 做标准化和风险扫描。可疑链接不会直接跳转，而是先显示“即将访问外部链接”确认页；用户确认后才会跳转。一次性链接只会在最终确认跳转时消耗。

### 后台新增接口

```http
GET    /{suffix}/api/audit
GET    /{suffix}/api/domains
POST   /{suffix}/api/domains
PATCH  /{suffix}/api/domains/{id}
DELETE /{suffix}/api/domains/{id}
PATCH  /{suffix}/api/apikeys/{id}
POST   /{suffix}/api/apikeys/{id}/revoke
```

后台设置新增 TTL 策略、风险域名策略、二维码策略等字段，并通过 `/api/config` 下发公开安全配置。

### TTL 与二维码策略

后台设置会通过 `/api/config` 下发 TTL 和二维码策略：

- TTL 选项会自动去重、排序、移除负数，并按最大 TTL 裁剪。
- `ttl_allow_never=false` 时，`0`（永不过期）不会出现在前台选项中，也不能通过 API 创建永久链接。
- 如果禁止永久链接但未设置默认 TTL，系统会使用最小的正数 TTL 选项作为默认值；如果没有任何正数选项，则创建请求会失败。
- 二维码文字最长 120 字。
- `qr_allow_user_customize=false` 时，用户/API 不能提交自定义 `qr_text` / `qr_template`，但后台默认二维码文字仍会生效。
- `qr_show_direct=false` 只控制创建成功后是否直接显示二维码，不会删除已保存的二维码元数据。
- `qr_logo_url` 只有在 `qr_logo_enabled=true` 且 URL 非空时才会返回给公开页。

### 多域名管理

域名对象字段：

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | uint64 | 域名记录 ID |
| `hostname` | string | 域名，保存时会转为小写并去除首尾空白 |
| `purpose` | string | 用途，建议值：`public`、`admin`、`both`；创建时为空则默认为 `public` |
| `is_default` | bool | 是否默认域名 |
| `force_https` | bool | 是否标记为强制 HTTPS |
| `enabled` | bool | 是否启用 |

当前限制：域名表目前主要用于后台维护和展示；创建短链返回的 `short_url` 仍根据当前请求 Host / `X-Forwarded-Host` 生成，暂未根据 `is_default` 自动选择域名，也暂未根据 `force_https` 强制改写 scheme 或限制请求 Host。短码仍全局唯一。

---

## 错误码说明

| HTTP 状态码 | 说明 | 常见原因 |
|-------------|------|----------|
| `400` | 请求参数错误 | URL 格式不正确、未同意隐私规则、短码冲突、敏感词命中 |
| `401` | 未授权 | API Key 无效、管理员登录失败、Session 失效 |
| `403` | 禁止访问 | IP 被封禁、CF Turnstile 验证失败 |
| `404` | 资源不存在 | 短码不存在、已过期、一次性链接已使用 |
| `413` | 请求体过大 | 超过请求体大小限制 |
| `429` | 请求过于频繁 | 创建或跳转触发限流 |
| `500` | 服务器内部错误 | 数据库、缓存、加密或其他服务异常 |

---

## 最佳实践

1. **API Key 安全**：Key 仅在创建时展示一次，丢失后需重新生成。
2. **短码选择**：自定义短码不要使用可猜测的敏感词或管理后缀。
3. **一次性链接**：适合临时验证码、临时文件等低频场景。
4. **过期时间**：API 推荐使用 `ttl`；需要精确时间时使用 RFC3339 `expires_at`。
5. **密码保护**：密码只保存 bcrypt 哈希，不会明文存储。
6. **举报处理**：管理员可在后台查看、处理或自动处理用户举报。
7. **限速与封禁**：默认创建每分钟 10 次、跳转每分钟 60 次，可在后台调整。

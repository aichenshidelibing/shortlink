#!/usr/bin/env bash
set -e
umask 077

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; BLUE='\033[0;34m'; NC='\033[0m'
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}   Shortlink 一键部署脚本${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

# ── Registry mirrors ──
DOCKER_MIRRORS=(
    "https://docker.m.daocloud.io"
    "https://dockerhub.timeweb.cloud"
    "https://docker.1ms.run"
    "https://docker.xuanyuan.me"
)

sanitize() { echo "$1" | tr -cd '[:alnum:]._-'; }

validate_port() {
    local p="$1"
    [[ "$p" =~ ^[0-9]+$ ]] && [ "$p" -ge 1 ] && [ "$p" -le 65535 ]
}

detect_compose() {
    if docker compose version >/dev/null 2>&1; then
        COMPOSE_CMD="docker compose"
        return 0
    fi
    if command -v docker-compose >/dev/null 2>&1 && docker-compose version >/dev/null 2>&1; then
        COMPOSE_CMD="docker-compose"
        return 0
    fi
    echo -e "${RED}Docker Compose 未安装或不可用，请安装 Compose v2 插件后重试${NC}"
    echo "  apt-get install docker-compose-v2"
    exit 1
}

ensure_docker() {
    if command -v docker >/dev/null 2>&1; then
        detect_compose
        return 0
    fi

    echo -e "${YELLOW}Docker 未安装，尝试自动安装...${NC}"
    if command -v snap >/dev/null 2>&1 && sudo snap install docker 2>/dev/null; then
        echo -e "${GREEN}✓ Docker 已通过 snap 安装${NC}"
        detect_compose
        return 0
    fi
    if command -v apt-get >/dev/null 2>&1 && sudo apt-get update -qq && sudo apt-get install -y -qq docker.io docker-compose-v2 2>/dev/null; then
        echo -e "${GREEN}✓ Docker 已通过 apt 安装${NC}"
        detect_compose
        return 0
    fi
    if command -v curl >/dev/null 2>&1 && curl -fsSL https://get.docker.com | sudo sh 2>/dev/null; then
        echo -e "${GREEN}✓ Docker 已通过官方脚本安装${NC}"
        detect_compose
        return 0
    fi

    echo -e "${RED}无法自动安装 Docker，请手动安装后重试${NC}"
    echo "  curl -fsSL https://get.docker.com | sudo sh"
    exit 1
}

configure_mirror() {
    local dj="/etc/docker/daemon.json" mirrors_json
    mirrors_json=$(printf '%s\n' "${DOCKER_MIRRORS[@]}" | python3 -c 'import json,sys; print(json.dumps([line.strip() for line in sys.stdin if line.strip()]))')

    echo -e "${YELLOW}>>> 配置 Docker 镜像加速...${NC}"
    if [ -w "$dj" ] || [ -w "$(dirname "$dj")" ] 2>/dev/null; then
        sudo mkdir -p /etc/docker 2>/dev/null || true
        MIRRORS_JSON="$mirrors_json" DJ="$dj" python3 - <<'PY' | sudo tee "$dj" >/dev/null 2>&1 && \
            echo -e "${GREEN}✓ 镜像加速已配置${NC}" && \
            sudo systemctl restart docker 2>/dev/null || true
import json, os
from pathlib import Path

path = Path(os.environ['DJ'])
try:
    data = json.loads(path.read_text()) if path.exists() and path.read_text().strip() else {}
except Exception:
    data = {}
if not isinstance(data, dict):
    data = {}
data['registry-mirrors'] = json.loads(os.environ['MIRRORS_JSON'])
print(json.dumps(data, indent=2, ensure_ascii=False))
PY
    else
        echo -e "${YELLOW}⚠ 无权限配置镜像加速（非 root），跳过${NC}"
    fi
}

pull_retry() {
    local img="$1" max=3 delay=5
    echo -ne "${BLUE}拉取 ${img} ...${NC}"
    for ((i=1; i<=max; i++)); do
        if docker pull "$img" 2>/dev/null; then
            echo -e " ${GREEN}✓${NC}"
            return 0
        fi
        [ $i -lt $max ] && sleep $delay && delay=$((delay*2))
    done
    echo -e " ${RED}✗${NC}"
    return 1
}

pull_images() {
    echo -e "${YELLOW}>>> 预拉取基础镜像...${NC}"
    local ok=true
    for img in "golang:1.26.5-alpine3.24" "mysql:8.4" "redis:8.8.0-alpine"; do
        pull_retry "$img" || ok=false
    done
    $ok || echo -e "${YELLOW}部分镜像拉取失败，构建时会自动重试${NC}"
    echo ""
}

genkey() { openssl rand -base64 32 2>/dev/null || head -c32 /dev/urandom | base64 | tr -d '\n'; }
gencode() { printf '%06d' "$((10#$(od -An -N3 -tu4 /dev/urandom 2>/dev/null | tr -d ' ') % 1000000))"; }
random_port() {
    local p
    while true; do
        p=$((20000 + $(od -An -N2 -tu2 /dev/urandom 2>/dev/null | tr -d ' ') % 40000))
        if ! ss -ltn 2>/dev/null | grep -qE ":${p}[[:space:]]"; then
            echo "$p"
            return 0
        fi
    done
}

ask() {
    local text="$1" def="$2" is_secret="${3:-false}"
    [ -n "$def" ] && text="${text} [默认: ${def}]"
    text="${text}: "
    local val
    if [ "$is_secret" = "true" ]; then
        read -rs -p "$text" val; echo "" >&2
    else
        read -r -p "$text" val
    fi
    [ -z "$val" ] && val="$def"
    echo "$val"
}

ask_yes_no() {
    local text="$1" def="${2:-n}" ans
    while true; do
        ans=$(ask "$text (y/n)" "$def")
        case "${ans,,}" in
            y|yes) return 0 ;;
            n|no) return 1 ;;
            *) echo -e "${RED}请输入 y 或 n${NC}" ;;
        esac
    done
}

urlencode() {
    local s="$1" out="" i c hex
    for ((i=0; i<${#s}; i++)); do
        c=${s:i:1}
        case "$c" in
            [a-zA-Z0-9.~_-]) out+="$c" ;;
            *) printf -v hex '%%%02X' "'$c"; out+="$hex" ;;
        esac
    done
    echo "$out"
}

send_feishu() {
    local webhook="$1" secret="$2" msg="$3" timestamp sign payload
    timestamp=$(date +%s)
    sign=""
    if [ -n "$secret" ]; then
        local sign_key
        sign_key=$(printf '%s\n%s' "$timestamp" "$secret")
        sign=$(printf '' | openssl dgst -sha256 -hmac "$sign_key" -binary | base64 | tr -d '\n')
    fi
    payload=$(printf '{"timestamp":%s,"sign":"%s","msg_type":"text","content":{"text":"%s"}}' "$timestamp" "$sign" "$(json_escape "$msg")")
    curl -fsS -X POST "$webhook" -H 'Content-Type: application/json' -d "$payload" >/dev/null
}

send_telegram() {
    local token="$1" chat="$2" msg="$3" payload
    payload=$(printf '{"chat_id":"%s","text":"%s","parse_mode":"HTML"}' "$(json_escape "$chat")" "$(json_escape "$msg")")
    curl -fsS -X POST "https://api.telegram.org/bot${token}/sendMessage" -H 'Content-Type: application/json' -d "$payload" >/dev/null
}

send_dingtalk() {
    local webhook="$1" secret="$2" msg="$3" url timestamp sign payload
    url="$webhook"
    if [ -n "$secret" ]; then
        timestamp=$(date +%s%3N)
        sign=$(printf '%s\n%s' "$timestamp" "$secret" | openssl dgst -sha256 -hmac "$secret" -binary | base64 | tr -d '\n')
        url="${webhook}&timestamp=${timestamp}&sign=$(urlencode "$sign")"
    fi
    payload=$(printf '{"msgtype":"text","text":{"content":"%s"}}' "$(json_escape "$msg")")
    curl -fsS -X POST "$url" -H 'Content-Type: application/json' -d "$payload" >/dev/null
}

send_wecom() {
    local webhook="$1" msg="$2" payload
    payload=$(printf '{"msgtype":"text","text":{"content":"%s"}}' "$(json_escape "$msg")")
    curl -fsS -X POST "$webhook" -H 'Content-Type: application/json' -d "$payload" >/dev/null
}

send_bark() {
    local key="$1" endpoint="$2" msg="$3"
    [ -z "$endpoint" ] && endpoint="https://api.day.app"
    endpoint="${endpoint%/}"
    curl -fsS "${endpoint}/$(urlencode "$key")/$(urlencode "$msg")" >/dev/null
}

send_discord() {
    local webhook="$1" msg="$2" payload
    payload=$(printf '{"content":"%s"}' "$(json_escape "$msg")")
    curl -fsS -X POST "$webhook" -H 'Content-Type: application/json' -d "$payload" >/dev/null
}

send_webhook() {
    local webhook="$1" secret="$2" msg="$3" payload
    payload=$(printf '{"text":"%s"}' "$(json_escape "$msg")")
    if [ -n "$secret" ]; then
        curl -fsS -X POST "$webhook" -H 'Content-Type: application/json' -H "X-Webhook-Secret: ${secret}" -d "$payload" >/dev/null
    else
        curl -fsS -X POST "$webhook" -H 'Content-Type: application/json' -d "$payload" >/dev/null
    fi
}

send_email() {
    local host="$1" port="$2" user="$3" pass="$4" from="$5" to="$6" msg="$7"
    command -v python3 >/dev/null || { echo "需要 python3 才能测试邮件渠道" >&2; return 1; }
    EMAIL_HOST="$host" EMAIL_PORT="${port:-587}" EMAIL_USER="$user" EMAIL_PASS="$pass" EMAIL_FROM="$from" EMAIL_TO="$to" EMAIL_MSG="$msg" python3 - <<'PY'
import os, smtplib
from email.message import EmailMessage
msg = EmailMessage()
msg['Subject'] = '【Shortlink】部署通知测试'
msg['From'] = os.environ['EMAIL_FROM']
msg['To'] = os.environ['EMAIL_TO']
msg.set_content(os.environ['EMAIL_MSG'])
with smtplib.SMTP(os.environ['EMAIL_HOST'], int(os.environ.get('EMAIL_PORT') or '587'), timeout=15) as s:
    s.starttls()
    user = os.environ.get('EMAIL_USER') or ''
    if user:
        s.login(user, os.environ.get('EMAIL_PASS') or '')
    s.send_message(msg)
PY
}

json_escape() {
    local s="$1"
    s=${s//\\/\\\\}; s=${s//\"/\\\"}; s=${s//$'\n'/\\n}; s=${s//$'\r'/}
    echo "$s"
}

dotenv_quote() {
    local s="$1"
    s=${s//$'\r'/}
    s=${s//\\/\\\\}; s=${s//\"/\\\"}; s=${s//\$/\\$}; s=${s//\`/\\\`}; s=${s//$'\n'/\\n}
    printf '"%s"' "$s"
}

yaml_quote() {
    local s="$1"
    s=${s//$'\r'/}
    s=${s//\\/\\\\}; s=${s//\"/\\\"}; s=${s//$'\n'/\\n}
    printf '"%s"' "$s"
}

write_env_var() { printf '%s=%s\n' "$1" "$(dotenv_quote "$2")"; }
write_env_raw() { printf '%s=%s\n' "$1" "$2"; }

load_existing_env() {
    local file="$1" line key val
    [ -f "$file" ] || return 0
    while IFS= read -r line || [ -n "$line" ]; do
        line=${line%$'\r'}
        [[ "$line" =~ ^[[:space:]]*$ ]] && continue
        [[ "$line" =~ ^[[:space:]]*# ]] && continue
        [[ "$line" == *=* ]] || continue
        key=${line%%=*}
        val=${line#*=}
        key=$(sanitize "$key")
        case "$key" in
            ADMIN_USERNAME|ADMIN_PASSWORD|ENCRYPTION_KEY|ADMIN_SESSION_SECRET|SERVER_PORT|SERVER_MODE|HTTP_BIND|HTTP_PORT|DB_USER|DB_PASSWORD|DB_NAME|DB_ROOT_PASSWORD|REDIS_PASSWORD|HASHIDS_SALT|SNOWFLAKE_WORKER_ID|SNOWFLAKE_DATACENTER_ID|CAPTCHA_ENABLED|CAPTCHA_PROVIDER|CAPTCHA_MODE|CAPTCHA_NORMAL_PROVIDER|CAPTCHA_ESCALATION_PROVIDER|CAPTCHA_FAILURE_THRESHOLD|CAPTCHA_RISK_WINDOW_SECONDS|CAP_SITE_KEY|CAP_SECRET_KEY|CAP_VERIFY_URL|CAP_API_ENDPOINT|CAP_ADMIN_KEY|CAP_HTTP_BIND|CAP_HTTP_PORT|CAP_INTERNAL_URL|FEISHU_ENABLED|FEISHU_WEBHOOK|FEISHU_SECRET|TELEGRAM_ENABLED|TELEGRAM_BOT_TOKEN|TELEGRAM_CHAT_ID|DINGTALK_ENABLED|DINGTALK_WEBHOOK|DINGTALK_SECRET|WECOM_ENABLED|WECOM_WEBHOOK|BARK_ENABLED|BARK_KEY|BARK_ENDPOINT|DISCORD_ENABLED|DISCORD_WEBHOOK|EMAIL_ENABLED|EMAIL_HOST|EMAIL_PORT|EMAIL_USER|EMAIL_PASS|EMAIL_FROM|EMAIL_TO|WEBHOOK_ENABLED|WEBHOOK_URL|WEBHOOK_SECRET)
                ;;
            *) continue ;;
        esac
        if [[ "$val" == \"*\" && "$val" == *\" ]]; then
            val=${val:1:${#val}-2}
            val=${val//\\n/$'\n'}; val=${val//\\\"/\"}; val=${val//\\\$/\$}; val=${val//\\\`/\`}; val=${val//\\\\/\\}
        fi
        printf -v "$key" '%s' "$val"
    done < "$file"
}

verify_code() {
    local sender_desc="$1" code="$2" input
    echo -e "${BLUE}已发送测试消息到 ${sender_desc}${NC}"
    input=$(ask "请输入消息中的 6 位验证码" "")
    if [ "$input" != "$code" ]; then
        echo -e "${RED}验证码不匹配，通知渠道验证失败${NC}"
        return 1
    fi
    echo -e "${GREEN}✓ 通知渠道验证通过${NC}"
}

cap_login() {
    local base="$1" admin_key="$2"
    curl -fsS -X POST "${base%/}/auth/login" \
        -H 'Content-Type: application/json' \
        -d "{\"admin_key\":\"$(json_escape "$admin_key")\"}" 2>/dev/null
}

cap_create_key() {
    local base="$1" token="$2" hash="$3" name="$4" origin="$5" auth body
    auth=$(printf '{"token":"%s","hash":"%s"}' "$(json_escape "$token")" "$(json_escape "$hash")" | base64 | tr -d '\n')
    body="{\"name\":\"$(json_escape "$name")\",\"instrumentation\":true,\"blockAutomatedBrowsers\":false"
    if [ -n "$origin" ]; then
        body="${body},\"corsOrigins\":[\"$(json_escape "$origin")\"]"
    fi
    body="${body}}"
    curl -fsS -X POST "${base%/}/server/keys" \
        -H "Authorization: Bearer ${auth}" \
        -H 'Content-Type: application/json' \
        -d "$body" 2>/dev/null
}

extract_json_string() {
    python3 -c 'import json,sys; data=json.load(sys.stdin); print(data.get(sys.argv[1], ""))' "$1" 2>/dev/null
}

cap_key_ready() {
    local base="http://127.0.0.1:${CAP_HTTP_PORT}"
    [ -n "${CAP_SITE_KEY:-}" ] || return 1
    curl -fsS "${base}/${CAP_SITE_KEY}/challenge" >/dev/null 2>&1
}

get_current_admin_suffix() {
    local line
    line=$( ($COMPOSE_CMD logs --no-color app 2>/dev/null || true) | grep '"msg":"Admin suffix"' | tail -n1 || true )
    [ -n "$line" ] || return 1
    printf '%s' "$line" | python3 -c 'import json,sys
line=sys.stdin.read().strip()
if not line:
    raise SystemExit(1)
try:
    print(json.loads(line).get("suffix",""))
except Exception:
    raise SystemExit(1)'
}

auto_configure_cap() {
    local base="http://127.0.0.1:${CAP_HTTP_PORT}" origin="$1" login token hash created site secret
    echo -e "${YELLOW}>>> 自动初始化 Cap Site Key...${NC}"
    for _ in 1 2 3 4 5 6 7 8 9 10; do
        if curl -fsS "${base}/" >/dev/null 2>&1; then break; fi
        sleep 2
    done
    login=$(cap_login "$base" "$CAP_ADMIN_KEY" || true)
    token=$(printf '%s' "$login" | extract_json_string session_token)
    hash=$(printf '%s' "$login" | extract_json_string hashed_token)
    if [ -z "$token" ] || [ -z "$hash" ]; then
        echo -e "${YELLOW}⚠ Cap 登录接口暂不可用，保留控制台手动入口${NC}"
        return 1
    fi
    created=$(cap_create_key "$base" "$token" "$hash" "Shortlink" "$origin" || true)
    site=$(printf '%s' "$created" | extract_json_string siteKey)
    secret=$(printf '%s' "$created" | extract_json_string secretKey)
    if [ -z "$site" ] || [ -z "$secret" ]; then
        echo -e "${YELLOW}⚠ Cap key 创建接口未返回密钥，保留控制台手动入口${NC}"
        return 1
    fi
    CAP_SITE_KEY="$site"
    CAP_SECRET_KEY="$secret"
    CAP_VERIFY_URL="http://cap:3000/${CAP_SITE_KEY}/siteverify"
    CAP_API_ENDPOINT="/cap/${CAP_SITE_KEY}/"
    echo -e "${GREEN}✓ Cap Site Key 已自动创建并写回配置${NC}"
    return 0
}

test_channel() {
    local channel="$1" code msg
    code=$(gencode)
    msg="【Shortlink】部署通知测试\n验证码：${code}\n收到并输入验证码后才会继续部署。"
    case "$channel" in
        feishu) send_feishu "$FEISHU_WEBHOOK" "$FEISHU_SECRET" "$msg" && verify_code "飞书" "$code" ;;
        telegram) send_telegram "$TELEGRAM_BOT_TOKEN" "$TELEGRAM_CHAT_ID" "$msg" && verify_code "Telegram" "$code" ;;
        dingtalk) send_dingtalk "$DINGTALK_WEBHOOK" "$DINGTALK_SECRET" "$msg" && verify_code "钉钉" "$code" ;;
        wecom) send_wecom "$WECOM_WEBHOOK" "$msg" && verify_code "企业微信" "$code" ;;
        bark) send_bark "$BARK_KEY" "$BARK_ENDPOINT" "$msg" && verify_code "Bark" "$code" ;;
        discord) send_discord "$DISCORD_WEBHOOK" "$msg" && verify_code "Discord" "$code" ;;
        email) send_email "$EMAIL_HOST" "$EMAIL_PORT" "$EMAIL_USER" "$EMAIL_PASS" "$EMAIL_FROM" "$EMAIL_TO" "$msg" && verify_code "Email" "$code" ;;
        webhook) send_webhook "$WEBHOOK_URL" "$WEBHOOK_SECRET" "$msg" && verify_code "Webhook" "$code" ;;
        *) return 1 ;;
    esac
}

configure_notifications() {
    FEISHU_ENABLED=false; TELEGRAM_ENABLED=false; DINGTALK_ENABLED=false; WECOM_ENABLED=false
    BARK_ENABLED=false; DISCORD_ENABLED=false; EMAIL_ENABLED=false; WEBHOOK_ENABLED=false

    echo ""
    echo -e "${YELLOW}>>> 通知渠道配置${NC}"
    echo "管理后缀轮换、登录告警等会发送到通知渠道。建议至少配置一个。"
    echo "可选渠道：1) 飞书  2) Telegram  3) 钉钉  4) 企业微信  5) Bark  6) Discord  7) Email  8) Webhook  0) 跳过"

    local choice ok=false
    while [ "$ok" = "false" ]; do
        choice=$(ask "请选择通知渠道编号" "1")
        case "$choice" in
            1|feishu|飞书)
                FEISHU_WEBHOOK=$(ask "飞书 Webhook URL" "${FEISHU_WEBHOOK:-}")
                FEISHU_SECRET=$(ask "飞书签名 Secret（可选）" "${FEISHU_SECRET:-}" "true")
                [ -n "$FEISHU_WEBHOOK" ] || { echo -e "${RED}Webhook 不能为空${NC}"; continue; }
                FEISHU_ENABLED=true; test_channel feishu && ok=true ;;
            2|telegram|Telegram)
                echo -e "${YELLOW}Telegram 安全提示：请使用私聊 Chat ID 或可信私有群，不要把 Bot 拉入公开群；本项目不会接收 Telegram 命令，陌生人无法通过 Bot 查询后缀。${NC}"
                TELEGRAM_BOT_TOKEN=$(ask "Telegram Bot Token" "${TELEGRAM_BOT_TOKEN:-}" "true")
                TELEGRAM_CHAT_ID=$(ask "Telegram Chat ID" "${TELEGRAM_CHAT_ID:-}")
                [ -n "$TELEGRAM_BOT_TOKEN" ] && [ -n "$TELEGRAM_CHAT_ID" ] || { echo -e "${RED}Bot Token 和 Chat ID 不能为空${NC}"; continue; }
                TELEGRAM_ENABLED=true; test_channel telegram && ok=true ;;
            3|dingtalk|钉钉)
                DINGTALK_WEBHOOK=$(ask "钉钉 Webhook URL" "${DINGTALK_WEBHOOK:-}")
                DINGTALK_SECRET=$(ask "钉钉签名 Secret（可选）" "${DINGTALK_SECRET:-}" "true")
                [ -n "$DINGTALK_WEBHOOK" ] || { echo -e "${RED}Webhook 不能为空${NC}"; continue; }
                DINGTALK_ENABLED=true; test_channel dingtalk && ok=true ;;
            4|wecom|企业微信)
                WECOM_WEBHOOK=$(ask "企业微信机器人 Webhook URL" "${WECOM_WEBHOOK:-}")
                [ -n "$WECOM_WEBHOOK" ] || { echo -e "${RED}Webhook 不能为空${NC}"; continue; }
                WECOM_ENABLED=true; test_channel wecom && ok=true ;;
            5|bark|Bark)
                BARK_KEY=$(ask "Bark Key" "${BARK_KEY:-}")
                BARK_ENDPOINT=$(ask "Bark Endpoint" "${BARK_ENDPOINT:-https://api.day.app}")
                [ -n "$BARK_KEY" ] || { echo -e "${RED}Bark Key 不能为空${NC}"; continue; }
                BARK_ENABLED=true; test_channel bark && ok=true ;;
            6|discord|Discord)
                DISCORD_WEBHOOK=$(ask "Discord Webhook URL" "${DISCORD_WEBHOOK:-}")
                [ -n "$DISCORD_WEBHOOK" ] || { echo -e "${RED}Webhook 不能为空${NC}"; continue; }
                DISCORD_ENABLED=true; test_channel discord && ok=true ;;
            7|email|Email)
                EMAIL_HOST=$(ask "SMTP Host" "${EMAIL_HOST:-}")
                EMAIL_PORT=$(ask "SMTP Port" "${EMAIL_PORT:-587}")
                EMAIL_USER=$(ask "SMTP Username" "${EMAIL_USER:-}")
                EMAIL_PASS=$(ask "SMTP Password" "${EMAIL_PASS:-}" "true")
                EMAIL_FROM=$(ask "发件人 From" "${EMAIL_FROM:-$EMAIL_USER}")
                EMAIL_TO=$(ask "收件人 To（多个用逗号分隔）" "${EMAIL_TO:-}")
                [ -n "$EMAIL_HOST" ] && [ -n "$EMAIL_TO" ] && [ -n "$EMAIL_FROM" ] || { echo -e "${RED}Host/From/To 不能为空${NC}"; continue; }
                EMAIL_ENABLED=true; test_channel email && ok=true ;;
            8|webhook|Webhook)
                WEBHOOK_URL=$(ask "通用 Webhook URL" "${WEBHOOK_URL:-}")
                WEBHOOK_SECRET=$(ask "X-Webhook-Secret（可选）" "${WEBHOOK_SECRET:-}" "true")
                [ -n "$WEBHOOK_URL" ] || { echo -e "${RED}Webhook URL 不能为空${NC}"; continue; }
                WEBHOOK_ENABLED=true; test_channel webhook && ok=true ;;
            0|skip|跳过)
                if ask_yes_no "跳过通知会导致后缀变更只能从日志/后台查看，确认跳过吗" "n"; then
                    ok=true
                fi ;;
            *) echo -e "${RED}无效选择${NC}" ;;
        esac
    done
}

main() {
    ensure_docker
    echo -e "${GREEN}Docker: $(docker --version)${NC}"
    echo ""

    configure_mirror
    pull_images
    for img in "tiago2/cap:latest" "valkey/valkey:9-alpine"; do
        pull_retry "$img" || echo -e "${YELLOW}${img} 拉取失败，启动时会自动重试${NC}"
    done
    echo ""

    echo -e "${YELLOW}>>> 生成安全密钥...${NC}"
    if [ -f .env ]; then
        echo -e "${BLUE}   检测到既有 .env — 安全解析并复用其中的密钥/密码（如需重置请先 ${COMPOSE_CMD} down -v）${NC}"
        load_existing_env .env
    fi
    ENCRYPTION_KEY="${ENCRYPTION_KEY:-$(genkey)}"
    SESSION_SECRET="${ADMIN_SESSION_SECRET:-$(genkey)}"
    HASHIDS_SALT="${HASHIDS_SALT:-$(genkey)}"
    DB_PASSWORD="${DB_PASSWORD:-$(genkey)}"
    DB_ROOT_PASSWORD="${DB_ROOT_PASSWORD:-$(genkey)}"
    REDIS_PASSWORD="${REDIS_PASSWORD:-$(genkey)}"
    CAP_ADMIN_KEY="${CAP_ADMIN_KEY:-$(genkey)}"
    echo -e "${GREEN}✓ 密钥已就绪${NC}"
    echo ""

    echo -e "${YELLOW}>>> 必要配置${NC}"
    while true; do
        ADMIN_PASSWORD=$(ask "管理员密码 (用户名admin)" "" "true")
        [ -n "$ADMIN_PASSWORD" ] && break
        echo -e "${RED}密码不能为空${NC}"
    done

    while true; do
        PORT=$(ask "对外访问端口" "8080")
        PORT=$(sanitize "$PORT")
        validate_port "$PORT" && break
        echo -e "${RED}无效端口，请输入 1-65535${NC}"
    done

    DB_USER=$(ask "数据库用户名" "${DB_USER:-shortlink}")
    DB_NAME=$(ask "数据库名称" "${DB_NAME:-shortlink}")
    if [ -z "${CAP_HTTP_PORT:-}" ] || [ "${CAP_HTTP_PORT:-}" = "3000" ]; then
        CAP_HTTP_PORT="$(random_port)"
    fi
    CAP_HTTP_PORT=$(ask "Cap 本地管理端口（仅绑定 127.0.0.1，回车使用随机安全端口）" "$CAP_HTTP_PORT")
    CAP_HTTP_PORT=$(sanitize "$CAP_HTTP_PORT")
    validate_port "$CAP_HTTP_PORT" || CAP_HTTP_PORT="$(random_port)"
    CAP_HTTP_BIND="${CAP_HTTP_BIND:-127.0.0.1}"
    CAP_INTERNAL_URL="${CAP_INTERNAL_URL:-http://cap:3000}"
    CAP_API_ENDPOINT="${CAP_API_ENDPOINT:-/cap/${CAP_SITE_KEY:-}/}"
    CAP_VERIFY_URL="${CAP_VERIFY_URL:-http://cap:3000/${CAP_SITE_KEY:-}/siteverify}"
    configure_notifications

    echo -e "${YELLOW}>>> 写入 .env ...${NC}"
    {
        echo "# Shortlink — $(date '+%Y-%m-%d %H:%M:%S')"
        write_env_var ADMIN_USERNAME admin
        write_env_var ADMIN_PASSWORD "$ADMIN_PASSWORD"
        write_env_var ENCRYPTION_KEY "$ENCRYPTION_KEY"
        write_env_var ADMIN_SESSION_SECRET "$SESSION_SECRET"
        write_env_raw SERVER_PORT "$PORT"
        write_env_var SERVER_MODE release
        echo "# Loopback-only. The container listens on 127.0.0.1:HTTP_PORT of the host —"
        echo "# put nginx / a reverse proxy in front of it before exposing publicly."
        write_env_var HTTP_BIND 127.0.0.1
        write_env_raw HTTP_PORT "$PORT"
        write_env_var DB_USER "$DB_USER"
        write_env_var DB_PASSWORD "$DB_PASSWORD"
        write_env_var DB_NAME "$DB_NAME"
        write_env_var DB_ROOT_PASSWORD "$DB_ROOT_PASSWORD"
        write_env_var REDIS_PASSWORD "$REDIS_PASSWORD"
        write_env_var HASHIDS_SALT "$HASHIDS_SALT"
        write_env_raw SNOWFLAKE_WORKER_ID 1
        write_env_raw SNOWFLAKE_DATACENTER_ID 1
        write_env_raw CAPTCHA_ENABLED "${CAPTCHA_ENABLED:-true}"
        write_env_var CAPTCHA_PROVIDER "${CAPTCHA_PROVIDER:-cap}"
        write_env_var CAPTCHA_MODE "${CAPTCHA_MODE:-adaptive}"
        write_env_var CAPTCHA_NORMAL_PROVIDER "${CAPTCHA_NORMAL_PROVIDER:-cap}"
        write_env_var CAPTCHA_ESCALATION_PROVIDER "${CAPTCHA_ESCALATION_PROVIDER:-turnstile}"
        write_env_raw CAPTCHA_FAILURE_THRESHOLD "${CAPTCHA_FAILURE_THRESHOLD:-3}"
        write_env_raw CAPTCHA_RISK_WINDOW_SECONDS "${CAPTCHA_RISK_WINDOW_SECONDS:-600}"
        write_env_var CAP_SITE_KEY "${CAP_SITE_KEY:-}"
        write_env_var CAP_SECRET_KEY "${CAP_SECRET_KEY:-}"
        write_env_var CAP_VERIFY_URL "${CAP_VERIFY_URL:-}"
        write_env_var CAP_API_ENDPOINT "${CAP_API_ENDPOINT:-}"
        write_env_var CAP_ADMIN_KEY "$CAP_ADMIN_KEY"
        write_env_var CAP_HTTP_BIND "$CAP_HTTP_BIND"
        write_env_raw CAP_HTTP_PORT "$CAP_HTTP_PORT"
        write_env_var CAP_INTERNAL_URL "$CAP_INTERNAL_URL"
        write_env_raw FEISHU_ENABLED "$FEISHU_ENABLED"
        write_env_var FEISHU_WEBHOOK "${FEISHU_WEBHOOK:-}"
        write_env_var FEISHU_SECRET "${FEISHU_SECRET:-}"
        write_env_raw TELEGRAM_ENABLED "$TELEGRAM_ENABLED"
        write_env_var TELEGRAM_BOT_TOKEN "${TELEGRAM_BOT_TOKEN:-}"
        write_env_var TELEGRAM_CHAT_ID "${TELEGRAM_CHAT_ID:-}"
        write_env_raw DINGTALK_ENABLED "$DINGTALK_ENABLED"
        write_env_var DINGTALK_WEBHOOK "${DINGTALK_WEBHOOK:-}"
        write_env_var DINGTALK_SECRET "${DINGTALK_SECRET:-}"
        write_env_raw WECOM_ENABLED "$WECOM_ENABLED"
        write_env_var WECOM_WEBHOOK "${WECOM_WEBHOOK:-}"
        write_env_raw BARK_ENABLED "$BARK_ENABLED"
        write_env_var BARK_KEY "${BARK_KEY:-}"
        write_env_var BARK_ENDPOINT "${BARK_ENDPOINT:-}"
        write_env_raw DISCORD_ENABLED "$DISCORD_ENABLED"
        write_env_var DISCORD_WEBHOOK "${DISCORD_WEBHOOK:-}"
        write_env_raw EMAIL_ENABLED "$EMAIL_ENABLED"
        write_env_var EMAIL_HOST "${EMAIL_HOST:-}"
        write_env_var EMAIL_PORT "${EMAIL_PORT:-587}"
        write_env_var EMAIL_USER "${EMAIL_USER:-}"
        write_env_var EMAIL_PASS "${EMAIL_PASS:-}"
        write_env_var EMAIL_FROM "${EMAIL_FROM:-}"
        write_env_var EMAIL_TO "${EMAIL_TO:-}"
        write_env_raw WEBHOOK_ENABLED "$WEBHOOK_ENABLED"
        write_env_var WEBHOOK_URL "${WEBHOOK_URL:-}"
        write_env_var WEBHOOK_SECRET "${WEBHOOK_SECRET:-}"
    } > .env
    echo -e "${GREEN}✓ .env 已生成 (绑定 127.0.0.1:${PORT})${NC}"

    cat > configs/config.yaml << ENDCFG
server: {port: ${PORT}, mode: release, trusted_proxy: true}
database: {driver: mysql, host: mysql, port: 3306, user: $(yaml_quote "$DB_USER"), password: $(yaml_quote "$DB_PASSWORD"), dbname: $(yaml_quote "$DB_NAME"), charset: utf8mb4, max_open: 50, max_idle: 10}
redis: {addr: redis:6379, password: $(yaml_quote "$REDIS_PASSWORD"), db: 0}
admin: {username: admin, password: $(yaml_quote "$ADMIN_PASSWORD"), session_secret: $(yaml_quote "$SESSION_SECRET")}
encryption: {key: $(yaml_quote "$ENCRYPTION_KEY")}
shortlink: {default_length: 6, max_custom_length: 32, min_custom_length: 4, hashids_salt: $(yaml_quote "$HASHIDS_SALT"), snowflake_worker: 1, snowflake_datacenter: 1}
rate_limit: {create_per_minute: 10, redirect_per_minute: 60}
cloudflare: {enabled: false, site_key: "", secret_key: ""}
captcha:
  enabled: ${CAPTCHA_ENABLED:-true}
  provider: cap
  mode: adaptive
  normal_provider: cap
  escalation_provider: turnstile
  failure_threshold: 3
  risk_window_seconds: 600
  cap: {site_key: $(yaml_quote "${CAP_SITE_KEY:-}"), secret_key: $(yaml_quote "${CAP_SECRET_KEY:-}"), verify_url: $(yaml_quote "${CAP_VERIFY_URL:-}"), api_endpoint: $(yaml_quote "${CAP_API_ENDPOINT:-}")}
  playcaptcha: {enabled: false, site_key: "", secret_key: "", endpoint: ""}
notification:
  feishu: {enabled: ${FEISHU_ENABLED}, webhook: $(yaml_quote "${FEISHU_WEBHOOK:-}"), secret: $(yaml_quote "${FEISHU_SECRET:-}")}
  telegram: {enabled: ${TELEGRAM_ENABLED}, bot_token: $(yaml_quote "${TELEGRAM_BOT_TOKEN:-}"), chat_id: $(yaml_quote "${TELEGRAM_CHAT_ID:-}")}
  dingtalk: {enabled: ${DINGTALK_ENABLED}, webhook: $(yaml_quote "${DINGTALK_WEBHOOK:-}"), secret: $(yaml_quote "${DINGTALK_SECRET:-}")}
  wecom: {enabled: ${WECOM_ENABLED}, webhook: $(yaml_quote "${WECOM_WEBHOOK:-}")}
  bark: {enabled: ${BARK_ENABLED}, key: $(yaml_quote "${BARK_KEY:-}"), endpoint: $(yaml_quote "${BARK_ENDPOINT:-}")}
  discord: {enabled: ${DISCORD_ENABLED}, webhook: $(yaml_quote "${DISCORD_WEBHOOK:-}")}
  email: {enabled: ${EMAIL_ENABLED}, host: $(yaml_quote "${EMAIL_HOST:-}"), port: $(yaml_quote "${EMAIL_PORT:-587}"), user: $(yaml_quote "${EMAIL_USER:-}"), pass: $(yaml_quote "${EMAIL_PASS:-}"), from: $(yaml_quote "${EMAIL_FROM:-}"), to: $(yaml_quote "${EMAIL_TO:-}")}
  webhook: {enabled: ${WEBHOOK_ENABLED}, url: $(yaml_quote "${WEBHOOK_URL:-}"), secret: $(yaml_quote "${WEBHOOK_SECRET:-}")}
features: {allow_custom_code: true, require_privacy_agree: true}
background: {enabled: false, url: "", type: image}
ENDCFG
    chmod 600 .env configs/config.yaml 2>/dev/null || true
    echo -e "${GREEN}✓ config.yaml 已生成${NC}"

    echo ""
    echo -e "${YELLOW}>>> 构建并启动...${NC}"
    $COMPOSE_CMD down --remove-orphans 2>/dev/null || true
    $COMPOSE_CMD up --build -d

    if ! cap_key_ready; then
        [ -n "${CAP_SITE_KEY:-}" ] && echo -e "${YELLOW}⚠ 既有 Cap Site Key 不可用，正在重新创建...${NC}"
        CAP_SITE_KEY=""
        CAP_SECRET_KEY=""
        auto_configure_cap "" || true
    fi
    if [ -n "${CAP_SITE_KEY:-}" ] && [ -n "${CAP_SECRET_KEY:-}" ]; then
        CAP_VERIFY_URL="http://cap:3000/${CAP_SITE_KEY}/siteverify"
        CAP_API_ENDPOINT="/cap/${CAP_SITE_KEY}/"
        CAP_SITE_KEY="$CAP_SITE_KEY" CAP_SECRET_KEY="$CAP_SECRET_KEY" CAP_VERIFY_URL="$CAP_VERIFY_URL" CAP_API_ENDPOINT="$CAP_API_ENDPOINT" python3 - <<'PY'
import os
from pathlib import Path

def dotenv_quote(v):
    return '"' + v.replace('\\', '\\\\').replace('"', '\\"').replace('$', '\\$').replace('`', '\\`').replace('\n', '\\n') + '"'

p = Path('.env')
s = p.read_text()
repls = {
    'CAP_SITE_KEY': os.environ['CAP_SITE_KEY'],
    'CAP_SECRET_KEY': os.environ['CAP_SECRET_KEY'],
    'CAP_VERIFY_URL': os.environ['CAP_VERIFY_URL'],
    'CAP_API_ENDPOINT': os.environ['CAP_API_ENDPOINT'],
}
for k, v in repls.items():
    lines = s.splitlines()
    found = False
    out = []
    q = dotenv_quote(v)
    for line in lines:
        if line.startswith(k + '='):
            out.append(k + '=' + q)
            found = True
        else:
            out.append(line)
    if not found:
        out.append(k + '=' + q)
    s = '\n'.join(out) + '\n'
p.write_text(s)

c = Path('configs/config.yaml')
y = c.read_text()
lines = []
for line in y.splitlines():
    if line.lstrip().startswith('cap: {'):
        indent = line[:len(line) - len(line.lstrip())]
        line = (indent + 'cap: {site_key: "' + repls['CAP_SITE_KEY'] + '", secret_key: "' + repls['CAP_SECRET_KEY'] +
                '", verify_url: "' + repls['CAP_VERIFY_URL'] + '", api_endpoint: "' + repls['CAP_API_ENDPOINT'] + '"}')
    lines.append(line)
c.write_text('\n'.join(lines) + '\n')
PY
        $COMPOSE_CMD up -d app
    fi

    ADMIN_SUFFIX="$(get_current_admin_suffix || true)"
    echo ""
    echo -e "${GREEN}========================================${NC}"
    echo -e "${GREEN}   部署成功！${NC}"
    echo -e "${GREEN}========================================${NC}"
    echo ""
    echo -e "📌 访问地址: ${BLUE}http://127.0.0.1:${PORT}${NC}"
    echo -e "📌 管理员账号: ${BLUE}admin${NC}"
    echo -e "📌 管理员密码: ${BLUE}${ADMIN_PASSWORD}${NC}"
    if [ -n "${ADMIN_SUFFIX:-}" ]; then
        echo -e "📌 当前后缀: ${BLUE}${ADMIN_SUFFIX}${NC}"
        echo -e "📌 管理后台: ${BLUE}http://127.0.0.1:${PORT}/${ADMIN_SUFFIX}/${NC}"
    else
        echo -e "📌 管理后台: ${BLUE}请查看应用日志中的 Admin suffix${NC}"
    fi
    echo -e "📌 已绑定回环 (127.0.0.1) — 外网访问请自行配置 Nginx 反代到 ${BLUE}127.0.0.1:${PORT}${NC}"
    echo -e "📌 Cap 控制台: ${BLUE}http://127.0.0.1:${CAP_HTTP_PORT}${NC}（本机随机端口，不暴露公网）"
    echo -e "📌 Cap 公网页面/组件统一走 Shortlink 同域反代: ${BLUE}/cap/${NC}；反代公网时只需要把域名指向 Shortlink 主服务。"
    echo -e "📌 Cap 管理密钥已写入 .env 的 CAP_ADMIN_KEY；脚本会尽量自动创建 Site Key/Secret。"
    echo ""
    echo -e "${YELLOW}TOTP 绑定:${NC}"
    echo -e "  ${BLUE}登录后台后，在设置/TOTP 页面查看并绑定验证器${NC}"
    echo ""
    echo -e "  停止: docker compose down"
    echo -e "  日志: docker compose logs -f app"
}
main "$@"

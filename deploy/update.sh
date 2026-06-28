#!/usr/bin/env bash
# =============================================================================
# Sub2API remote-image deployment script.
# Server side only pulls images from the registry and restarts containers.
# Build and push images from Windows with build-push-tcr.ps1.
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEPLOY_DIR="${DEPLOY_DIR:-$SCRIPT_DIR}"
BACKUP_DIR="${BACKUP_DIR:-/www/backup/sub2api}"
APP_TAG_OVERRIDE="${1:-${APP_TAG:-}}"

load_env_file() {
  local file="$1"
  while IFS='=' read -r key value || [ -n "$key" ]; do
    key="${key#"${key%%[![:space:]]*}"}"
    key="${key%"${key##*[![:space:]]}"}"
    case "$key" in
      ""|\#*) continue ;;
    esac
    if ! printf '%s' "$key" | grep -Eq '^[A-Za-z_][A-Za-z0-9_]*$'; then
      continue
    fi
    value="${value%$'\r'}"
    value="${value#"${value%%[![:space:]]*}"}"
    value="${value%"${value##*[![:space:]]}"}"
    case "$value" in
      \"*\") value="${value#\"}"; value="${value%\"}" ;;
      \'*\') value="${value#\'}"; value="${value%\'}" ;;
    esac
    export "$key=$value"
  done < "$file"
}

escape_sed_replacement() {
  printf '%s' "$1" | sed -e 's/[\/&|]/\\&/g'
}

upsert_env() {
  local key="$1"
  local value="$2"
  local file="$3"
  local escaped
  escaped="$(escape_sed_replacement "$value")"
  if grep -q "^${key}=" "$file"; then
    sed -i "s|^${key}=.*|${key}=${escaped}|" "$file"
  else
    printf '\n%s=%s\n' "$key" "$value" >> "$file"
  fi
}

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "[ERROR] Required command not found: $1"
    exit 1
  fi
}

require_command docker
require_command curl
require_command tar
require_command sed

if ! docker compose version >/dev/null 2>&1; then
  echo "[ERROR] Docker Compose v2 is required."
  exit 1
fi

mkdir -p "$DEPLOY_DIR"
cd "$DEPLOY_DIR"

if [ ! -f .env ]; then
  if [ -f .env.example ]; then
    cp .env.example .env
    chmod 600 .env
  fi
  echo "[ERROR] $DEPLOY_DIR/.env is missing. Configure it first, especially SUB2API_IMAGE and secrets."
  exit 1
fi

load_env_file "$DEPLOY_DIR/.env"

if [ -n "$APP_TAG_OVERRIDE" ]; then
  upsert_env "APP_TAG" "$APP_TAG_OVERRIDE" "$DEPLOY_DIR/.env"
  export APP_TAG="$APP_TAG_OVERRIDE"
fi

if [ -z "${SUB2API_IMAGE:-}" ] || [ "$SUB2API_IMAGE" = "ccr.ccs.tencentyun.com/your-namespace/sub2api" ]; then
  echo "[ERROR] SUB2API_IMAGE must point to your Tencent Cloud TCR image."
  echo "        Example: SUB2API_IMAGE=ccr.ccs.tencentyun.com/my-namespace/sub2api"
  exit 1
fi

if [ -z "${APP_TAG:-}" ]; then
  echo "[ERROR] APP_TAG is empty. Pass a tag as the first argument or set APP_TAG in .env."
  exit 1
fi

echo "=== [1/5] Backup current runtime data and config ==="
mkdir -p "$BACKUP_DIR"
mkdir -p "$DEPLOY_DIR/data" "$DEPLOY_DIR/redis_data"
tar czf "$BACKUP_DIR/deploy-backup-$(date +%Y%m%d-%H%M%S).tar.gz" \
  -C "$DEPLOY_DIR" .env data redis_data 2>/dev/null || true

echo "=== [2/5] Ensure runtime directories ==="
mkdir -p "$DEPLOY_DIR/data/logs" "$DEPLOY_DIR/redis_data"

echo "=== [3/5] Fix Baota Nginx reverse proxy target if present ==="
SUB2API_NGINX_PROXY="${SUB2API_NGINX_PROXY:-/www/server/panel/vhost/nginx/proxy/sub.bahew.com/9f6e6800cfae7749eb6c486619254b9c_sub.bahew.com.conf}"
if [ -f "$SUB2API_NGINX_PROXY" ]; then
  cp "$SUB2API_NGINX_PROXY" "$SUB2API_NGINX_PROXY.bak-$(date +%Y%m%d-%H%M%S)"
  sed -i -E 's#proxy_pass http://172\.[0-9]+\.[0-9]+\.[0-9]+:8080;#proxy_pass http://127.0.0.1:8080;#' "$SUB2API_NGINX_PROXY"
  sed -i -E 's#proxy_pass http://sub2api:8080;#proxy_pass http://127.0.0.1:8080;#' "$SUB2API_NGINX_PROXY"
  if command -v nginx >/dev/null 2>&1; then
    nginx -t && nginx -s reload
  elif [ -x /www/server/nginx/sbin/nginx ]; then
    /www/server/nginx/sbin/nginx -t && /www/server/nginx/sbin/nginx -s reload
  else
    echo "[WARNING] nginx command not found; please reload Nginx manually."
  fi
else
  echo "[INFO] Baota proxy file not found, skipping Nginx fix."
fi

echo "=== [4/5] Pull remote images ==="
echo "[INFO] Pulling ${SUB2API_IMAGE}:${APP_TAG}"
docker compose pull sub2api redis

echo "=== [5/5] Recreate services ==="
docker compose up -d --force-recreate sub2api redis

HEALTH_URL="${SUB2API_HEALTH_URL:-http://127.0.0.1:${SERVER_PORT:-8080}/health}"
echo "[INFO] Waiting for Sub2API health: $HEALTH_URL"
for attempt in $(seq 1 30); do
  if curl -fsS "$HEALTH_URL" >/dev/null; then
    echo ">>> [SUCCESS] Deployment succeeded. Image: ${SUB2API_IMAGE}:${APP_TAG}"
    exit 0
  fi
  if [ "$attempt" -eq 30 ]; then
    echo ">>> [ERROR] Health check failed. Recent logs:"
    docker compose logs --tail=80 sub2api
    exit 1
  fi
  sleep 2
done

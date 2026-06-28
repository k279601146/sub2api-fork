#!/bin/bash
# =============================================================================
# Sub2API 滚动构建与一键部署升级脚本 (集成 GitHub Token)
# =============================================================================
set -e

SRC_DIR="/www/wwwroot/sub2api-fork"
DEPLOY_DIR="/www/wwwroot/sub2api-deploy/deploy"
BACKUP_DIR="/www/backup/sub2api"

# 从部署目录的 .env 文件加载私有变量（如 GITHUB_TOKEN）
if [ -f "$DEPLOY_DIR/.env" ]; then
  # 只导出需要的变量1
  export $(grep -E "^(GITHUB_TOKEN|BRANCH)=" "$DEPLOY_DIR/.env" | xargs)
fi

# 兜底默认分支
BRANCH="${BRANCH:-idehotai}"

if [ -z "${GITHUB_TOKEN}" ]; then
  echo "[ERROR] 未在部署目录的 .env 中配置 GITHUB_TOKEN！请先配置该变量。"
  exit 1
fi

# 使用 GITHUB_TOKEN 的鉴权 URL 格式
AUTH_REPO_URL="https://oauth2:${GITHUB_TOKEN}@github.com/k279601146/sub2api-fork.git"
# 转换成使用 gitclone 镜像加速的鉴权 URL
ACCELERATED_REPO_URL="https://oauth2:${GITHUB_TOKEN}@gh-proxy.com/github.com/k279601146/sub2api-fork.git"

echo "=== [1/5] 备份当前运行数据与配置 ==="
mkdir -p "$BACKUP_DIR"
if [ -d "$DEPLOY_DIR" ] && [ -f "$DEPLOY_DIR/.env" ]; then
  mkdir -p "$DEPLOY_DIR/data"
  tar czf "$BACKUP_DIR/deploy-backup-$(date +%Y%m%d-%H%M%S).tar.gz" -C "$DEPLOY_DIR" .env data/ || true
fi

echo "=== [2/5] 拉取 GitHub 二开代码 (使用 Token 鉴权与 gitclone 镜像加速) ==="
# 如果源码目录不存在，进行首次克隆
if [ ! -d "$SRC_DIR/.git" ]; then
  echo "源码目录不存在，正在通过带有 Token 鉴权的加速镜像克隆私有仓库..."
  git clone -b "$BRANCH" "$ACCELERATED_REPO_URL" "$SRC_DIR"
fi

cd "$SRC_DIR"
# 确保本地 git 配置中的 remote url 包含 Token 并且使用了加速镜像
git remote set-url origin "$ACCELERATED_REPO_URL"

echo "拉取分支 $BRANCH 的最新代码..."
git fetch --all
git reset --hard "origin/$BRANCH"

# 获取最新 commit hash
NEW_COMMIT=$(git rev-parse --short HEAD)
echo "当前更新版本 Commit: $NEW_COMMIT"

echo "=== [3/5] 同步部署文件并更新配置 ==="
install -m 0644 "$SRC_DIR/deploy/docker-compose.yml" "$DEPLOY_DIR/docker-compose.yml"
install -m 0644 "$SRC_DIR/deploy/.env.example" "$DEPLOY_DIR/.env.example"
install -m 0755 "$SRC_DIR/deploy/docker-entrypoint.sh" "$DEPLOY_DIR/docker-entrypoint.sh"
mkdir -p "$DEPLOY_DIR/data"
if [ -f "$SRC_DIR/backend/resources/GeoLite2-Country.mmdb" ]; then
  install -m 0644 "$SRC_DIR/backend/resources/GeoLite2-Country.mmdb" "$DEPLOY_DIR/data/GeoLite2-Country.mmdb"
else
  echo "[WARNING] GeoLite2-Country.mmdb not found in backend/resources; model_region_isolation geoip lookup may be disabled."
fi

cd "$DEPLOY_DIR"
if [ -f .env ]; then
  sed -i "s/^APP_COMMIT=.*/APP_COMMIT=${NEW_COMMIT}/" .env
  sed -i "s/^APP_TAG=.*/APP_TAG=${NEW_COMMIT}/" .env
else
  echo "[WARNING] 部署目录下的 .env 文件不存在，请参考部署文档先生成 .env！"
fi

echo "=== [3.5/5] 修正宝塔 Nginx 反代目标 ==="
SUB2API_NGINX_PROXY="/www/server/panel/vhost/nginx/proxy/sub.bahew.com/9f6e6800cfae7749eb6c486619254b9c_sub.bahew.com.conf"
if [ -f "$SUB2API_NGINX_PROXY" ]; then
  cp "$SUB2API_NGINX_PROXY" "$SUB2API_NGINX_PROXY.bak-$(date +%Y%m%d-%H%M%S)"
  sed -i -E 's#proxy_pass http://172\.[0-9]+\.[0-9]+\.[0-9]+:8080;#proxy_pass http://127.0.0.1:8080;#' "$SUB2API_NGINX_PROXY"
  sed -i -E 's#proxy_pass http://sub2api:8080;#proxy_pass http://127.0.0.1:8080;#' "$SUB2API_NGINX_PROXY"
  if command -v nginx >/dev/null 2>&1; then
    nginx -t && nginx -s reload
  elif [ -x /www/server/nginx/sbin/nginx ]; then
    /www/server/nginx/sbin/nginx -t && /www/server/nginx/sbin/nginx -s reload
  else
    echo "[WARNING] 未找到 nginx 命令，请手动检查并重载 Nginx。"
  fi
else
  echo "[INFO] 未找到 sub.bahew.com 宝塔反代配置，跳过 Nginx 修正。"
fi

echo "=== [4/5] 现场构建新版本 Docker 镜像 ==="
echo "当前 Docker Compose 数据库连接配置："
docker compose config | grep -E "DATABASE_(HOST|PORT|USER|DBNAME|SSLMODE):" || true
docker compose build sub2api

echo "=== [5/5] 重启并运行新容器 ==="
docker compose up -d  sub2api
#日常重启：docker compose up -d sub2api
#改配置 / 权限异常 / 配置不生效：docker compose up -d --force-recreate sub2api
#两者都不会删除数据卷（/app/data 数据库、日志文件都保留，不用担心数据丢失）
echo "=== 更新完成，进行健康检查 ==="
sleep 3
if curl -fsS http://172.17.0.1:8080/health >/dev/null; then
  echo ">>> [SUCCESS] 升级成功！服务运行正常。最新 Commit: $NEW_COMMIT"
else
  echo ">>> [ERROR] 健康检查失败，请检查 Docker 日志！"
  docker compose logs --tail=50 sub2api
fi

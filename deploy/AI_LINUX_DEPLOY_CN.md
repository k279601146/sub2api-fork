# AI 助手 Linux 源码部署指南

本文档用于指导 AI 运维助手把当前二次开发后的 Sub2API 源码部署到 Linux 服务器，并通过域名 `sub.bahew.com` 对外提供 HTTPS 服务。

## 推荐架构

- 先安装宝塔面板，用于后续站点、Nginx、SSL、防火墙和日常运维管理。
- Sub2API 不使用官方镜像，改为基于当前项目源码在服务器本地构建镜像。
- PostgreSQL、Redis 和 Sub2API 由 Docker Compose 管理，数据落在部署目录，便于备份迁移。
- 宝塔 Nginx 只做反向代理和 HTTPS，转发到宿主机本地 `127.0.0.1:8080`。
- 不启动浏览器测试，验收全部使用 `curl`、`docker compose`、`systemctl` 和宝塔/Nginx 状态检查。

推荐目录：

```text
/opt/sub2api-src        # 当前二开源码仓库或上传后的源码
/opt/sub2api-deploy     # Compose、.env、运行数据
/opt/sub2api-backups    # 备份文件
```

## 交给 AI 助手的任务说明

可以把下面这段直接发给具备 SSH 权限的 AI 运维助手：

```text
请在 Linux 服务器上部署当前二次开发版 Sub2API。

固定域名：sub.bahew.com
源码目录：/opt/sub2api-src
部署目录：/opt/sub2api-deploy
应用监听：127.0.0.1:8080
管理面板：宝塔面板
反向代理：宝塔 Nginx
HTTPS：宝塔面板申请 Let's Encrypt 证书，或使用同等命令行证书方案

要求：
1. 先检查系统、DNS、端口和已有服务，不要覆盖已有部署。
2. 先安装宝塔面板，安装命令使用用户提供的脚本。
3. 必须使用当前二开源码构建 Sub2API，不要拉取 weishaw/sub2api 官方镜像作为应用镜像。
4. PostgreSQL、Redis、Sub2API 使用 Docker Compose 管理。
5. 自动生成强随机 POSTGRES_PASSWORD、JWT_SECRET、TOTP_ENCRYPTION_KEY、ADMIN_PASSWORD。
6. 不在日志或回复中明文输出密钥；只说明密钥已写入 /opt/sub2api-deploy/.env。
7. Nginx 必须开启 underscores_in_headers on，以兼容带下划线的请求头。
8. 不启动浏览器测试；使用 curl 验证 /health 和 HTTPS 响应。
9. 完成后输出宝塔入口获取方式、服务状态、访问地址、管理员账号获取方式和常用运维命令。
```

## 部署前检查

AI 助手开始执行前必须确认：

- 已获得服务器 SSH 权限，并具备 `root` 或 `sudo` 权限。
- `sub.bahew.com` 的 DNS `A` 或 `AAAA` 记录已经指向目标服务器公网 IP。
- 服务器安全组允许入站 `22/tcp`、`80/tcp`、`443/tcp`，宝塔面板端口按安装输出放行。
- 服务器上没有正在使用 `80`、`443`、`8080` 的冲突服务，或已经确认可以调整。
- 当前二开源码已经能通过 Git 拉取，或可以上传到服务器。

检查命令：

```bash
set -euo pipefail

DOMAIN="sub.bahew.com"
SRC_DIR="/opt/sub2api-src"
APP_DIR="/opt/sub2api-deploy"
BACKUP_DIR="/opt/sub2api-backups"
APP_PORT="8080"

hostnamectl || true
id
ss -lntp | grep -E ':(80|443|8080)\b' || true
getent ahosts "$DOMAIN" || true
curl -4s ifconfig.me || true
```

如果 DNS 没有解析到当前服务器，暂停证书配置并提示用户先修改 DNS。应用可以先部署，但 HTTPS 证书签发依赖公网 DNS 和 `80/tcp` 可访问。

## 1. 安装宝塔面板

按用户要求，宝塔面板必须先安装。安装脚本如下：

```bash
if [ -f /usr/bin/curl ]; then
  curl -sSO https://download.bt.cn/install/install_panel.sh
else
  wget -O install_panel.sh https://download.bt.cn/install/install_panel.sh
fi
bash install_panel.sh ed8484bec
```

安装完成后记录宝塔输出的面板地址、用户名、密码和安全入口。不要把面板密码写入公开日志。

常用宝塔命令：

```bash
bt status
bt default
```

建议在宝塔里安装或确认：

- Nginx
- Docker 管理器，便于查看容器；命令行仍以 Docker Compose 为准
- 系统防火墙放行 `80`、`443` 和宝塔面板端口

## 2. 安装基础命令和 Docker

以下命令适用于 Ubuntu/Debian。其他发行版按系统包管理器等价安装 `git`、`curl`、`openssl`、`docker` 和 Docker Compose v2。

```bash
sudo apt-get update
sudo apt-get install -y ca-certificates curl gnupg git openssl
```

安装 Docker：

```bash
if ! command -v docker >/dev/null 2>&1; then
  . /etc/os-release
  case "$ID" in
    ubuntu|debian) DOCKER_REPO_ID="$ID" ;;
    *) echo "当前脚本只自动配置 Ubuntu/Debian 的 Docker 源，请按发行版手动安装 Docker。"; exit 1 ;;
  esac

  sudo install -m 0755 -d /etc/apt/keyrings
  curl -fsSL "https://download.docker.com/linux/${DOCKER_REPO_ID}/gpg" | sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg
  sudo chmod a+r /etc/apt/keyrings/docker.gpg
  echo \
    "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/${DOCKER_REPO_ID} ${VERSION_CODENAME} stable" \
    | sudo tee /etc/apt/sources.list.d/docker.list >/dev/null
  sudo apt-get update
  sudo apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
fi

sudo systemctl enable --now docker
docker compose version
```

## 3. 准备当前源码

不要从官方仓库重新拉取未二开的代码。使用用户的二开仓库地址或上传包。

方式 A：从你的 Git 仓库拉取：

```bash
sudo mkdir -p "$SRC_DIR"
sudo chown "$USER":"$USER" "$SRC_DIR"

# 替换为你的二开仓库地址
git clone <YOUR_SUB2API_FORK_REPO_URL> "$SRC_DIR"
cd "$SRC_DIR"
git status --short
git rev-parse --short HEAD
```

方式 B：上传当前源码包：

```bash
sudo mkdir -p "$SRC_DIR"
sudo chown "$USER":"$USER" "$SRC_DIR"

# 示例：把 sub2api-fork.tar.gz 上传到 /tmp 后解压
tar xzf /tmp/sub2api-fork.tar.gz -C "$SRC_DIR" --strip-components=1
cd "$SRC_DIR"
git status --short || true
```

构建前确认关键文件存在：

```bash
test -f "$SRC_DIR/Dockerfile"
test -f "$SRC_DIR/frontend/package.json"
test -f "$SRC_DIR/frontend/pnpm-lock.yaml"
test -f "$SRC_DIR/backend/go.mod"
```

## 4. 创建源码构建版 Compose

部署目录只保存运行配置和数据，源码目录只保存代码。

```bash
sudo mkdir -p "$APP_DIR" "$BACKUP_DIR"
sudo chown "$USER":"$USER" "$APP_DIR" "$BACKUP_DIR"
cd "$APP_DIR"

if [ -f docker-compose.yml ] || [ -f .env ]; then
  echo "部署目录已存在，请先确认是否为旧部署：$APP_DIR"
  ls -la "$APP_DIR"
  exit 1
fi

mkdir -p data postgres_data redis_data
chmod 700 data postgres_data redis_data
```

写入 `docker-compose.yml`。这里的 `sub2api` 服务使用 `build.context: ${SRC_DIR}`，会从当前二开源码构建镜像。

```bash
cat > docker-compose.yml <<'YAML'
services:
  sub2api:
    build:
      context: ${SRC_DIR}
      dockerfile: Dockerfile
      args:
        GOPROXY: ${GOPROXY:-https://goproxy.cn,direct}
        GOSUMDB: ${GOSUMDB:-sum.golang.google.cn}
        COMMIT: ${APP_COMMIT:-local}
    image: sub2api-custom:${APP_TAG:-latest}
    container_name: sub2api
    restart: unless-stopped
    ulimits:
      nofile:
        soft: 100000
        hard: 100000
    ports:
      - "${BIND_HOST:-127.0.0.1}:${SERVER_PORT:-8080}:8080"
    volumes:
      - ./data:/app/data
    environment:
      - AUTO_SETUP=true
      - SERVER_HOST=0.0.0.0
      - SERVER_PORT=8080
      - SERVER_MODE=${SERVER_MODE:-release}
      - RUN_MODE=${RUN_MODE:-standard}
      - DATABASE_HOST=postgres
      - DATABASE_PORT=5432
      - DATABASE_USER=${POSTGRES_USER:-sub2api}
      - DATABASE_PASSWORD=${POSTGRES_PASSWORD:?POSTGRES_PASSWORD is required}
      - DATABASE_DBNAME=${POSTGRES_DB:-sub2api}
      - DATABASE_SSLMODE=disable
      - DATABASE_MAX_OPEN_CONNS=${DATABASE_MAX_OPEN_CONNS:-256}
      - DATABASE_MAX_IDLE_CONNS=${DATABASE_MAX_IDLE_CONNS:-128}
      - REDIS_HOST=redis
      - REDIS_PORT=6379
      - REDIS_PASSWORD=${REDIS_PASSWORD:-}
      - REDIS_DB=${REDIS_DB:-0}
      - REDIS_POOL_SIZE=${REDIS_POOL_SIZE:-4096}
      - REDIS_MIN_IDLE_CONNS=${REDIS_MIN_IDLE_CONNS:-256}
      - ADMIN_EMAIL=${ADMIN_EMAIL:-admin@sub.bahew.com}
      - ADMIN_PASSWORD=${ADMIN_PASSWORD:-}
      - JWT_SECRET=${JWT_SECRET:-}
      - JWT_EXPIRE_HOUR=${JWT_EXPIRE_HOUR:-24}
      - TOTP_ENCRYPTION_KEY=${TOTP_ENCRYPTION_KEY:-}
      - TZ=${TZ:-Asia/Shanghai}
      - SECURITY_URL_ALLOWLIST_ENABLED=${SECURITY_URL_ALLOWLIST_ENABLED:-false}
      - SECURITY_URL_ALLOWLIST_ALLOW_INSECURE_HTTP=${SECURITY_URL_ALLOWLIST_ALLOW_INSECURE_HTTP:-false}
      - SECURITY_URL_ALLOWLIST_ALLOW_PRIVATE_HOSTS=${SECURITY_URL_ALLOWLIST_ALLOW_PRIVATE_HOSTS:-false}
      - UPDATE_PROXY_URL=${UPDATE_PROXY_URL:-}
      - GEMINI_OAUTH_CLIENT_ID=${GEMINI_OAUTH_CLIENT_ID:-}
      - GEMINI_OAUTH_CLIENT_SECRET=${GEMINI_OAUTH_CLIENT_SECRET:-}
      - GEMINI_CLI_OAUTH_CLIENT_SECRET=${GEMINI_CLI_OAUTH_CLIENT_SECRET:-}
      - ANTIGRAVITY_OAUTH_CLIENT_SECRET=${ANTIGRAVITY_OAUTH_CLIENT_SECRET:-}
    depends_on:
      postgres:
        condition: service_healthy
      redis:
        condition: service_healthy
    networks:
      - sub2api-network
    healthcheck:
      test: ["CMD", "wget", "-q", "-T", "5", "-O", "/dev/null", "http://localhost:8080/health"]
      interval: 30s
      timeout: 10s
      retries: 3
      start_period: 30s

  postgres:
    image: postgres:18-alpine
    container_name: sub2api-postgres
    restart: unless-stopped
    ulimits:
      nofile:
        soft: 100000
        hard: 100000
    volumes:
      - ./postgres_data:/var/lib/postgresql/data
    environment:
      - PGDATA=/var/lib/postgresql/data
      - POSTGRES_USER=${POSTGRES_USER:-sub2api}
      - POSTGRES_PASSWORD=${POSTGRES_PASSWORD:?POSTGRES_PASSWORD is required}
      - POSTGRES_DB=${POSTGRES_DB:-sub2api}
      - TZ=${TZ:-Asia/Shanghai}
    networks:
      - sub2api-network
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U ${POSTGRES_USER:-sub2api} -d ${POSTGRES_DB:-sub2api}"]
      interval: 10s
      timeout: 5s
      retries: 5
      start_period: 10s

  redis:
    image: redis:8-alpine
    container_name: sub2api-redis
    restart: unless-stopped
    ulimits:
      nofile:
        soft: 100000
        hard: 100000
    volumes:
      - ./redis_data:/data
    command: >
      sh -c '
        redis-server
        --save 60 1
        --appendonly yes
        --appendfsync everysec
        ${REDIS_PASSWORD:+--requirepass "$REDIS_PASSWORD"}'
    environment:
      - TZ=${TZ:-Asia/Shanghai}
      - REDISCLI_AUTH=${REDIS_PASSWORD:-}
    networks:
      - sub2api-network
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 10s
      timeout: 5s
      retries: 5
      start_period: 5s

networks:
  sub2api-network:
    driver: bridge
YAML
```

## 5. 写入生产环境变量

自动生成密钥并写入 `.env`。不要在最终回复里粘贴这些密钥。

```bash
cd "$APP_DIR"

APP_COMMIT="$(cd "$SRC_DIR" && git rev-parse --short HEAD 2>/dev/null || date +%Y%m%d%H%M%S)"
POSTGRES_PASSWORD="$(openssl rand -hex 32)"
JWT_SECRET="$(openssl rand -hex 32)"
TOTP_ENCRYPTION_KEY="$(openssl rand -hex 32)"
ADMIN_PASSWORD="$(openssl rand -base64 24 | tr -d '=+/' | cut -c1-20)"

cat > .env <<EOF
SRC_DIR=${SRC_DIR}
APP_COMMIT=${APP_COMMIT}
APP_TAG=${APP_COMMIT}
BIND_HOST=127.0.0.1
SERVER_PORT=8080
SERVER_MODE=release
RUN_MODE=standard
TZ=Asia/Shanghai
POSTGRES_USER=sub2api
POSTGRES_PASSWORD=${POSTGRES_PASSWORD}
POSTGRES_DB=sub2api
REDIS_PASSWORD=
ADMIN_EMAIL=admin@sub.bahew.com
ADMIN_PASSWORD=${ADMIN_PASSWORD}
JWT_SECRET=${JWT_SECRET}
JWT_EXPIRE_HOUR=24
TOTP_ENCRYPTION_KEY=${TOTP_ENCRYPTION_KEY}
SECURITY_URL_ALLOWLIST_ENABLED=false
SECURITY_URL_ALLOWLIST_ALLOW_INSECURE_HTTP=false
SECURITY_URL_ALLOWLIST_ALLOW_PRIVATE_HOSTS=false
GOPROXY=https://goproxy.cn,direct
GOSUMDB=sum.golang.google.cn
EOF

chmod 600 .env
```

如果要启用 Simple Mode，再追加：

```bash
cat >> .env <<'EOF'
RUN_MODE=simple
SIMPLE_MODE_CONFIRM=true
EOF
```

## 6. 构建并启动二开版 Sub2API

首次构建会安装前端依赖、编译前端、编译 Go 后端，并把前端嵌入后端二进制。

```bash
cd "$APP_DIR"
docker compose build --pull sub2api
docker compose up -d
docker compose ps
docker compose logs --tail=120 sub2api
```

本地健康检查：

```bash
curl -fsS http://127.0.0.1:8080/health
```

如果 `/health` 没有返回成功，先查看日志，不要继续配置 HTTPS：

```bash
cd "$APP_DIR"
docker compose logs --tail=200 sub2api
docker compose logs --tail=100 postgres
docker compose logs --tail=100 redis
```

## 7. 配置宝塔 Nginx 反向代理

推荐在宝塔面板中操作：

1. 网站 -> 添加站点。
2. 域名填写 `sub.bahew.com`。
3. PHP 版本选择纯静态或不使用 PHP。
4. 反向代理目标填写 `http://127.0.0.1:8080`。
5. SSL -> Let's Encrypt -> 申请证书并开启强制 HTTPS。

如果需要 AI 助手通过命令行写入宝塔 Nginx 配置，可使用下面的方式。宝塔 Nginx 常见路径为 `/www/server/nginx/conf/nginx.conf` 和 `/www/server/panel/vhost/nginx/`。

先写入 Nginx 全局配置，开启下划线请求头和 WebSocket upgrade 变量：

```bash
BT_NGINX_CONF="/www/server/nginx/conf/nginx.conf"

if [ -f "$BT_NGINX_CONF" ]; then
  if ! grep -q 'underscores_in_headers on;' "$BT_NGINX_CONF"; then
    sudo sed -i '/http[[:space:]]*{/a \    underscores_in_headers on;\n    map $http_upgrade $connection_upgrade {\n        default upgrade;\n        "" close;\n    }' "$BT_NGINX_CONF"
  fi
else
  echo "未找到宝塔 Nginx 主配置，请先在宝塔面板安装 Nginx。"
  exit 1
fi
```

写入站点反向代理配置：

```bash
sudo mkdir -p /www/server/panel/vhost/nginx
sudo tee /www/server/panel/vhost/nginx/sub.bahew.com.conf >/dev/null <<'NGINX'
server {
    listen 80;
    listen [::]:80;
    server_name sub.bahew.com;

    client_max_body_size 256m;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;

        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Forwarded-Host $host;

        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection $connection_upgrade;

        proxy_buffering off;
        proxy_request_buffering off;
        proxy_read_timeout 3600s;
        proxy_send_timeout 3600s;
    }
}
NGINX

sudo /www/server/nginx/sbin/nginx -t
sudo /etc/init.d/nginx reload
```

HTTP 验证：

```bash
curl -I http://sub.bahew.com/health
```

## 8. 配置 HTTPS

推荐使用宝塔面板申请证书：

1. 网站 -> `sub.bahew.com` -> SSL。
2. 选择 Let's Encrypt。
3. 申请成功后开启强制 HTTPS。
4. 确认站点配置仍保留反向代理到 `127.0.0.1:8080`。

如果证书由宝塔托管，后续续期也交给宝塔，不要再用另一套工具重复管理同一站点证书。

HTTPS 验收：

```bash
curl -fsS https://sub.bahew.com/health
curl -I https://sub.bahew.com/
```

## 9. 管理员账号

默认写入：

- 管理员邮箱：`admin@sub.bahew.com`
- 管理员密码：保存在 `/opt/sub2api-deploy/.env` 的 `ADMIN_PASSWORD`

查看管理员密码时只在服务器本机执行，不要把密码发到公共日志：

```bash
cd /opt/sub2api-deploy
grep '^ADMIN_PASSWORD=' .env
```

首次登录后请立即修改管理员密码，并妥善备份 `.env`。

## 10. 二开版本更新流程

每次更新代码前先备份运行目录：

```bash
cd /opt
sudo tar czf "$BACKUP_DIR/sub2api-deploy-$(date +%Y%m%d-%H%M%S).tar.gz" sub2api-deploy
```

拉取或上传新源码：

```bash
cd "$SRC_DIR"
git status --short
git pull --ff-only
git rev-parse --short HEAD
```

重新构建并滚动替换应用容器：

```bash
cd "$APP_DIR"
NEW_COMMIT="$(cd "$SRC_DIR" && git rev-parse --short HEAD)"
sed -i "s/^APP_COMMIT=.*/APP_COMMIT=${NEW_COMMIT}/" .env
sed -i "s/^APP_TAG=.*/APP_TAG=${NEW_COMMIT}/" .env

docker compose build --pull sub2api
docker compose up -d sub2api
docker compose logs --tail=120 sub2api
curl -fsS http://127.0.0.1:8080/health
curl -fsS https://sub.bahew.com/health
```

## 11. 回滚流程

如果新版本异常，优先用最近备份恢复部署目录，或切回上一个 Git commit 后重建。

切回源码版本重建：

```bash
cd "$SRC_DIR"
git log --oneline -5
git checkout <OLD_COMMIT>

cd "$APP_DIR"
OLD_COMMIT="$(cd "$SRC_DIR" && git rev-parse --short HEAD)"
sed -i "s/^APP_COMMIT=.*/APP_COMMIT=${OLD_COMMIT}/" .env
sed -i "s/^APP_TAG=.*/APP_TAG=${OLD_COMMIT}/" .env

docker compose build sub2api
docker compose up -d sub2api
curl -fsS http://127.0.0.1:8080/health
```

从备份恢复数据：

```bash
cd "$APP_DIR"
docker compose down

cd /opt
sudo mv sub2api-deploy "sub2api-deploy.broken.$(date +%Y%m%d-%H%M%S)"
sudo tar xzf "$BACKUP_DIR/sub2api-deploy-YYYYMMDD-HHMMSS.tar.gz"
sudo chown -R "$USER":"$USER" /opt/sub2api-deploy

cd "$APP_DIR"
docker compose up -d
```

## 12. 常用运维命令

```bash
cd /opt/sub2api-deploy

# 查看容器状态
docker compose ps

# 查看应用日志
docker compose logs -f sub2api

# 重启应用
docker compose restart sub2api

# 重启全部服务
docker compose restart

# 停止全部服务
docker compose down

# 查看宝塔状态和入口
bt status
bt default

# 检查宝塔 Nginx
sudo /www/server/nginx/sbin/nginx -t
sudo /etc/init.d/nginx reload
```

## 13. 备份建议

必须备份：

- `/opt/sub2api-deploy/.env`
- `/opt/sub2api-deploy/data`
- `/opt/sub2api-deploy/postgres_data`
- `/opt/sub2api-deploy/redis_data`
- `/opt/sub2api-src`，或确保 Git 远端包含当前二开代码

备份命令：

```bash
cd /opt
sudo mkdir -p "$BACKUP_DIR"
sudo tar czf "$BACKUP_DIR/sub2api-full-$(date +%Y%m%d-%H%M%S).tar.gz" sub2api-deploy sub2api-src
```

## 14. 完成标准

部署完成时，AI 助手应给出以下信息：

- 宝塔面板已安装，并说明可通过 `bt default` 查看入口和账号。
- `docker compose ps` 中 `sub2api`、`postgres`、`redis` 均为运行或健康状态。
- `curl -fsS http://127.0.0.1:8080/health` 成功。
- `curl -fsS https://sub.bahew.com/health` 成功。
- `https://sub.bahew.com/` 可访问。
- 宝塔 Nginx 配置检测成功。
- 管理员邮箱为 `admin@sub.bahew.com`，管理员密码保存在 `/opt/sub2api-deploy/.env`。
- 当前应用镜像由 `/opt/sub2api-src` 的源码构建，不是官方 `weishaw/sub2api:latest` 镜像。

## 15. 故障排查

端口冲突：

```bash
sudo ss -lntp | grep -E ':(80|443|8080)\b'
```

构建失败：

```bash
cd /opt/sub2api-deploy
docker compose build --no-cache sub2api
```

应用无法启动：

```bash
cd /opt/sub2api-deploy
docker compose logs --tail=200 sub2api
docker compose logs --tail=100 postgres
docker compose logs --tail=100 redis
```

宝塔 Nginx 502：

```bash
curl -v http://127.0.0.1:8080/health
sudo /www/server/nginx/sbin/nginx -t
sudo tail -n 100 /www/wwwlogs/nginx_error.log 2>/dev/null || true
```

证书申请失败：

```bash
getent ahosts sub.bahew.com
curl -I http://sub.bahew.com/.well-known/acme-challenge/test || true
bt status
```

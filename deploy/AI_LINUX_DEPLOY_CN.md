# Sub2API Linux 混合模式部署指南 (宝塔面板 + Docker + 外部 PostgreSQL)

本文档专为在 Linux 服务器上使用**宝塔面板**、**Nginx** 和**宿主机 PostgreSQL**，并结合 **Docker** 运行 Sub2API 核心服务的混合部署模式而编写。本文档还详细说明了本地二次开发代码如何通过 GitHub 同步并在服务器上快速更新构建。

---

## 1. 架构设计与路径约定

在本项目中，为了最大化利用宝塔面板的便捷管理，同时保持服务的独立性与安全性，我们采用以下混合部署方案：

- **宝塔面板**：用于管理 Nginx、安全组、SSL 证书以及系统的防火墙。
- **Nginx (宝塔安装)**：作为反向代理，启用 `underscores_in_headers` 以支持自定义请求头，并配置 SSL。
- **PostgreSQL**：运行在宿主机（由宝塔或系统安装），使用已建好的数据库：
  - **数据库名**：`sub2api_fork`
  - **用户名**：`sub2api_fork`
  - **密码**：`ArKMbcKDJ4bpWx4x`
  - **地址**：宿主机内网 IP 或 `172.17.0.1`（Docker 网桥的宿主机 IP，或使用宿主机公网 IP）。
- **Sub2API & Redis**：通过 Docker 运行。Sub2API 使用**本地二开源码**在服务器现场构建，以确保二开逻辑生效。

### 目录与路径映射

按照宝塔面板和一般 Linux 规范：
- **网站安装/源码目录** (`/www/wwwroot/sub2api-fork`)：用于存放同步自 GitHub 的最新二次开发源码。
- **部署与数据运行目录** (`/www/wwwroot/sub2api-deploy`)：用于存放 `docker-compose.yml`、`.env` 配置文件以及 Redis、运行日志等数据。
- **备份目录** (`/www/backup/sub2api`)：用于存储备份文件。

---

## 2. 部署前置检查与准备

在开始前，请登录服务器执行以下确认：

### 2.1 确认宿主机数据库连接
确保 Docker 容器能够连接到宿主机的 PostgreSQL。
1. 在宝塔面板中，进入 **数据库** -> **PostgreSQL**。
2. 确保 `sub2api_fork` 数据库的“访问权限”设置为“所有人”或“指定IP”（添加 `172.17.0.0/16` 或 `172.18.0.0/16` 允许 Docker 容器访问）。
3. 记录宿主机在 Docker 网卡上的 IP（通常为 `172.17.0.1`）。可以通过在宿主机运行以下命令获取：
   ```bash
   ip addr show docker0 | grep -Po 'inet \K[\d.]+'
   ```

### 2.2 安装 Docker & Docker Compose
如果服务器尚未安装 Docker，请先安装：
```bash
sudo apt-get update && sudo apt-get install -y curl git openssl

# 安装 Docker
if ! command -v docker >/dev/null 2>&1; then
  curl -fsSL https://get.docker.com | bash -s docker
fi
sudo systemctl enable --now docker
```

---

## 3. 步骤一：拉取二开源码

我们将本地二次开发的代码推送到 GitHub（例如：`https://github.com/your-username/sub2api-fork.git`），然后在服务器拉取。

```bash
# 创建网站安装目录并赋予权限
sudo mkdir -p /www/wwwroot/sub2api-fork
sudo chown -R $USER:$USER /www/wwwroot/sub2api-fork

# 克隆代码
git clone https://github.com/your-username/sub2api-fork.git /www/wwwroot/sub2api-fork
```

---

## 4. 步骤二：配置部署环境

我们创建一个独立的运行目录 `/www/wwwroot/sub2api-deploy`，避免源码目录被日志、持久化卷和 `.env` 配置文件污染。

```bash
# 创建部署与运行数据目录
sudo mkdir -p /www/wwwroot/sub2api-deploy/redis_data
sudo mkdir -p /www/wwwroot/sub2api-deploy/data
sudo chown -R $USER:$USER /www/wwwroot/sub2api-deploy
cd /www/wwwroot/sub2api-deploy
```

### 4.1 编写 `docker-compose.yml`
在此方案中，我们剔除了 PostgreSQL 容器，直接使用宿主机的数据库。Redis 仍运行 in Docker 中，Sub2API 基于前面的源码目录现场构建。

在 `/www/wwwroot/sub2api-deploy` 下创建 `docker-compose.yml`：

```yaml
version: '3.8'

services:
  sub2api:
    build:
      context: /www/wwwroot/sub2api-fork
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
      
      # 外部 PostgreSQL 数据库配置（指向宿主机）
      - DATABASE_HOST=${DATABASE_HOST:-172.17.0.1}
      - DATABASE_PORT=${DATABASE_PORT:-5432}
      - DATABASE_USER=${POSTGRES_USER:-sub2api_fork}
      - DATABASE_PASSWORD=${POSTGRES_PASSWORD:-ArKMbcKDJ4bpWx4x}
      - DATABASE_DBNAME=${POSTGRES_DB:-sub2api_fork}
      - DATABASE_SSLMODE=disable
      - DATABASE_MAX_OPEN_CONNS=${DATABASE_MAX_OPEN_CONNS:-256}
      - DATABASE_MAX_IDLE_CONNS=${DATABASE_MAX_IDLE_CONNS:-128}
      
      # 容器内 Redis 配置
      - REDIS_HOST=redis
      - REDIS_PORT=6379
      - REDIS_PASSWORD=${REDIS_PASSWORD:-}
      - REDIS_DB=${REDIS_DB:-0}
      - REDIS_POOL_SIZE=${REDIS_POOL_SIZE:-4096}
      - REDIS_MIN_IDLE_CONNS=${REDIS_MIN_IDLE_CONNS:-256}
      
      # 业务变量
      - ADMIN_EMAIL=${ADMIN_EMAIL:-admin@sub2api.local}
      - ADMIN_PASSWORD=${ADMIN_PASSWORD}
      - JWT_SECRET=${JWT_SECRET}
      - JWT_EXPIRE_HOUR=${JWT_EXPIRE_HOUR:-24}
      - TOTP_ENCRYPTION_KEY=${TOTP_ENCRYPTION_KEY}
      - TZ=${TZ:-Asia/Shanghai}
      
      # 安全配置
      - SECURITY_URL_ALLOWLIST_ENABLED=false
    depends_on:
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
```

### 4.2 写入环境变量文件 `.env`
自动生成高强度安全密钥，并配置外部数据库信息。执行以下脚本直接生成 `.env`：

```bash
cd /www/wwwroot/sub2api-deploy

# 获取代码最新 Commit Hash 作为构建 Tag
APP_COMMIT="$(cd /www/wwwroot/sub2api-fork && git rev-parse --short HEAD 2>/dev/null || date +%Y%m%d%H%M%S)"
JWT_SECRET="$(openssl rand -hex 32)"
TOTP_ENCRYPTION_KEY="$(openssl rand -hex 32)"
ADMIN_PASSWORD="$(openssl rand -base64 24 | tr -d '=+/' | cut -c1-20)"
REDIS_PASSWORD="$(openssl rand -hex 16)"

cat > .env <<EOF
APP_COMMIT=${APP_COMMIT}
APP_TAG=${APP_COMMIT}
BIND_HOST=127.0.0.1
SERVER_PORT=8080
SERVER_MODE=release
RUN_MODE=standard
TZ=Asia/Shanghai

# PostgreSQL 宿主机配置
DATABASE_HOST=127.0.0.1
DATABASE_PORT=5432
POSTGRES_USER=sub2api_fork
POSTGRES_PASSWORD=ArKMbcKDJ4bpWx4x
POSTGRES_DB=sub2api_fork

# Redis 配置
REDIS_PASSWORD=${REDIS_PASSWORD}

# 超级管理员（首次运行自动初始化）
ADMIN_EMAIL=admin@yourdomain.com
ADMIN_PASSWORD=${ADMIN_PASSWORD}

# 安全密钥
JWT_SECRET=${JWT_SECRET}
JWT_EXPIRE_HOUR=24
TOTP_ENCRYPTION_KEY=${TOTP_ENCRYPTION_KEY}

# 构建源代理（可加快依赖下载）
GOPROXY=https://goproxy.cn,direct
GOSUMDB=sum.golang.google.cn
EOF

chmod 600 .env
```

---

## 5. 步骤三：构建并启动容器

在运行目录执行以下构建和拉起容器的命令：

```bash
cd /www/wwwroot/sub2api-deploy

# 构建应用镜像并拉起服务
docker compose build --pull sub2api
docker compose up -d

# 检查容器状态
docker compose ps
```

### 验证应用是否健康：
```bash
curl -fsS http://127.0.0.1:8080/health
```
如果返回 `ok` 或 `{ "status": "ok" }` 则说明 Sub2API 已成功运行，数据库迁移和连接已通过验证。

---

## 6. 步骤四：配置宝塔 Nginx 反代与 SSL

### 6.1 网站配置
1. 登录宝塔面板，进入 **网站** -> **添加站点**。
2. **域名** 填写您的解析域名（如 `sub.yourdomain.com`）。
3. **根目录** 默认为 `/www/wwwroot/sub.yourdomain.com`，可以不用管（PHP 版本选择“纯静态”）。
4. 进入该站点的 **设置** -> **反向代理** -> **添加反向代理**：
   - **代理名称**：`sub2api`
   - **目标URL**：`http://127.0.0.1:8080`
   - **发送域名**：`$host`
5. 点击提交。

### 6.2 启用自定义 Headers 支持 (关键步)
Sub2API 强依赖特定的 HTTP Headers 传输元数据。如果 Nginx 未开启下划线 Headers 支持，代理会丢弃这些关键信息。

1. 在宝塔面板中，进入 **软件商店** -> **Nginx 对应的设置** -> **配置修改**。
2. 寻找 `http { ... }` 部分，并在里面添加一行：
   ```nginx
   underscores_in_headers on;
   ```
3. 保存并重载 Nginx 配置。

### 6.3 申请 HTTPS 证书
1. 网站设置中进入 **SSL** 面板，选择 **Let's Encrypt**。
2. 勾选您的域名进行申请，成功后开启右侧的 **强制HTTPS** 按钮。

---

## 7. 二次开发版日常更新流程

当本地代码修改并推送到 GitHub 后，您可以使用以下标准化流程无缝更新服务器版本。

为了极致的安全与自动化，我们可以将以下逻辑保存为升级脚本 `/www/wwwroot/sub2api-deploy/update.sh` :

### 7.1 创建升级脚本
```bash
cat > /www/wwwroot/sub2api-deploy/update.sh <<'EOF'
#!/bin/bash
# =============================================================================
# Sub2API 滚动更新脚本
# =============================================================================
set -e

SRC_DIR="/www/wwwroot/sub2api-fork"
DEPLOY_DIR="/www/wwwroot/sub2api-deploy"
BACKUP_DIR="/www/backup/sub2api"

echo "=== [1/5] 备份当前数据库与配置 ==="
mkdir -p "$BACKUP_DIR"
cd "$DEPLOY_DIR"
# 备份 .env 与当前运行的 Redis 状态
tar czf "$BACKUP_DIR/deploy-backup-$(date +%Y%m%d-%H%M%S).tar.gz" .env data/

echo "=== [2/5] 拉取 GitHub 最新二开代码 ==="
cd "$SRC_DIR"
git fetch --all
git reset --hard origin/main # 强制同步为 Github 的最新 main 分支

# 获取最新 commit hash
NEW_COMMIT=$(git rev-parse --short HEAD)
echo "当前更新版本 Commit: $NEW_COMMIT"

echo "=== [3/5] 更新构建配置 ==="
cd "$DEPLOY_DIR"
sed -i "s/^APP_COMMIT=.*/APP_COMMIT=${NEW_COMMIT}/" .env
sed -i "s/^APP_TAG=.*/APP_TAG=${NEW_COMMIT}/" .env

echo "=== [4/5] 现场构建新版本镜像 ==="
docker compose build sub2api

echo "=== [5/5] 重启容器应用 ==="
docker compose up -d sub2api

echo "=== 更新完成，进行健康检查 ==="
sleep 3
if curl -fsS http://127.0.0.1:8080/health >/dev/null; then
  echo ">>> [SUCCESS] 升级成功！服务运行正常。"
else
  echo ">>> [ERROR] 健康检查失败，请检查 Docker 日志！"
  docker compose logs --tail=50 sub2api
fi
EOF

chmod +x /www/wwwroot/sub2api-deploy/update.sh
```

### 7.2 执行更新
以后需要同步更新时，只需连接服务器并运行一行命令即可：
```bash
/www/wwwroot/sub2api-deploy/update.sh
```

---

## 8. 故障排除与维护常用命令

进入运行目录 `/www/wwwroot/sub2api-deploy` 执行以下操作：

- **查看服务状态**：
  ```bash
  docker compose ps
  ```
- **查看实时日志**：
  ```bash
  docker compose logs -f sub2api
  ```
- **重启服务**：
  ```bash
  docker compose restart sub2api
  ```
- **获取超级管理员初始密码**：
  由于密码是由程序随机生成并写入 `.env` 的，如需获取，请登录服务器执行：
  ```bash
  cat /www/wwwroot/sub2api-deploy/.env | grep ADMIN_PASSWORD
  ```

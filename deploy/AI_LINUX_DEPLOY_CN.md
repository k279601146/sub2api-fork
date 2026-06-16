# AI 助手 Linux 自动部署指南

本文档用于指导 AI 运维助手把 Sub2API 自动部署到 Linux 服务器，并通过域名 `sub.bahew.com` 对外提供 HTTPS 服务。

默认部署方式：

- 使用 Docker Compose 部署 Sub2API、PostgreSQL、Redis。
- 使用宿主机 Nginx 反向代理到 `127.0.0.1:8080`。
- 使用 Certbot 申请和续期 Let's Encrypt 证书。
- 不启动浏览器测试，所有验收通过 `curl`、`docker compose`、`systemctl` 完成。

## 交给 AI 助手的任务说明

可以把下面这段直接发给具备 SSH 权限的 AI 运维助手：

```text
请在一台 Linux 服务器上自动部署 Sub2API。

固定域名：sub.bahew.com
部署目录：/opt/sub2api-deploy
应用监听：127.0.0.1:8080
反向代理：Nginx
HTTPS：Let's Encrypt / Certbot
数据目录：/opt/sub2api-deploy/data、postgres_data、redis_data

要求：
1. 先检查系统、DNS、端口和已有服务，不要覆盖已有部署。
2. 使用 Docker Compose 部署 Sub2API、PostgreSQL、Redis。
3. 自动生成强随机 POSTGRES_PASSWORD、JWT_SECRET、TOTP_ENCRYPTION_KEY。
4. 不在日志或回复中明文输出密钥；只说明密钥已写入 /opt/sub2api-deploy/.env。
5. Nginx 必须开启 underscores_in_headers on，以兼容带下划线的请求头。
6. 配置 sub.bahew.com 的 HTTPS 反向代理。
7. 不启动浏览器测试；使用 curl 验证 /health 和 HTTPS 首页响应。
8. 完成后输出服务状态、访问地址、管理员账号获取方式、常用运维命令。
```

## 部署前检查

AI 助手开始执行前必须确认：

- 已获得服务器 SSH 权限，并具备 `sudo` 权限。
- `sub.bahew.com` 的 DNS `A` 或 `AAAA` 记录已经指向目标服务器公网 IP。
- 服务器的安全组和防火墙允许入站 `80/tcp`、`443/tcp`。
- 服务器上没有正在使用 `80`、`443`、`8080` 的冲突服务，或已经确认可以调整。

检查命令：

```bash
set -euo pipefail

DOMAIN="sub.bahew.com"
APP_DIR="/opt/sub2api-deploy"
APP_PORT="8080"

hostnamectl || true
id
command -v docker || true
command -v nginx || true
ss -lntp | grep -E ':(80|443|8080)\b' || true
getent ahosts "$DOMAIN" || true
curl -4s ifconfig.me || true
```

如果 DNS 没有解析到当前服务器，暂停部署并提示用户先修改 DNS。证书签发依赖公网 DNS 和 `80/tcp` 可访问。

## 安装基础依赖

以下命令适用于 Ubuntu/Debian。其他发行版需要按系统包管理器等价安装 Docker、Nginx、Certbot。

```bash
sudo apt-get update
sudo apt-get install -y ca-certificates curl gnupg lsb-release openssl nginx certbot python3-certbot-nginx
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

## 准备部署目录

不要在已有部署目录中无提示覆盖文件。若目录已存在，先检查内容并征得用户确认。

```bash
sudo mkdir -p "$APP_DIR"
sudo chown "$USER":"$USER" "$APP_DIR"
cd "$APP_DIR"

if [ -f docker-compose.yml ] || [ -f .env ]; then
  echo "部署目录已存在，请先确认是否为旧部署：$APP_DIR"
  ls -la "$APP_DIR"
  exit 1
fi
```

下载官方 Docker Compose 本地目录版配置：

```bash
curl -fsSL https://raw.githubusercontent.com/Wei-Shaw/sub2api/main/deploy/docker-compose.local.yml -o docker-compose.yml
curl -fsSL https://raw.githubusercontent.com/Wei-Shaw/sub2api/main/deploy/.env.example -o .env.example
cp .env.example .env
mkdir -p data postgres_data redis_data
chmod 700 data postgres_data redis_data
```

## 写入生产环境配置

自动生成密钥并写入 `.env`。不要在最终回复里粘贴这些密钥。

```bash
POSTGRES_PASSWORD="$(openssl rand -hex 32)"
JWT_SECRET="$(openssl rand -hex 32)"
TOTP_ENCRYPTION_KEY="$(openssl rand -hex 32)"
ADMIN_PASSWORD="$(openssl rand -base64 24 | tr -d '=+/' | cut -c1-20)"

sed -i "s/^BIND_HOST=.*/BIND_HOST=127.0.0.1/" .env
sed -i "s/^SERVER_PORT=.*/SERVER_PORT=8080/" .env
sed -i "s/^SERVER_MODE=.*/SERVER_MODE=release/" .env
sed -i "s/^TZ=.*/TZ=Asia\/Shanghai/" .env
sed -i "s/^POSTGRES_PASSWORD=.*/POSTGRES_PASSWORD=${POSTGRES_PASSWORD}/" .env
sed -i "s/^JWT_SECRET=.*/JWT_SECRET=${JWT_SECRET}/" .env
sed -i "s/^TOTP_ENCRYPTION_KEY=.*/TOTP_ENCRYPTION_KEY=${TOTP_ENCRYPTION_KEY}/" .env
sed -i "s/^ADMIN_EMAIL=.*/ADMIN_EMAIL=admin@sub.bahew.com/" .env
sed -i "s/^ADMIN_PASSWORD=.*/ADMIN_PASSWORD=${ADMIN_PASSWORD}/" .env

chmod 600 .env
```

如果需要启用 Simple Mode，可以额外写入：

```bash
sed -i "s/^RUN_MODE=.*/RUN_MODE=simple/" .env
grep -q '^SIMPLE_MODE_CONFIRM=' .env || echo 'SIMPLE_MODE_CONFIRM=true' >> .env
```

## 启动 Sub2API

```bash
cd "$APP_DIR"
docker compose up -d
docker compose ps
docker compose logs --tail=100 sub2api
```

本地健康检查：

```bash
curl -fsS http://127.0.0.1:8080/health
```

如果 `/health` 没有返回成功，先查看日志，不要继续配置 HTTPS：

```bash
docker compose logs --tail=200 sub2api
docker compose logs --tail=100 postgres
docker compose logs --tail=100 redis
```

## 配置 Nginx 反向代理

创建站点配置：

```bash
sudo tee /etc/nginx/sites-available/sub2api.conf >/dev/null <<'NGINX'
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
        proxy_read_timeout 3600s;
        proxy_send_timeout 3600s;
    }
}
NGINX
```

确保 Nginx `http` 块开启下划线请求头支持。Sub2API 的多账号粘性会话会用到带下划线的 header，缺少此配置可能导致路由异常。

```bash
sudo tee /etc/nginx/conf.d/sub2api-common.conf >/dev/null <<'NGINX'
underscores_in_headers on;

map $http_upgrade $connection_upgrade {
    default upgrade;
    '' close;
}
NGINX
```

启用站点并重载：

```bash
sudo ln -sf /etc/nginx/sites-available/sub2api.conf /etc/nginx/sites-enabled/sub2api.conf
sudo nginx -t
sudo systemctl enable --now nginx
sudo systemctl reload nginx
```

HTTP 验证：

```bash
curl -I http://sub.bahew.com/health
```

## 申请 HTTPS 证书

执行前确认 `80/tcp` 已对公网开放。

```bash
sudo certbot --nginx -d sub.bahew.com --redirect --agree-tos -m admin@sub.bahew.com --no-eff-email
sudo nginx -t
sudo systemctl reload nginx
```

验证自动续期：

```bash
sudo certbot renew --dry-run
```

HTTPS 验收：

```bash
curl -fsS https://sub.bahew.com/health
curl -I https://sub.bahew.com/
```

## 防火墙建议

如果使用 UFW：

```bash
sudo ufw allow OpenSSH
sudo ufw allow 'Nginx Full'
sudo ufw deny 8080/tcp || true
sudo ufw status verbose
```

由于应用已经绑定 `127.0.0.1:8080`，公网无法直接访问 8080；防火墙规则只是额外保护。

## 管理员账号

默认写入：

- 管理员邮箱：`admin@sub.bahew.com`
- 管理员密码：保存在 `/opt/sub2api-deploy/.env` 的 `ADMIN_PASSWORD`

查看管理员密码时只在服务器本机执行，不要把密码发到公共日志：

```bash
cd /opt/sub2api-deploy
grep '^ADMIN_PASSWORD=' .env
```

首次登录后请立即修改管理员密码，并妥善备份 `.env`。

## 常用运维命令

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

# 更新镜像并重建容器
docker compose pull
docker compose up -d

# 查看 Nginx 状态
sudo systemctl status nginx

# 查看证书
sudo certbot certificates
```

## 备份与恢复

备份前建议短暂停止写入：

```bash
cd /opt
sudo tar czf "sub2api-backup-$(date +%Y%m%d-%H%M%S).tar.gz" sub2api-deploy
```

恢复到新服务器：

```bash
cd /opt
sudo tar xzf sub2api-backup-YYYYMMDD-HHMMSS.tar.gz
sudo chown -R "$USER":"$USER" /opt/sub2api-deploy
cd /opt/sub2api-deploy
docker compose up -d
```

## 回滚

如果更新后异常，优先回到更新前的镜像版本或恢复备份。

```bash
cd /opt/sub2api-deploy
docker compose logs --tail=200 sub2api
docker compose down

# 使用最近备份恢复时，先确认备份文件名
cd /opt
sudo mv sub2api-deploy "sub2api-deploy.broken.$(date +%Y%m%d-%H%M%S)"
sudo tar xzf sub2api-backup-YYYYMMDD-HHMMSS.tar.gz
sudo chown -R "$USER":"$USER" /opt/sub2api-deploy
cd /opt/sub2api-deploy
docker compose up -d
```

## 完成标准

部署完成时，AI 助手应给出以下信息：

- `docker compose ps` 中 `sub2api`、`postgres`、`redis` 均为运行或健康状态。
- `curl -fsS http://127.0.0.1:8080/health` 成功。
- `curl -fsS https://sub.bahew.com/health` 成功。
- `https://sub.bahew.com/` 可访问。
- Nginx 配置检测 `sudo nginx -t` 成功。
- Certbot 续期测试 `sudo certbot renew --dry-run` 成功。
- 管理员邮箱为 `admin@sub.bahew.com`，管理员密码保存在 `/opt/sub2api-deploy/.env`。

## 故障排查

端口冲突：

```bash
sudo ss -lntp | grep -E ':(80|443|8080)\b'
```

应用无法启动：

```bash
cd /opt/sub2api-deploy
docker compose logs --tail=200 sub2api
docker compose logs --tail=100 postgres
docker compose logs --tail=100 redis
```

证书申请失败：

```bash
getent ahosts sub.bahew.com
curl -I http://sub.bahew.com/.well-known/acme-challenge/test || true
sudo journalctl -u nginx -n 100 --no-pager
sudo certbot certificates
```

Nginx 502：

```bash
curl -v http://127.0.0.1:8080/health
sudo nginx -t
sudo tail -n 100 /var/log/nginx/error.log
```

# Sub2API 远程镜像部署说明

适用场景：Windows 本地开发、构建并推送镜像到腾讯云 TCR；Linux 宝塔服务器只负责 `docker compose pull` 和 `docker compose up -d`，不再拉源码、不再现场构建镜像。

## 1. 服务器部署目录

服务器运行目录：

```bash
/www/wwwroot/sub2api-deploy/deploy
```

把本仓库 `deploy/` 下的文件同步到该目录：

- `docker-compose.yml`
- `.env.example`
- `update.sh`
- `docker-entrypoint.sh`

首次准备：

```bash
cd /www/wwwroot/sub2api-deploy/deploy
cp .env.example .env
chmod 600 .env
chmod +x update.sh docker-entrypoint.sh
```

## 2. 必填镜像配置

编辑服务器 `.env`：

```ini
SUB2API_IMAGE=ccr.ccs.tencentyun.com/你的命名空间/sub2api
APP_TAG=latest
```

其他数据库、Redis、JWT、管理员账号等生产配置继续保留在 `.env` 中。

## 3. 本地构建并推送

Windows PowerShell 中执行：

```powershell
$env:TCR_REGISTRY = "ccr.ccs.tencentyun.com"
$env:TCR_NAMESPACE = "你的命名空间"
.\scripts\build-push-tcr.ps1
```

脚本默认使用当前 Git commit 短 hash 作为镜像 tag，并同时推送 `latest`。也可以指定 tag：

```powershell
.\scripts\build-push-tcr.ps1 -Tag "20260628-001"
```

## 4. 服务器一键部署

使用本地脚本输出的 tag：

```bash
cd /www/wwwroot/sub2api-deploy/deploy
./update.sh 20260628-001
```

`update.sh` 会：

1. 备份 `.env`、`data/`、`redis_data/`。
2. 确保运行目录存在。
3. 拉取 `${SUB2API_IMAGE}:${APP_TAG}` 和 Redis 镜像。
4. 重建 `sub2api`、`redis` 容器。
5. 检查 `/health`。

## 5. 数据目录

以下目录是运行数据，不要删除：

- `data/`：应用配置、日志、GeoLite、页面等运行文件。
- `redis_data/`：Redis AOF/RDB 持久化数据。

只要部署目录和 compose 的 `volumes` 不变，并且不执行 `docker compose down -v`，切换远程镜像部署后数据仍然保存在这些目录里。

#!/bin/bash
# =============================================================================
# Sub2API 滚动构建与一键部署升级脚本 (集成 GitHub Token)
# =============================================================================
set -e

SRC_DIR="/www/wwwroot/sub2api-fork"
DEPLOY_DIR="/www/wwwroot/sub2api-deploy"
BACKUP_DIR="/www/backup/sub2api"
BRANCH="idehotai"

# GitHub 私有仓库凭证
GITHUB_TOKEN="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"
# 使用 GITHUB_TOKEN 的鉴权 URL 格式
AUTH_REPO_URL="https://oauth2:${GITHUB_TOKEN}@github.com/k279601146/sub2api-fork.git"
# 转换成使用 gitclone 镜像加速的鉴权 URL
ACCELERATED_REPO_URL="https://oauth2:${GITHUB_TOKEN}@gitclone.com/github.com/k279601146/sub2api-fork.git"

echo "=== [1/5] 备份当前运行数据与配置 ==="
mkdir -p "$BACKUP_DIR"
if [ -d "$DEPLOY_DIR" ] && [ -f "$DEPLOY_DIR/.env" ]; then
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

echo "=== [3/5] 更新部署目录配置 ==="
cd "$DEPLOY_DIR"
if [ -f .env ]; then
  sed -i "s/^APP_COMMIT=.*/APP_COMMIT=${NEW_COMMIT}/" .env
  sed -i "s/^APP_TAG=.*/APP_TAG=${NEW_COMMIT}/" .env
else
  echo "[WARNING] 部署目录下的 .env 文件不存在，请参考部署文档先生成 .env！"
fi

echo "=== [4/5] 现场构建新版本 Docker 镜像 ==="
docker compose build sub2api

echo "=== [5/5] 重启并运行新容器 ==="
docker compose up -d sub2api

echo "=== 更新完成，进行健康检查 ==="
sleep 3
if curl -fsS http://127.0.0.1:8080/health >/dev/null; then
  echo ">>> [SUCCESS] 升级成功！服务运行正常。最新 Commit: $NEW_COMMIT"
else
  echo ">>> [ERROR] 健康检查失败，请检查 Docker 日志！"
  docker compose logs --tail=50 sub2api
fi

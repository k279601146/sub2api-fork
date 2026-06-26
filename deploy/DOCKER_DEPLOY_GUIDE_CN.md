# Sub2API Docker 混合部署与配置说明 (宝塔面板 + Docker + 宿主机 PostgreSQL)

为了配合你的部署需求，我们对项目中的 `deploy/` 文件夹进行了清理，删除了所有不必要的非 Docker 安装脚本，并优化了更新脚本 `deploy/update.sh` 的安全性，不再硬编码敏感 Token。

以下是关于如何将这些文件部署到 Linux 服务器以及需要配置哪些文件的详细指南。

---

## 1. 部署文件结构与准备

### 哪些文件需要上传到 `/www/wwwroot/sub2api-deploy/`？
在清理 `deploy` 文件夹之后，你只需要把本地 `deploy/` 下的以下核心部署文件上传到 Linux 服务器的 `/www/wwwroot/sub2api-deploy/` 目录：

1. **`docker-compose.yml`**：Docker 容器编排文件。
2. **`docker-entrypoint.sh`**：容器初始化入口脚本（用于自动修复卷权限和启动服务）。
3. **`.env.example`**：环境变量模板文件。
4. **`update.sh`**：用于拉取最新代码、自动构建和滚动更新镜像的升级脚本。
5. **`codex-instructions.md.tmpl`**：(可选) 顶层 Codex instructions 模板文件（如果不使用 VS Code/Codex 等代理，可不挂载或不配置）。

此外，**在上传后，请在服务器上为 `update.sh` 和 `docker-entrypoint.sh` 赋予可执行权限**：
```bash
chmod +x /www/wwwroot/sub2api-deploy/update.sh
chmod +x /www/wwwroot/sub2api-deploy/docker-entrypoint.sh
```

---

## 2. 配置文件说明：`config.yaml` 与 `.env` 是否重复？

在 Sub2API 的架构设计中，`.env`（环境变量）与 `config.yaml`（配置文件）**分工明确，并不完全重复，但会有部分交集**：

### 2.1 `.env` (环境变量文件)
* **作用**：控制容器的系统级启动参数、端口映射、卷挂载，以及注入诸如数据库连接密码、JWT 密钥等**高敏感密钥**。
* **修改位置**：服务器上的 `/www/wwwroot/sub2api-deploy/.env`。
* **优先级**：**环境变量的优先级是最高的**。如果 `.env` 注入了某个变量（例如 `DATABASE_PASSWORD`），Go 程序读取配置时会直接覆盖 `config.yaml` 对应字段的默认值。

### 2.2 `config.yaml` (全局应用配置文件)
* **作用**：用于配置更细粒度的业务参数（例如：流式超时、地区模型隔离策略、并发控制策略、OAuth 接入细节等）。
* **修改位置**：**在容器运行中，所有的配置管理都可以在 Sub2API 的【后台管理面板】可视化修改并保存**。系统会自动将修改写入容器内的 `/app/data/config.yaml`，即映射回宿主机的 `/www/wwwroot/sub2api-deploy/data/config.yaml`。
* **注意**：本地 `deploy/config.yaml` 是本地开发或者参考用的。你在首次部署时，程序在检测到没有 `config.yaml` 时会自动根据内置默认值和 `.env` 注入的变量生成一份全新的 `config.yaml` 放在 `data/` 目录下。
* **结论**：**因此，你不需要手动去配置 `config.yaml` 文件**。你只需要配置好 `.env`，剩余的业务配置等到容器启动后，登录管理后台直接进行可视化配置即可。

---

## 3. 服务器 `.env` 文件的具体配置

在服务器的 `/www/wwwroot/sub2api-deploy/` 下，请根据 `.env.example` 创建你的 `.env` 文件：
```bash
cp .env.example .env
```
然后编辑 `.env`：
```ini
# -----------------------------------------------------------------------------
# 基础配置
# -----------------------------------------------------------------------------
BIND_HOST=127.0.0.1
SERVER_PORT=8080
SERVER_MODE=release
TZ=Asia/Shanghai

# -----------------------------------------------------------------------------
# 宿主机 PostgreSQL 数据库配置 (按你的宝塔数据库信息填写)
# -----------------------------------------------------------------------------
DATABASE_PORT=5432
POSTGRES_USER=sub2api_fork
POSTGRES_PASSWORD=ArKMbcKDJ4bpWx4x
POSTGRES_DB=sub2api_fork

# 如果容器内后端无法通过 127.0.0.1 访问宿主机 PG，请在 config.yaml 自动生成后，
# 或在环境变量中将数据库 HOST 设置为 Docker 默认网桥网关 IP (通常是 172.17.0.1)
DATABASE_HOST=172.17.0.1

# -----------------------------------------------------------------------------
# 内部容器 Redis 密码配置 (由你自定义一个复杂密码即可)
# -----------------------------------------------------------------------------
REDIS_PASSWORD=YourSecureRedisPasswordHere

# -----------------------------------------------------------------------------
# 首次运行自动初始化管理员配置 (如果你不配置，程序会在首次启动时自动随机生成)
# -----------------------------------------------------------------------------
ADMIN_EMAIL=admin@yourdomain.com
ADMIN_PASSWORD=YourCustomAdminPassword

# -----------------------------------------------------------------------------
# 安全与加密密钥 (必须为 64 位 16 进制字符串，推荐在本地生成后填入)
# 生成命令: openssl rand -hex 32
# -----------------------------------------------------------------------------
JWT_SECRET=f4a0b2...您的JWT高强度密钥
TOTP_ENCRYPTION_KEY=d9c1e3...您的TOTP双因素认证加密密钥

# -----------------------------------------------------------------------------
# 升级与日常二开代码更新配置 (用于 update.sh 脚本)
# -----------------------------------------------------------------------------
# 拉取你的私有 GitHub 仓库的 Personal Access Token (必须具备 repo 权限)
GITHUB_TOKEN=ghp_YourGitHubPersonalAccessToken

# 你的二次开发代码分支，自动拉取并构建此分支的代码
BRANCH=idehotai
```

---

## 4. 首次部署与日常更新命令

### 4.1 首次部署与启动服务
首次部署时，你**不需要手动克隆源码**。上传好部署文件并配置好 `.env`（确认已填写 `GITHUB_TOKEN`）后，直接运行升级脚本即可一键完成源码克隆、镜像构建和容器启动：

```bash
# 运行一键部署升级脚本
/www/wwwroot/sub2api-deploy/update.sh
```

> **提示**：运行该脚本后，它会检测到 `/www/wwwroot/sub2api-fork` 目录不存在，并自动使用 `.env` 中配置的 `GITHUB_TOKEN` 完成私有仓库克隆，随后现场构建镜像并拉起服务。

### 4.2 极简一键更新升级
当你在本地修改二开代码，推送到 GitHub 后，只需在服务器上执行：
```bash
/www/wwwroot/sub2api-deploy/update.sh
```
该脚本会自动：
1. 备份当前的 `.env` 和 `data/` 配置目录。
2. 强制拉取最新的二开分支代码。
3. 读取 `.env` 中的 `GITHUB_TOKEN` 凭证完成私有库拉取。
4. 现场执行 Docker 镜像的重新构建。
5. 滚动重启 `sub2api` 容器并进行健康检查。

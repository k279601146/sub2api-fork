# Sub2API 本地运行指南

本文档记录了在本地开发环境下启动 Sub2API 服务所需的步骤。

## 1. 环境准备

确保以下服务正在运行：
*   **PostgreSQL**: `localhost:5432` (用户/密码/数据库: `sub2api` 或 `postgres`)
*   **Redis**: `localhost:6379`

## 2. 启动后端服务 (Go)

后端服务监听在 `8080` 端口。

```bash
cd backend
go run ./cmd/server 2>&1 | tee server.log
```

## 3. 启动前端服务 (Vue/Vite)

前端服务监听在 `3000` 端口，并自动代理 `/api` 请求到后端。

```bash
cd frontend
pnpm install  # 首次运行需安装依赖
npm run dev
```

## 4. 访问系统

*   **访问地址**: [http://localhost:3000](http://localhost:3000)
*   **默认管理员账号**: `admin@sub2api.local`
*   **默认管理员密码**: `admin123`

---

## 5. 常见问题：关于配置文件路径

项目目前优先从以下路径加载配置：
1.  `D:\app\data\config.yaml` (优先级最高，因为代码中硬编码了 `/app/data`)
2.  `backend/config.yaml`

### 为什么配置文件在项目外？

在 `backend/internal/config/config.go` 中，代码使用了以下逻辑：
```go
viper.AddConfigPath("/app/data")
```
这种设计主要是为了 **Docker 部署优化**。在 Docker 容器中，`/app/data` 通常被挂载为一个持久化卷，用于存储配置和日志。

由于你的项目位于 `D:` 盘，Windows 会将绝对路径 `/app/data` 解析为 **`D:\app\data`**。这就是为什么它会出现在项目目录之外。

**建议**:
如果你希望将配置放回项目内，可以：
1.  删除 `D:\app\data\config.yaml`，系统将回退使用 `backend/config.yaml`。
2.  或者在启动时设置环境变量 `DATA_DIR` 指向你的项目目录。

# AGENTS.md

本文件为在本仓库工作的 AI agent、自动化编码助手和后续开发者提供项目级导航与约定。除非子目录中存在更具体的 `AGENTS.md`，否则以下规则适用于整个仓库。

## 服务器部署约定

- 当前部署基于服务器本机 Docker 构建，不是 CI 远端构建。修改 Dockerfile、compose、部署脚本或构建参数后，需要在服务器部署目录验证对应 Docker 构建流程。
- 本仓库源码目录是 `/www/wwwroot/sub2api-fork`。如果存在部署目录或 `deploy/` 下的运行副本，运行副本不应作为唯一修改来源；持久化修改必须回写到本仓库并提交/push 到 GitHub，避免重新部署时被源码版本覆盖。
- 和 `/www/wwwroot/dev2_OpenHarness_SaaS` 联动排查时，要同步检查两个仓库的部署脚本和 `AGENTS.md` 说明，尤其是服务器 Docker 构建、源码目录与部署目录同步、镜像重建触发条件这些信息。
- 仓库中已有若干本地备份或实验文件可能未跟踪，例如 `Dockerfile.fastbuild`、`Dockerfile.nopgclient`、`deploy/*.bak-*`。除非任务明确要求，不要删除、回滚或提交这些文件。

## 工作原则

- 开始改动前先检查 `git status --short --branch`，区分已跟踪改动和本地未跟踪文件。
- 保持改动聚焦，不做无关格式化、重命名或大规模重构。
- 不要提交密钥、真实用户数据、运行日志、上传文件、构建产物或缓存目录。

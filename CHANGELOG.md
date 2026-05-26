# Changelog

## v0.1.5 — 2026-05-26

- `GET /api/files/:id` 响应增加 `shares` 字段，展示关联分享的密码、有效期、下载次数信息
- PATCH 接口新增 `is_active` 字段，支持启用/禁用分享

## v0.1.4 — 2026-05-26

- API 分享端点支持通过 share code 定位（GET/PATCH/DELETE `/api/shares/:id`）
- 分享码生成规则改为必须包含至少一个字母和一个数字

## v0.1.3 — 2026-05-26

- 新增 `PATCH /api/shares/:id` 接口，支持更新分享密码、有效期、下载次数

## v0.1.2 — 2026-05-26

- 修复下载端点错误返回裸 JSON 的问题，改为重定向到 HTML 错误页面
- "由 Togos 驱动" 信息移至页面层级，确保所有状态可见
- 添加 GitHub 项目链接到页脚

## v0.1.1 — 2026-05-26

- Docker 支持：Dockerfile、docker-compose.yml、CI 自动推送镜像到 GHCR
- CI 构建矩阵新增 Windows arm64
- 版本号编译注入，启动日志显示版本
- 测试脚本覆盖 42 个测试用例

## v0.1.0 — 2026-05-26

首个正式版本。

### 功能

- 文件上传 API (`POST /api/files`)，支持 multipart/form-data
- 本地文件路径导入 API (`POST /api/files/local`)
- 文件列表、详情、删除 API
- 分享创建 API (`POST /api/shares`)，支持密码、有效期、下载次数保护
- 分享列表、详情、删除 API
- 公开分享下载页面 (`GET /s/:code`)，简洁清爽的 UI
- 密码保护分享，验证后 Cookie 持久化
- 下载次数限制，超出后返回 410
- 有效期限制，过期后返回 410
- 断点续传 (Range 请求支持)
- Bearer Token API 认证
- 公开端点速率限制 (30 次/分钟/IP)
- SQLite 持久化存储，WAL 模式
- 环境变量和命令行参数配置
- 单文件静态编译，无运行时依赖
- 跨平台支持：Linux、macOS、Windows (amd64/arm64)
- CLI 启动时自动生成管理 Token

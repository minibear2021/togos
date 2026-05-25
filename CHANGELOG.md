# Changelog

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

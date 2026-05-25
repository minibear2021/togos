# CLAUDE.md

## 项目概述

Togos 是一个轻量级文件分享工具，Go 开发，编译为单文件静态二进制。通过 REST API 管理文件上传和分享创建，支持密码、有效期、下载次数保护。

## 构建和运行

```bash
# 编译（必须禁用 CGo 以生成纯静态二进制）
CGO_ENABLED=0 go build -o togos .

# 运行测试服务
./togos -data-dir /tmp/togos-test -listen :19876 -admin-token test
# 通过 curl + API 测试功能

# 交叉编译
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o togos .
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o togos.exe .
```

## 项目结构

```
main.go         — 入口，路由，HTTP Server 配置
config.go       — 环境变量 + flag 解析
store.go        — SQLite 持久化，CRUD，密码哈希
api.go          — REST API 处理器
share.go        — 公开分享页面 + 下载处理 + HTML 模板
middleware.go   — 认证、速率限制、安全头、日志、recovery
API.md          — 完整 API 接口文档
README.md       — 项目说明
```

## 关键架构约定

### 单文件设计
- 编译时必须 `CGO_ENABLED=0`，确保纯 Go SQLite 驱动生效
- 唯一外部依赖是 `modernc.org/sqlite`（纯 Go SQLite，无 CGo）
- SQLite 连接串必须带 `?_journal_mode=WAL&_foreign_keys=on`
- SQLite 连接池设为 1（`SetMaxOpenConns(1)`），因为 SQLite 不支持并发写

### 路由分发
- `main.go` 中 `/s/` 路由根据 HTTP method 和 path suffix 分发到 share handler
- `/api/` 路由通过 `AuthMiddleware` 保护，走 `apiMux` 子路由
- `api.RouteAPI()` 在 switch 中按 path + method 做二次分发
- Go 1.22+ ServeMux 的 `/api/` pattern 匹配所有 `/api/*` 路径

### 数据模型
- 文件以 `<id>.<ext>` 存储在 `data/files/`，不保留原始文件名
- 分享码是 8 位随机小写字母数字 (`[a-z0-9]`)
- 密码用 SHA-256 + 32 字节随机盐，存储为 `hex(salt):hex(hash)`
- `expires_at` 存储 Unix 时间戳字符串，空字符串表示永不过期
- `max_downloads` 为 0 表示不限制下载次数
- 文件删除时 ON DELETE CASCADE 自动删除关联分享

### 安全规范
- API 端点必须通过 Bearer Token 认证
- 公开端点 `/s/*` 有速率限制（30 req/min/IP），API 端点不限制
- public file path 不在 API 响应中暴露（`json:"-"` 标签）
- password hash 不在 API 响应中暴露
- 所有用户输入在 HTML 输出前做 HTML 实体转义
- 使用 `filepath.Base()` 防路径遍历
- 分享码强制校验字符集 `[a-z0-9]`
- `Content-Disposition: attachment` 防止浏览器内联渲染文件

### 测试习惯
```bash
# 快速冒烟
./togos -data-dir /tmp/t -listen :19999 -admin-token x &
# 测试上传、创建分享、密码验证、下载、删除等流程
# 验证 429 速率限制触发
# 验证 401 未认证拦截
kill %1
```

### 修改代码注意事项
- 编译必须 `CGO_ENABLED=0`，否则会尝试用 CGo SQLite 驱动
- 分享页 HTML 模板 `sharePageTemplate` 在 `share.go` 末尾
- 修改路由分发逻辑时，注意 `/api/` pattern 在 Go 1.22+ 的语义
- RateLimiter 使用 IP 作为 key，清理协程每 5 分钟运行一次

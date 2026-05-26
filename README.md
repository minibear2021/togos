# Togos — 轻量级文件分享工具

单文件、跨平台的文件分享服务，通过 REST API 管理，支持密码、有效期、下载次数限制。

## 特性

- **单文件部署** — 编译为单一静态二进制，无运行时依赖
- **跨平台** — 支持 Linux、macOS、Windows (amd64/arm64)
- **REST API 管理** — 所有操作通过 API 完成，方便脚本化
- **多种保护** — 密码、有效期、下载次数三重限制
- **SQLite 持久化** — 零配置数据库，数据为单个 `.db` 文件
- **简洁分享页** — 用户访问分享链接看到干净的下载页面
- **安全性** — Token 认证、速率限制、XSS 防护、路径遍历防护

## 快速开始

### Docker（推荐）

```bash
# 拉取镜像
docker pull ghcr.io/minibear2021/togos:latest

# 运行
docker run -d -p 8080:8080 -v ./data:/data ghcr.io/minibear2021/togos:latest
```

或使用 docker-compose：

```bash
docker compose up -d
```

### 二进制文件

从 [Releases](https://github.com/minibear2021/togos/releases) 下载对应平台的二进制文件直接运行。

### 编译

```bash
# 当前平台
CGO_ENABLED=0 go build -o togos .

# Linux
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o togos-linux .

# macOS (Apple Silicon)
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -o togos-mac .

# Windows
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o togos.exe .
```

### 启动

```bash
# 全部使用默认值
./togos

# 自定义配置
./togos -listen :9090 -data-dir /var/togos -site-url https://share.example.com

# 使用环境变量
LISTEN_ADDR=:9090 SITE_URL=https://share.example.com ./togos
```

启动后在终端查看输出的管理员 Token，通过 API 管理文件和分享。

### 典型工作流

```bash
# 1. 上传文件
curl -H "Authorization: Bearer <token>" \
  -F "file=@document.pdf" \
  http://localhost:8080/api/files

# 2. 创建分享（密码 + 10次下载 + 24小时）
curl -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"file_id":1,"password":"abc","max_downloads":10,"expires_in":86400}' \
  http://localhost:8080/api/shares

# 3. 将返回的 share_url 发给对方即可
```

## 配置参数

| 环境变量 | 命令行参数 | 默认值 | 说明 |
|----------|-----------|--------|------|
| `LISTEN_ADDR` | `-listen` | `:8080` | 监听地址 |
| `DATA_DIR` | `-data-dir` | `./data` | 数据存储目录 |
| `MAX_FILE_SIZE` | `-max-file-size` | `100` | 最大文件大小 (MB) |
| `SITE_URL` | `-site-url` | `http://localhost:8080` | 站点 URL，用于生成分享链接 |
| `ADMIN_TOKEN` | `-admin-token` | 自动生成 | 管理员 Token |

配置文件查找优先级：环境变量 > 命令行参数 > 默认值

## API 端点

所有 `/api/*` 端点需要认证：请求头 `Authorization: Bearer <token>`

### 文件管理

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/api/files` | 上传文件 (multipart/form-data, 字段名 `file`) |
| `POST` | `/api/files/local` | 从服务器本地路径导入文件 `{"path":"..."}` |
| `GET` | `/api/files` | 列出所有文件 |
| `GET` | `/api/files/:id` | 获取文件详情 |
| `DELETE` | `/api/files/:id` | 删除文件及关联的所有分享 |

### 分享管理

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/api/shares` | 创建分享 `{"file_id":1,"password":"","max_downloads":0,"expires_in":0}` |
| `GET` | `/api/shares` | 列出所有分享 |
| `GET` | `/api/shares/:id` | 获取分享详情 |
| `DELETE` | `/api/shares/:id` | 删除分享 |

### 公开访问

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/s/:code` | 分享下载页面（密码保护则显示密码输入框） |
| `POST` | `/s/:code` | 提交提取密码 |
| `GET` | `/s/:code/download` | 下载文件（需先验证密码） |

完整 API 文档和示例见 [API.md](API.md)。

## 安全设计

- **密码哈希** — SHA-256 + 32 字节随机盐存储
- **速率限制** — 公开端点每 IP 30 次/分钟，超出返回 429
- **安全 HTTP 头** — X-Frame-Options, X-Content-Type-Options, Referrer-Policy 等
- **输入校验** — 文件名净化、分享代码格式验证
- **XSS 防护** — 所有用户输入在页面中做 HTML 转义
- **路径遍历防护** — 文件以数字 ID 存储，不保留原始文件名
- **上传限制** — 可配置文件大小上限，默认 100MB
- **下载保护** — Content-Disposition header 防止浏览器内联渲染

## 数据存储

```
data/
├── togos.db        # SQLite 数据库 (WAL 模式)
└── files/          # 上传的文件 (以 ID.扩展名 命名)
```

## 许可证

MIT

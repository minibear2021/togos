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

## CI/CD 集成

Togos 专为自动化流水线设计，所有管理操作通过 REST API 完成，无需图形界面。典型的集成流程只有三步：

> 构建产物 → 上传并创建分享链接 → 传递给下游

### 场景一：GitHub Actions 构建产物分发给 QA

```yaml
# .github/workflows/release.yml
- name: Upload to Togos and share
  run: |
    # 上传 APK
    RESP=$(curl -s -X POST "$TOGOS_URL/api/files" \
      -H "Authorization: Bearer $TOGOS_TOKEN" \
      -F "file=@app-release.apk")
    FILE_ID=$(echo "$RESP" | jq -r '.id')

    # 创建分享：7天有效，限制 50 次下载，带密码保护
    SHARE=$(curl -s -X POST "$TOGOS_URL/api/shares" \
      -H "Authorization: Bearer $TOGOS_TOKEN" \
      -H "Content-Type: application/json" \
      -d "{
        \"file_id\": $FILE_ID,
        \"password\": \"${{ secrets.SHARE_PASSWORD }}\",
        \"max_downloads\": 50,
        \"expires_in\": 604800
      }")
    SHARE_URL=$(echo "$SHARE" | jq -r '.share_url')

    # 写入构建摘要，QA 可直接点击下载
    echo "📦 [Android 测试包]($SHARE_URL) | 密码: \`${{ secrets.SHARE_PASSWORD }}\`" \
      >> $GITHUB_STEP_SUMMARY
```

### 场景二：多平台构建产物汇总

```yaml
# 矩阵构建后用一个 Job 汇总所有产物链接
summary:
  needs: build
  runs-on: ubuntu-latest
  steps:
    - name: Generate download summary
      run: |
        echo "## 🚀 构建完成 (${{ github.ref_name }})" >> $GITHUB_STEP_SUMMARY
        echo "| 平台 | 下载 |" >> $GITHUB_STEP_SUMMARY
        echo "|------|------|" >> $GITHUB_STEP_SUMMARY

        for PLATFORM in linux-amd64 darwin-arm64 windows-amd64; do
          RESP=$(curl -s -X POST "$TOGOS_URL/api/files" \
            -H "Authorization: Bearer $TOGOS_TOKEN" \
            -F "file=@togos-$PLATFORM.tar.gz")
          FILE_ID=$(echo "$RESP" | jq -r '.id')

          SHARE=$(curl -s -X POST "$TOGOS_URL/api/shares" \
            -H "Authorization: Bearer $TOGOS_TOKEN" \
            -H "Content-Type: application/json" \
            -d "{\"file_id\":$FILE_ID,\"expires_in\":259200}")
          URL=$(echo "$SHARE" | jq -r '.share_url')

          echo "| $PLATFORM | [$URL]($URL) |" >> $GITHUB_STEP_SUMMARY
        done
```

### 场景三：CI 上传，通过 PATCH 动态调整访问策略

构建产物在 CI 中上传后，可以根据审批流程动态调整分享策略：

```bash
# CI 中：上传并设为初始不可访问
SHARE=$(curl -s -X POST "$TOGOS_URL/api/shares" \
  -H "Authorization: Bearer $TOGOS_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"file_id\":123,\"max_downloads\":200,\"is_active\":false}")
CODE=$(echo "$SHARE" | jq -r '.code')
# 分享码通过通知（邮件/Slack）发送给审批人

# 审批通过后：启用分享并设置 24 小时有效期
curl -X PATCH "$TOGOS_URL/api/shares/$CODE" \
  -H "Authorization: Bearer $TOGOS_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"is_active":true,"expires_in":86400}'

# 审批拒绝后：删除分享
curl -X DELETE "$TOGOS_URL/api/shares/$CODE" \
  -H "Authorization: Bearer $TOGOS_TOKEN"
```

### 场景四：浏览器端自动打包分享

配合 GitHub Actions 的 `workflow_dispatch`，在浏览器中一键触发构建，产物自动上传并通过分享链接返回：

```yaml
on:
  workflow_dispatch:
    inputs:
      branch:
        description: '要构建的分支'
        required: true
        default: 'main'

jobs:
  build-and-share:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          ref: ${{ github.event.inputs.branch }}

      - name: Build and share
        run: |
          npm run build

          RESP=$(curl -s -X POST "$TOGOS_URL/api/files" \
            -H "Authorization: Bearer $TOGOS_TOKEN" \
            -F "file=@dist/bundle.zip")
          FILE_ID=$(echo "$RESP" | jq -r '.id')

          SHARE=$(curl -s -X POST "$TOGOS_URL/api/shares" \
            -H "Authorization: Bearer $TOGOS_TOKEN" \
            -H "Content-Type: application/json" \
            -d "{\"file_id\":$FILE_ID,\"expires_in\":3600,\"max_downloads\":10}")
          URL=$(echo "$SHARE" | jq -r '.share_url')

          echo "✅ 构建完成，1小时内有效: $URL" >> $GITHUB_STEP_SUMMARY
```

### 与 GitLab CI / Jenkins 集成

原理相同，只需在对应的 pipeline 步骤中调用 curl 操作 Togos API：

```yaml
# GitLab CI 示例
upload_to_togos:
  stage: deploy
  script:
    - |
      RESP=$(curl -s -X POST "$TOGOS_URL/api/files" \
        -H "Authorization: Bearer $TOGOS_TOKEN" \
        -F "file=@build/output.zip")
      FILE_ID=$(echo "$RESP" | jq -r '.id')
      SHARE=$(curl -s -X POST "$TOGOS_URL/api/shares" \
        -H "Authorization: Bearer $TOGOS_TOKEN" \
        -H "Content-Type: application/json" \
        -d "{\"file_id\":$FILE_ID,\"expires_in\":86400}")
      echo "Share URL: $(echo "$SHARE" | jq -r '.share_url')"
```

```groovy
// Jenkins Pipeline 示例
stage('Upload to Togos') {
    steps {
        script {
            def resp = sh(script: """
                curl -s -X POST "${TOGOS_URL}/api/files" \
                  -H "Authorization: Bearer ${TOGOS_TOKEN}" \
                  -F "file=@target/app.jar"
            """, returnStdout: true).trim()
            def fileId = readJSON(text: resp).id
            def share = sh(script: """
                curl -s -X POST "${TOGOS_URL}/api/shares" \
                  -H "Authorization: Bearer ${TOGOS_TOKEN}" \
                  -H "Content-Type: application/json" \
                  -d '{"file_id":${fileId},"expires_in":86400,"max_downloads":100}'
            """, returnStdout: true).trim()
            def shareUrl = readJSON(text: share).share_url
            echo "📦 构建产物: ${shareUrl}"
        }
    }
}
```

### 安全实践

- **Token 管理**：将 `TOGOS_TOKEN` 存入 CI 的 Secrets（GitHub Actions Secrets / GitLab CI Variables），禁止硬编码
- **时效控制**：CI 构建产物通常只临时分发，建议设置较短过期时间（1-24 小时）
- **下载限制**：限制下载次数可防止链接被恶意放大传播
- **密码保护**：敏感产物建议加密码，密码通过独立渠道（IM/邮件）通知
- **内网部署**：Togos 支持纯内网环境，无需外部服务依赖

## 配置参数

| 环境变量 | 命令行参数 | 默认值 | 说明 |
|----------|-----------|--------|------|
| `LISTEN_ADDR` | `-listen` | `:8080` | 监听地址 |
| `DATA_DIR` | `-data-dir` | `./data` | 数据存储目录 |
| `MAX_FILE_SIZE` | `-max-file-size` | `100` | 最大文件大小 (MB) |
| `SITE_URL` | `-site-url` | `http://localhost:8080` | 站点 URL，用于生成分享链接 |
| `ADMIN_TOKEN` | `-admin-token` | 自动生成 | 管理员 Token |
| `TEMPLATE_DIR` | `-template-dir` | （空） | 自定义模板目录，为空则使用内置模板 |

配置文件查找优先级：环境变量 > 命令行参数 > 默认值

### 自定义页面模板

指定 `-template-dir` 后，Togos 从该目录加载模板文件，修改样式无需重新编译：

| 文件 | 说明 | 占位符 |
|------|------|--------|
| `page.html` | 正常分享页面 | `%[1]s` 文件名, `%[2]s` 页面主体 |
| `error.html` | 错误页面（分享不存在/已过期/已禁用/次数用尽） | `%[1]s` 错误消息 |

两个文件均为可选，缺失的模板自动回退到内置默认模板。内置模板位于 `share.go` 的 `sharePageTemplate` / `sharePageErrorTemplate` 常量，可作为自定义模板的起点。

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

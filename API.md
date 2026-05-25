# Togos API 接口文档

## 通用说明

### 认证

所有 `/api/*` 端点需要认证，通过以下两种方式之一传递 Token：

- **HTTP Header**（推荐）: `Authorization: Bearer <token>`
- **URL 参数**: `?token=<token>`

Token 在启动时自动生成或通过 `ADMIN_TOKEN` / `-admin-token` 指定。

### 响应格式

所有 API 响应均为 JSON。

成功响应：
- `200 OK` — 请求成功
- `201 Created` — 资源创建成功

错误响应：
```json
{"error": "错误描述"}
```

常见 HTTP 状态码：`400` 参数错误、`401` 未认证、`403` 无权限、`404` 不存在、`413` 文件过大、`429` 频率限制。

### 数据类型

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | int64 | 自增主键 |
| `file_id` | int64 | 关联的文件 ID |
| `name` / `file_name` | string | 文件名 |
| `size` / `file_size` | int64 | 文件大小（字节） |
| `mime_type` | string | MIME 类型 |
| `code` | string | 分享码，8 位小写字母数字 |
| `has_password` | bool | 是否设置了提取密码 |
| `max_downloads` | int64 | 最大下载次数，0 为不限制 |
| `download_count` | int64 | 已下载次数 |
| `expires_at` | string | 过期时间（Unix 时间戳字符串），空为永不过期 |
| `expires_in` | int64 | 相对有效期（秒），请求参数 |
| `created_at` | string | 创建时间 |
| `is_active` | bool | 分享是否启用 |
| `share_url` | string | 分享链接 |

---

## 文件管理

### POST /api/files — 上传文件

使用 multipart/form-data 上传文件。

```
POST /api/files
Authorization: Bearer <token>
Content-Type: multipart/form-data

file=@/path/to/file.pdf
```

**参数**

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `file` | File | 是 | 要上传的文件 |

**响应** `201 Created`

```json
{
  "id": 1,
  "name": "document.pdf",
  "size": 102400,
  "mime_type": "application/pdf",
  "created_at": "2026-05-25T12:00:00Z"
}
```

**错误**

| 状态码 | 说明 |
|--------|------|
| 400 | 文件过大、未找到 file 字段、格式错误 |
| 401 | 缺少 Token |
| 403 | Token 无效 |

### POST /api/files/local — 从本地路径导入

从服务器本地文件路径导入文件。

```
POST /api/files/local
Authorization: Bearer <token>
Content-Type: application/json

{"path": "/home/user/photos/image.jpg"}
```

**请求体**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `path` | string | 是 | 服务器上的文件绝对路径 |

**响应** `201 Created` — 同文件上传

**错误**

| 状态码 | 说明 |
|--------|------|
| 400 | 缺少 path、路径无效、是目录、文件过大 |
| 404 | 文件不存在 |

### GET /api/files — 获取文件列表

```
GET /api/files
Authorization: Bearer <token>
```

**响应** `200 OK`

```json
[
  {
    "id": 1,
    "name": "document.pdf",
    "size": 102400,
    "mime_type": "application/pdf",
    "created_at": "2026-05-25 12:00:00"
  }
]
```

### GET /api/files/:id — 获取文件详情

```
GET /api/files/1
Authorization: Bearer <token>
```

**响应** `200 OK` — 单个文件对象

**错误** `404` — 文件不存在

### DELETE /api/files/:id — 删除文件

```
DELETE /api/files/1
Authorization: Bearer <token>
```

同时删除磁盘上的文件及关联的所有分享。

**响应** `200 OK`

```json
{"message": "文件已删除"}
```

---

## 分享管理

### POST /api/shares — 创建分享

```
POST /api/shares
Authorization: Bearer <token>
Content-Type: application/json

{
  "file_id": 1,
  "password": "secret123",
  "max_downloads": 10,
  "expires_in": 86400
}
```

**请求体**

| 字段 | 类型 | 必填 | 默认 | 说明 |
|------|------|------|------|------|
| `file_id` | int64 | 是 | — | 要分享的文件 ID |
| `password` | string | 否 | 空 | 提取密码，空则无密码 |
| `max_downloads` | int64 | 否 | 0 | 最大下载次数，0 不限 |
| `expires_in` | int64 | 否 | 0 | 有效期（秒），0 永久 |

**响应** `201 Created`

```json
{
  "id": 1,
  "file_id": 1,
  "file_name": "document.pdf",
  "file_size": 102400,
  "code": "a1b2c3d4",
  "has_password": true,
  "max_downloads": 10,
  "download_count": 0,
  "expires_at": "1716739200",
  "created_at": "2026-05-25 12:00:00",
  "is_active": true,
  "share_url": "https://share.example.com/s/a1b2c3d4"
}
```

### GET /api/shares — 获取分享列表

```
GET /api/shares
Authorization: Bearer <token>
```

**响应** `200 OK` — 分享对象数组

### GET /api/shares/:id — 获取分享详情

```
GET /api/shares/1
Authorization: Bearer <token>
```

**响应** `200 OK` — 单个分享对象

**错误** `404` — 分享不存在

### DELETE /api/shares/:id — 删除分享

```
DELETE /api/shares/1
Authorization: Bearer <token>
```

只删除分享记录，不影响文件。

**响应** `200 OK`

```json
{"message": "分享已删除"}
```

---

## 公开访问

公开端点无需 API Token，但受速率限制（30 次/分钟/IP）。

### GET /s/:code — 分享下载页面

用户访问分享链接时看到的 HTML 页面。如果分享设置了密码，页面将显示密码输入表单；否则直接显示下载按钮。

```
GET /s/a1b2c3d4
```

**响应** `200 OK` — HTML 页面

页面显示：文件名、大小、类型、有效期倒计时、剩余下载次数。

**特殊状态**

| 状态码 | 说明 |
|--------|------|
| 404 | 分享不存在或已删除 |
| 410 | 分享已过期或下载次数用尽 |

### POST /s/:code — 验证提取密码

提交密码表单，验证通过后设置 Cookie 并重定向回分享页面。

```
POST /s/a1b2c3d4
Content-Type: application/x-www-form-urlencoded

password=secret123
```

**响应** `302 Found` — 成功重定向到 `/s/:code`，失败重定向到 `/s/:code?error=密码错误`

### GET /s/:code/download — 下载文件

直接下载分享的文件。如果分享设置了密码，需要先通过密码验证（浏览器自动携带 Cookie）。

```
GET /s/a1b2c3d4/download
```

**响应头**

| 字段 | 说明 |
|------|------|
| `Content-Type` | 文件的 MIME 类型 |
| `Content-Disposition` | `attachment; filename="..."` |
| `Content-Length` | 文件字节数 |
| `Accept-Ranges` | `bytes`（支持断点续传） |

**特殊状态**

| 状态码 | 说明 |
|--------|------|
| 403 | 需要密码验证 |
| 410 | 分享已过期或下载次数用尽 |
| 206 | 部分内容（Range 请求） |

---

## Rate Limiting

公开端点 (`/s/*`) 限制为每 IP 每分钟 30 次请求。超出后返回：

```json
{"error": "请求过于频繁，请稍后再试"}
```

API 端点 (`/api/*`) 不受速率限制（认证后）。

---

## 内置文档

完整的 API 接口说明请参阅 [API.md](API.md) 文件。所有 API 端点均需 Bearer Token 认证。

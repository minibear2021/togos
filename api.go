package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type APIHandler struct {
	store *Store
	cfg   *Config
}

func NewAPIHandler(store *Store, cfg *Config) *APIHandler {
	return &APIHandler{store: store, cfg: cfg}
}

// ServeAPIDocs serves the API documentation page.
func (h *APIHandler) ServeAPIDocs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(apiDocsHTML))
}

// ——— File handlers ———

func (h *APIHandler) UploadFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 POST 方法"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, h.cfg.MaxFileSize+1<<20)

	if err := r.ParseMultipartForm(h.cfg.MaxFileSize); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "文件过大或请求格式错误"})
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "未找到文件字段 'file'"})
		return
	}
	defer file.Close()

	if header.Size > h.cfg.MaxFileSize {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": fmt.Sprintf("文件大小超过限制 (%dMB)", h.cfg.MaxFileSize/(1024*1024)),
		})
		return
	}

	// Sanitize filename
	name := filepath.Base(header.Filename)
	if name == "." || name == "/" || name == "" {
		name = "unnamed"
	}

	// Detect MIME type
	mimeType := header.Header.Get("Content-Type")
	if mimeType == "" {
		buf := make([]byte, 512)
		n, _ := file.Read(buf)
		mimeType = http.DetectContentType(buf[:n])
		file.Seek(0, io.SeekStart)
	}

	// Create file record in DB first to get ID
	dbFile, err := h.store.CreateFile(name, header.Size, "", mimeType)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "创建文件记录失败"})
		return
	}

	// Store file on disk with ID-based name
	ext := filepath.Ext(name)
	storedName := fmt.Sprintf("%d%s", dbFile.ID, ext)
	storedPath := filepath.Join(h.store.GetFilesDir(), storedName)

	dst, err := os.Create(storedPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "保存文件失败"})
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		os.Remove(storedPath)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "写入文件失败"})
		return
	}

	// Update file path in DB
	h.store.db.Exec("UPDATE files SET path = ? WHERE id = ?", storedPath, dbFile.ID)
	dbFile.path = storedPath

	writeJSON(w, http.StatusCreated, dbFile)
}

func (h *APIHandler) CreateFileFromLocal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 POST 方法"})
		return
	}

	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误，需要 JSON body"})
		return
	}

	if req.Path == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "缺少 'path' 字段"})
		return
	}

	// Security: resolve and validate path
	absPath, err := filepath.Abs(req.Path)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "无效的文件路径"})
		return
	}

	info, err := os.Stat(absPath)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "文件不存在或无法访问"})
		return
	}

	if info.IsDir() {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "不支持分享目录"})
		return
	}

	if info.Size() > h.cfg.MaxFileSize {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": fmt.Sprintf("文件大小超过限制 (%dMB)", h.cfg.MaxFileSize/(1024*1024)),
		})
		return
	}

	// Detect MIME type
	mimeType := detectMimeType(absPath)

	name := filepath.Base(absPath)
	dbFile, err := h.store.CreateFile(name, info.Size(), "", mimeType)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "创建文件记录失败"})
		return
	}

	// Copy file to data directory
	ext := filepath.Ext(name)
	storedName := fmt.Sprintf("%d%s", dbFile.ID, ext)
	storedPath := filepath.Join(h.store.GetFilesDir(), storedName)

	src, err := os.Open(absPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "无法读取源文件"})
		return
	}
	defer src.Close()

	dst, err := os.Create(storedPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "保存文件失败"})
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		os.Remove(storedPath)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "复制文件失败"})
		return
	}

	h.store.db.Exec("UPDATE files SET path = ? WHERE id = ?", storedPath, dbFile.ID)
	dbFile.path = storedPath

	writeJSON(w, http.StatusCreated, dbFile)
}

func (h *APIHandler) ListFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 GET 方法"})
		return
	}

	files, err := h.store.ListFiles()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "查询文件失败"})
		return
	}
	if files == nil {
		files = []*File{}
	}
	writeJSON(w, http.StatusOK, files)
}

func (h *APIHandler) GetFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 GET 方法"})
		return
	}

	id, err := parseID(r.URL.Path, "/api/files/")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "无效的文件 ID"})
		return
	}

	f, err := h.store.GetFile(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "查询文件失败"})
		return
	}
	if f == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "文件不存在"})
		return
	}

	writeJSON(w, http.StatusOK, f)
}

func (h *APIHandler) DeleteFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 DELETE 方法"})
		return
	}

	id, err := parseID(r.URL.Path, "/api/files/")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "无效的文件 ID"})
		return
	}

	if err := h.store.DeleteFile(id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "删除文件失败"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "文件已删除"})
}

// ——— Share handlers ———

func (h *APIHandler) CreateShare(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 POST 方法"})
		return
	}

	var req struct {
		FileID       int64  `json:"file_id"`
		Password     string `json:"password"`
		MaxDownloads int64  `json:"max_downloads"`
		ExpiresIn    int64  `json:"expires_in"` // seconds
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误，需要 JSON body"})
		return
	}

	if req.FileID <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "缺少有效的 'file_id' 字段"})
		return
	}

	f, err := h.store.GetFile(req.FileID)
	if err != nil || f == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "文件不存在"})
		return
	}

	if req.MaxDownloads < 0 {
		req.MaxDownloads = 0
	}

	var expiresAt string
	if req.ExpiresIn > 0 {
		expiresAt = fmt.Sprintf("%d", timeNow()+req.ExpiresIn)
	}

	share, err := h.store.CreateShare(req.FileID, req.Password, req.MaxDownloads, expiresAt, h.cfg.SiteURL)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "创建分享失败"})
		return
	}

	writeJSON(w, http.StatusCreated, share)
}

func (h *APIHandler) ListShares(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 GET 方法"})
		return
	}

	shares, err := h.store.ListShares(h.cfg.SiteURL)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "查询分享失败"})
		return
	}
	if shares == nil {
		shares = []*Share{}
	}
	writeJSON(w, http.StatusOK, shares)
}

func (h *APIHandler) GetShare(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 GET 方法"})
		return
	}

	id, err := parseID(r.URL.Path, "/api/shares/")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "无效的分享 ID"})
		return
	}

	share, err := h.store.GetShare(id, h.cfg.SiteURL)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "查询分享失败"})
		return
	}
	if share == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "分享不存在"})
		return
	}

	writeJSON(w, http.StatusOK, share)
}

func (h *APIHandler) DeleteShare(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 DELETE 方法"})
		return
	}

	id, err := parseID(r.URL.Path, "/api/shares/")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "无效的分享 ID"})
		return
	}

	if err := h.store.DeleteShare(id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "删除分享失败"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "分享已删除"})
}

// RouteAPI dispatches API requests to the appropriate handler.
func (h *APIHandler) RouteAPI(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	switch {
	case path == "/api/docs":
		h.ServeAPIDocs(w, r)
	case path == "/api/files" && r.Method == http.MethodGet:
		h.ListFiles(w, r)
	case path == "/api/files" && r.Method == http.MethodPost:
		h.UploadFile(w, r)
	case path == "/api/files/local" && r.Method == http.MethodPost:
		h.CreateFileFromLocal(w, r)
	case matchPrefix(path, "/api/files/") && r.Method == http.MethodGet:
		h.GetFile(w, r)
	case matchPrefix(path, "/api/files/") && r.Method == http.MethodDelete:
		h.DeleteFile(w, r)
	case path == "/api/shares" && r.Method == http.MethodGet:
		h.ListShares(w, r)
	case path == "/api/shares" && r.Method == http.MethodPost:
		h.CreateShare(w, r)
	case matchPrefix(path, "/api/shares/") && r.Method == http.MethodGet:
		h.GetShare(w, r)
	case matchPrefix(path, "/api/shares/") && r.Method == http.MethodDelete:
		h.DeleteShare(w, r)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "未找到该 API 端点"})
	}
}

// ——— Helpers ———

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func parseID(path, prefix string) (int64, error) {
	idStr := strings.TrimPrefix(path, prefix)
	idStr = strings.TrimSuffix(idStr, "/")
	return strconv.ParseInt(idStr, 10, 64)
}

func matchPrefix(path, prefix string) bool {
	return strings.HasPrefix(path, prefix) && len(path) > len(prefix)
}

func detectMimeType(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	mimeTypes := map[string]string{
		".html": "text/html", ".htm": "text/html",
		".css":  "text/css",
		".js":   "application/javascript",
		".json": "application/json",
		".xml":  "application/xml",
		".txt":  "text/plain", ".md": "text/markdown",
		".pdf":    "application/pdf",
		".zip":    "application/zip",
		".tar":    "application/x-tar",
		".gz":     "application/gzip",
		".7z":     "application/x-7z-compressed",
		".rar":    "application/x-rar-compressed",
		".jpg":    "image/jpeg", ".jpeg": "image/jpeg",
		".png":    "image/png",
		".gif":    "image/gif",
		".webp":   "image/webp",
		".svg":    "image/svg+xml",
		".ico":    "image/x-icon",
		".mp3":    "audio/mpeg",
		".wav":    "audio/wav",
		".ogg":    "audio/ogg",
		".flac":   "audio/flac",
		".mp4":    "video/mp4",
		".webm":   "video/webm",
		".avi":    "video/x-msvideo",
		".mov":    "video/quicktime",
		".doc":    "application/msword",
		".docx":   "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		".xls":    "application/vnd.ms-excel",
		".xlsx":   "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		".ppt":    "application/vnd.ms-powerpoint",
		".pptx":   "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	}
	if mime, ok := mimeTypes[ext]; ok {
		return mime
	}
	return "application/octet-stream"
}

func timeNow() int64 {
	return time.Now().Unix()
}

const apiDocsHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Togos API 文档</title>
<style>
	* { margin: 0; padding: 0; box-sizing: border-box; }
	body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; background: #f8f9fb; color: #333; line-height: 1.6; }
	.container { max-width: 860px; margin: 0 auto; padding: 40px 20px; }
	h1 { font-size: 28px; color: #1a1a1a; margin-bottom: 8px; }
	.subtitle { color: #888; font-size: 14px; margin-bottom: 32px; }
	h2 { font-size: 20px; color: #1a1a1a; margin: 32px 0 12px; padding-bottom: 8px; border-bottom: 2px solid #e5e5e5; }
	h3 { font-size: 16px; color: #333; margin: 20px 0 8px; }
	.endpoint { background: #fff; border-radius: 10px; padding: 20px 24px; margin: 16px 0; box-shadow: 0 1px 4px rgba(0,0,0,0.04); border: 1px solid #eee; }
	.method { display: inline-block; padding: 3px 10px; border-radius: 5px; font-size: 12px; font-weight: 600; color: #fff; margin-right: 8px; }
	.method.get { background: #22c55e; }
	.method.post { background: #3b82f6; }
	.method.delete { background: #ef4444; }
	.endpoint .path { font-family: "SF Mono", "Fira Code", monospace; font-size: 15px; font-weight: 500; }
	.endpoint .desc { color: #666; font-size: 13px; margin-top: 8px; }
	pre { background: #1e1e2e; color: #cdd6f4; border-radius: 8px; padding: 16px; overflow-x: auto; font-size: 13px; margin-top: 10px; }
	code { font-family: "SF Mono", "Fira Code", monospace; font-size: 13px; }
	.param-table { width: 100%; border-collapse: collapse; margin-top: 10px; font-size: 13px; }
	.param-table th { text-align: left; padding: 8px 12px; background: #f5f5f5; border-bottom: 2px solid #e5e5e5; }
	.param-table td { padding: 8px 12px; border-bottom: 1px solid #eee; }
	.param-table .name { font-family: monospace; color: #4f46e5; }
	.param-table .required { color: #ef4444; font-weight: 600; }
	.param-table .optional { color: #888; }
	.note { background: #fefce8; border: 1px solid #fde68a; border-radius: 8px; padding: 12px 16px; margin: 16px 0; font-size: 13px; color: #92400e; }
	.toc { background: #fff; border-radius: 10px; padding: 20px 24px; margin: 20px 0; box-shadow: 0 1px 4px rgba(0,0,0,0.04); border: 1px solid #eee; }
	.toc a { color: #4f46e5; text-decoration: none; }
	.toc a:hover { text-decoration: underline; }
	.toc ul { list-style: none; padding: 0; }
	.toc li { padding: 4px 0; }
</style>
</head>
<body>
<div class="container">
	<h1>Togos API 文档</h1>
	<p class="subtitle">文件分享工具 — 所有 API 端点均需 Bearer Token 认证</p>

	<div class="note">
		<strong>认证说明：</strong>所有 <code>/api/*</code> 端点需要在请求头中携带 <code>Authorization: Bearer &lt;token&gt;</code>，
		或通过 URL 查询参数 <code>?token=&lt;token&gt;</code> 传递。管理 Token 在服务启动时自动生成或通过环境变量/参数指定。
	</div>

	<div class="toc">
		<strong>目录</strong>
		<ul>
			<li><a href="#files">文件管理</a></li>
			<li><a href="#shares">分享管理</a></li>
			<li><a href="#public">公开访问</a></li>
		</ul>
	</div>

	<h2 id="files">文件管理</h2>

	<div class="endpoint">
		<span class="method post">POST</span>
		<span class="path">/api/files</span>
		<div class="desc">上传文件。使用 multipart/form-data 格式，文件字段名为 <code>file</code>。</div>
		<h3>请求参数</h3>
		<table class="param-table">
			<tr><th>参数</th><th>类型</th><th>必填</th><th>说明</th></tr>
			<tr><td class="name">file</td><td>File</td><td class="required">是</td><td>要上传的文件</td></tr>
		</table>
		<h3>示例</h3>
		<pre>curl -X POST http://localhost:8080/api/files \
  -H "Authorization: Bearer &lt;token&gt;" \
  -F "file=@/path/to/document.pdf"</pre>
		<h3>响应 (201)</h3>
		<pre>{
  "id": 1,
  "name": "document.pdf",
  "size": 102400,
  "mime_type": "application/pdf",
  "created_at": "2026-05-25T12:00:00Z"
}</pre>
	</div>

	<div class="endpoint">
		<span class="method post">POST</span>
		<span class="path">/api/files/local</span>
		<div class="desc">从本地文件路径创建文件记录。服务会将文件复制到数据目录。</div>
		<h3>请求参数</h3>
		<table class="param-table">
			<tr><th>参数</th><th>类型</th><th>必填</th><th>说明</th></tr>
			<tr><td class="name">path</td><td>String</td><td class="required">是</td><td>服务器上的本地文件路径</td></tr>
		</table>
		<h3>示例</h3>
		<pre>curl -X POST http://localhost:8080/api/files/local \
  -H "Authorization: Bearer &lt;token&gt;" \
  -H "Content-Type: application/json" \
  -d '{"path": "/home/user/photos/image.jpg"}'</pre>
	</div>

	<div class="endpoint">
		<span class="method get">GET</span>
		<span class="path">/api/files</span>
		<div class="desc">获取所有文件列表。</div>
		<h3>示例</h3>
		<pre>curl http://localhost:8080/api/files \
  -H "Authorization: Bearer &lt;token&gt;"</pre>
	</div>

	<div class="endpoint">
		<span class="method get">GET</span>
		<span class="path">/api/files/:id</span>
		<div class="desc">获取指定文件的详细信息。</div>
		<h3>示例</h3>
		<pre>curl http://localhost:8080/api/files/1 \
  -H "Authorization: Bearer &lt;token&gt;"</pre>
	</div>

	<div class="endpoint">
		<span class="method delete">DELETE</span>
		<span class="path">/api/files/:id</span>
		<div class="desc">删除指定文件及其所有关联的分享。</div>
		<h3>示例</h3>
		<pre>curl -X DELETE http://localhost:8080/api/files/1 \
  -H "Authorization: Bearer &lt;token&gt;"</pre>
	</div>

	<h2 id="shares">分享管理</h2>

	<div class="endpoint">
		<span class="method post">POST</span>
		<span class="path">/api/shares</span>
		<div class="desc">为文件创建新的分享链接。</div>
		<h3>请求参数</h3>
		<table class="param-table">
			<tr><th>参数</th><th>类型</th><th>必填</th><th>说明</th></tr>
			<tr><td class="name">file_id</td><td>Integer</td><td class="required">是</td><td>要分享的文件 ID</td></tr>
			<tr><td class="name">password</td><td>String</td><td class="optional">否</td><td>提取密码，不传则不设密码</td></tr>
			<tr><td class="name">max_downloads</td><td>Integer</td><td class="optional">否</td><td>最大下载次数，0 表示不限制</td></tr>
			<tr><td class="name">expires_in</td><td>Integer</td><td class="optional">否</td><td>有效期（秒），不传则永久有效</td></tr>
		</table>
		<h3>示例</h3>
		<pre>curl -X POST http://localhost:8080/api/shares \
  -H "Authorization: Bearer &lt;token&gt;" \
  -H "Content-Type: application/json" \
  -d '{"file_id": 1, "password": "secret123", "max_downloads": 10, "expires_in": 86400}'</pre>
		<h3>响应 (201)</h3>
		<pre>{
  "id": 1,
  "file_id": 1,
  "file_name": "document.pdf",
  "file_size": 102400,
  "code": "a1b2c3d4",
  "has_password": true,
  "max_downloads": 10,
  "download_count": 0,
  "expires_at": "1716739200",
  "created_at": "2026-05-25T12:00:00Z",
  "is_active": true,
  "share_url": "http://localhost:8080/s/a1b2c3d4"
}</pre>
	</div>

	<div class="endpoint">
		<span class="method get">GET</span>
		<span class="path">/api/shares</span>
		<div class="desc">获取所有分享列表。</div>
		<h3>示例</h3>
		<pre>curl http://localhost:8080/api/shares \
  -H "Authorization: Bearer &lt;token&gt;"</pre>
	</div>

	<div class="endpoint">
		<span class="method get">GET</span>
		<span class="path">/api/shares/:id</span>
		<div class="desc">获取指定分享的详细信息。</div>
	</div>

	<div class="endpoint">
		<span class="method delete">DELETE</span>
		<span class="path">/api/shares/:id</span>
		<div class="desc">删除指定分享。</div>
	</div>

	<h2 id="public">公开访问</h2>

	<div class="endpoint">
		<span class="method get">GET</span>
		<span class="path">/s/:code</span>
		<div class="desc">分享下载页面。如果分享设有密码，将显示密码输入表单。</div>
	</div>

	<div class="endpoint">
		<span class="method post">POST</span>
		<span class="path">/s/:code</span>
		<div class="desc">提交提取密码。密码正确后重定向回下载页面。</div>
	</div>

	<div class="endpoint">
		<span class="method get">GET</span>
		<span class="path">/s/:code/download</span>
		<div class="desc">下载分享的文件。如果设置了密码，需要先通过密码验证（Cookie 方式）。</div>
	</div>

	<h2>配置参数</h2>
	<table class="param-table">
		<tr><th>环境变量</th><th>命令行参数</th><th>默认值</th><th>说明</th></tr>
		<tr><td><code>LISTEN_ADDR</code></td><td><code>-listen</code></td><td>:8080</td><td>监听地址</td></tr>
		<tr><td><code>DATA_DIR</code></td><td><code>-data-dir</code></td><td>./data</td><td>数据存储目录</td></tr>
		<tr><td><code>MAX_FILE_SIZE</code></td><td><code>-max-file-size</code></td><td>100</td><td>最大文件大小 (MB)</td></tr>
		<tr><td><code>SITE_URL</code></td><td><code>-site-url</code></td><td>http://localhost:8080</td><td>站点 URL</td></tr>
		<tr><td><code>ADMIN_TOKEN</code></td><td><code>-admin-token</code></td><td>自动生成</td><td>管理 Token</td></tr>
	</table>

	<h2>安全特性</h2>
	<ul style="padding-left: 20px; font-size: 14px; color: #666;">
		<li>所有 API 端点需要 Bearer Token 认证</li>
		<li>公开分享端点有速率限制 (30次/分钟/IP)</li>
		<li>密码使用 SHA-256 + 随机盐哈希存储</li>
		<li>文件以数字 ID 命名存储，防止路径遍历</li>
		<li>HTTP 安全头 (X-Frame-Options, X-Content-Type-Options 等)</li>
		<li>文件下载使用 Content-Disposition 头防止浏览器内联渲染</li>
		<li>输入验证和 HTML 转义防止 XSS 攻击</li>
		<li>支持 Range 请求实现断点续传</li>
		<li>上传文件大小限制</li>
	</ul>
</div>
</body>
</html>`

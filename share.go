package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type ShareHandler struct {
	store *Store
	cfg   *Config
}

func NewShareHandler(store *Store, cfg *Config) *ShareHandler {
	return &ShareHandler{store: store, cfg: cfg}
}

// ServeSharePage serves the public share download page.
func (h *ShareHandler) ServeSharePage(w http.ResponseWriter, r *http.Request) {
	code := extractCode(r.URL.Path)
	if code == "" {
		http.NotFound(w, r)
		return
	}

	share, err := h.store.GetShareByCode(code, h.cfg.SiteURL)
	if err != nil || share == nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(sharePageError("分享不存在或已被删除")))
		return
	}

	if !share.IsActive {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusGone)
		w.Write([]byte(sharePageError("该分享已被禁用")))
		return
	}

	// Check expiration
	if share.ExpiresAt != "" {
		expiresUnix, _ := strconv.ParseInt(share.ExpiresAt, 10, 64)
		if expiresUnix > 0 && time.Now().Unix() > expiresUnix {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusGone)
			w.Write([]byte(sharePageError("该分享已过期")))
			return
		}
	}

	// Check download count
	if share.MaxDownloads > 0 && share.DownloadCount >= share.MaxDownloads {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusGone)
		w.Write([]byte(sharePageError("该分享的下载次数已达上限")))
		return
	}

	// Get the password cookie to check if already authenticated
	authCookie, _ := r.Cookie("share_auth_" + code)
	authenticated := authCookie != nil && h.store.VerifySharePassword(code, authCookie.Value)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(h.buildSharePage(share, authenticated)))
}

// ServeShareAction handles password verification and download initiation.
func (h *ShareHandler) ServeShareAction(w http.ResponseWriter, r *http.Request) {
	code := extractCode(r.URL.Path)
	if code == "" {
		http.NotFound(w, r)
		return
	}

	// Handle password verification
	if r.Method == http.MethodPost {
		password := r.FormValue("password")
		if h.store.VerifySharePassword(code, password) {
			http.SetCookie(w, &http.Cookie{
				Name:     "share_auth_" + code,
				Value:    password,
				Path:     "/s/" + code,
				HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
				MaxAge:   86400,
			})
			http.Redirect(w, r, "/s/"+code, http.StatusFound)
			return
		}
		http.Redirect(w, r, "/s/"+code+"?error=密码错误", http.StatusFound)
		return
	}

	http.NotFound(w, r)
}

// ServeDownload handles the file download.
func (h *ShareHandler) ServeDownload(w http.ResponseWriter, r *http.Request) {
	code := extractCode(r.URL.Path)
	if code == "" {
		http.NotFound(w, r)
		return
	}

	// Check password authentication
	authCookie, _ := r.Cookie("share_auth_" + code)

	share, err := h.store.GetShareByCode(code, h.cfg.SiteURL)
	if err != nil || share == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "分享不存在"})
		return
	}

	if share.HasPassword {
		if authCookie == nil || !h.store.VerifySharePassword(code, authCookie.Value) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "需要密码验证"})
			return
		}
	}

	// Validate share — redirect to share page for HTML error display
	if !share.IsActive {
		http.Redirect(w, r, "/s/"+code, http.StatusFound)
		return
	}

	if share.ExpiresAt != "" {
		expiresUnix, _ := strconv.ParseInt(share.ExpiresAt, 10, 64)
		if expiresUnix > 0 && time.Now().Unix() > expiresUnix {
			http.Redirect(w, r, "/s/"+code, http.StatusFound)
			return
		}
	}

	if share.MaxDownloads > 0 && share.DownloadCount >= share.MaxDownloads {
		http.Redirect(w, r, "/s/"+code, http.StatusFound)
		return
	}

	// Open file
	file, err := os.Open(share.filePath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "文件读取失败"})
		return
	}
	defer file.Close()

	// Increment download count
	h.store.IncrementDownloadCount(share.Code)

	// Set response headers
	w.Header().Set("Content-Type", share.mimeType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, sanitizeFilename(share.FileName)))
	w.Header().Set("Accept-Ranges", "bytes")

	// Handle range requests
	if rangeHeader := r.Header.Get("Range"); rangeHeader != "" {
		serveRangeRequest(w, r, file, share.FileSize)
		return
	}

	w.Header().Set("Content-Length", fmt.Sprintf("%d", share.FileSize))
	http.ServeContent(w, r, share.FileName, time.Now(), file)
}

// buildSharePage generates the HTML for the share page.
func (h *ShareHandler) buildSharePage(share *Share, authenticated bool) string {
	var bodyHTML string

	// File info section
	bodyHTML += fmt.Sprintf(`
		<div class="file-info">
			<div class="file-icon">&#x1F4C4;</div>
			<h2>%s</h2>
			<p class="file-meta">
				<span>大小: %s</span>
				<span>类型: %s</span>
			</p>
		</div>`, escHTML(share.FileName), formatSize(share.FileSize), escHTML(share.mimeType))

	// Expiration info
	if share.ExpiresAt != "" {
		expiresUnix, _ := strconv.ParseInt(share.ExpiresAt, 10, 64)
		if expiresUnix > 0 {
			expTime := time.Unix(expiresUnix, 0)
			remaining := time.Until(expTime)
			expireText := ""
			if remaining > 0 {
				expireText = fmt.Sprintf("剩余 %s", formatDuration(remaining))
			} else {
				expireText = "已过期"
			}
			bodyHTML += fmt.Sprintf(`<p class="expire-info">有效期至: %s (%s)</p>`,
				expTime.Format("2006-01-02 15:04:05"), expireText)
		}
	}

	// Download count info
	if share.MaxDownloads > 0 {
		remaining := share.MaxDownloads - share.DownloadCount
		bodyHTML += fmt.Sprintf(`<p class="download-info">剩余下载次数: %d / %d</p>`,
			remaining, share.MaxDownloads)
	}

	// Action section
	if share.HasPassword && !authenticated {
		bodyHTML += `
		<div class="password-form">
			<form method="POST" action="` + escHTML("/s/" + share.Code) + `">
				<input type="password" name="password" placeholder="请输入提取密码" required autofocus>
				<button type="submit">验证</button>
			</form>
		</div>`
	} else {
		bodyHTML += fmt.Sprintf(`
		<div class="download-section">
			<a href="%s/download" class="download-btn">下载文件</a>
		</div>`, escHTML("/s/"+share.Code))
	}

	return fmt.Sprintf(sharePageTemplate, escHTML(share.FileName), bodyHTML)
}

func extractCode(path string) string {
	path = strings.TrimPrefix(path, "/s/")
	path = strings.TrimSuffix(path, "/")

	// Handle /s/{code}/download and /s/{code}/action
	if idx := strings.Index(path, "/"); idx != -1 {
		path = path[:idx]
	}

	// Validate code format: only lowercase alphanumeric
	for _, c := range path {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')) {
			return ""
		}
	}
	return path
}

func sanitizeFilename(name string) string {
	// Remove path separators and control characters
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, "\\", "_")
	name = strings.ReplaceAll(name, ":", "_")

	// Escape quotes for Content-Disposition header
	name = strings.ReplaceAll(name, `"`, `\"`)

	// Trim spaces
	name = strings.TrimSpace(name)
	if name == "" {
		name = "download"
	}
	return name
}

func formatSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(size)/float64(div), "KMGTPE"[exp])
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%d秒", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%d分%d秒", int(d.Minutes()), int(d.Seconds())%60)
	}
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	if d < 24*time.Hour {
		return fmt.Sprintf("%d时%d分", hours, minutes)
	}
	days := hours / 24
	hours = hours % 24
	return fmt.Sprintf("%d天%d时", days, hours)
}

func escHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&#34;")
	s = strings.ReplaceAll(s, "'", "&#39;")
	return s
}

func serveRangeRequest(w http.ResponseWriter, r *http.Request, file *os.File, fileSize int64) {
	rangeHeader := r.Header.Get("Range")
	if !strings.HasPrefix(rangeHeader, "bytes=") {
		return
	}

	ranges := strings.TrimPrefix(rangeHeader, "bytes=")
	parts := strings.SplitN(ranges, "-", 2)
	if len(parts) != 2 {
		return
	}

	start, _ := strconv.ParseInt(parts[0], 10, 64)
	var end int64

	if parts[1] == "" {
		end = fileSize - 1
	} else {
		end, _ = strconv.ParseInt(parts[1], 10, 64)
	}

	if start < 0 || end < start || end >= fileSize {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", fileSize))
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	}

	contentLength := end - start + 1
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, fileSize))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", contentLength))
	w.WriteHeader(http.StatusPartialContent)

	file.Seek(start, io.SeekStart)
	io.CopyN(w, file, contentLength)
}

func sharePageError(message string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>文件分享 - 错误</title>
<style>
	* { margin: 0; padding: 0; box-sizing: border-box; }
	body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; background: #f5f5f5; display: flex; flex-direction: column; justify-content: center; align-items: center; min-height: 100vh; }
	.error-card { background: #fff; border-radius: 12px; padding: 48px; box-shadow: 0 2px 16px rgba(0,0,0,0.08); text-align: center; max-width: 420px; width: 90%%; }
	.error-icon { font-size: 48px; margin-bottom: 16px; }
	.error-card h2 { color: #333; margin-bottom: 12px; font-size: 20px; }
	.error-card p { color: #666; font-size: 14px; line-height: 1.6; }
	.footer { margin-top: 24px; font-size: 12px; color: #bbb; text-align: center; }
	.footer a { color: #999; text-decoration: none; }
	.footer a:hover { color: #4f46e5; }
</style>
</head>
<body>
<div class="error-card">
	<div class="error-icon">&#x26A0;</div>
	<h2>无法访问</h2>
	<p>%s</p>
</div>
<div class="footer"><a href="https://github.com/minibear2021/togos" target="_blank">由 Togos 驱动</a></div>
</body>
</html>`, escHTML(message))
}

// sharePageTemplate is the HTML template for the share download page.
const sharePageTemplate = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>%s - 文件分享</title>
<style>
	* { margin: 0; padding: 0; box-sizing: border-box; }
	body {
		font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", sans-serif;
		background: #f7f8fa;
		color: #333;
		display: flex;
		flex-direction: column;
		justify-content: center;
		align-items: center;
		min-height: 100vh;
		padding: 16px;
	}
	.card {
		background: #fff;
		border-radius: 16px;
		padding: 40px 32px;
		box-shadow: 0 2px 24px rgba(0,0,0,0.06);
		max-width: 440px;
		width: 100%%;
		text-align: center;
	}
	.file-icon {
		font-size: 56px;
		margin-bottom: 16px;
		line-height: 1;
	}
	.card h2 {
		font-size: 18px;
		font-weight: 600;
		color: #1a1a1a;
		margin-bottom: 8px;
		word-break: break-all;
	}
	.file-meta {
		display: flex;
		justify-content: center;
		gap: 16px;
		flex-wrap: wrap;
		margin-top: 8px;
	}
	.file-meta span {
		font-size: 13px;
		color: #888;
		background: #f5f5f5;
		padding: 4px 10px;
		border-radius: 6px;
	}
	.expire-info, .download-info {
		font-size: 13px;
		color: #999;
		margin-top: 12px;
	}
	.password-form {
		margin-top: 24px;
	}
	.password-form input[type="password"] {
		width: 100%%;
		padding: 12px 16px;
		border: 2px solid #e5e5e5;
		border-radius: 10px;
		font-size: 15px;
		outline: none;
		transition: border-color 0.2s;
		text-align: center;
	}
	.password-form input[type="password"]:focus {
		border-color: #4f46e5;
	}
	.password-form button {
		width: 100%%;
		margin-top: 12px;
		padding: 12px;
		background: #4f46e5;
		color: #fff;
		border: none;
		border-radius: 10px;
		font-size: 15px;
		font-weight: 500;
		cursor: pointer;
		transition: background 0.2s;
	}
	.password-form button:hover {
		background: #4338ca;
	}
	.download-section {
		margin-top: 24px;
	}
	.download-btn {
		display: inline-block;
		padding: 14px 40px;
		background: #4f46e5;
		color: #fff;
		border-radius: 10px;
		font-size: 16px;
		font-weight: 500;
		text-decoration: none;
		transition: background 0.2s, transform 0.1s;
	}
	.download-btn:hover {
		background: #4338ca;
	}
	.download-btn:active {
		transform: scale(0.98);
	}
	.error-msg {
		color: #e53e3e;
		font-size: 14px;
		margin-top: 12px;
	}
	.footer {
		margin-top: 24px;
		font-size: 12px;
		color: #bbb;
	}
	.footer a {
		color: #999;
		text-decoration: none;
	}
	.footer a:hover {
		color: #4f46e5;
	}
</style>
</head>
<body>
<div class="card">
	%s
</div>
	<div class="footer"><a href="https://github.com/minibear2021/togos" target="_blank">由 Togos 驱动</a></div>
</body>
</html>`

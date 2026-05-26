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

	shares, err := h.store.GetSharesByFileID(id, h.cfg.SiteURL)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "查询分享信息失败"})
		return
	}
	if shares == nil {
		shares = []*Share{}
	}

	writeJSON(w, http.StatusOK, struct {
		*File
		Shares []*Share `json:"shares"`
	}{f, shares})
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

	id, code, err := resolveShareParam(r.URL.Path, "/api/shares/")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "无效的分享标识，需要数字 ID 或 8 位分享码"})
		return
	}

	var share *Share
	if code != "" {
		share, err = h.store.GetShareByCode(code, h.cfg.SiteURL)
	} else {
		share, err = h.store.GetShare(id, h.cfg.SiteURL)
	}
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

	id, code, err := resolveShareParam(r.URL.Path, "/api/shares/")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "无效的分享标识，需要数字 ID 或 8 位分享码"})
		return
	}

	if code != "" {
		err = h.store.DeleteShareByCode(code)
	} else {
		err = h.store.DeleteShare(id)
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "删除分享失败"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "分享已删除"})
}

func (h *APIHandler) UpdateShare(w http.ResponseWriter, r *http.Request) {
	id, code, err := resolveShareParam(r.URL.Path, "/api/shares/")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "无效的分享标识，需要数字 ID 或 8 位分享码"})
		return
	}

	var req struct {
		Password      *string `json:"password"`
		MaxDownloads  *int64  `json:"max_downloads"`
		DownloadCount *int64  `json:"download_count"`
		ExpiresIn     *int64  `json:"expires_in"`
		IsActive      *bool   `json:"is_active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误，需要 JSON body"})
		return
	}

	if req.Password == nil && req.MaxDownloads == nil && req.DownloadCount == nil && req.ExpiresIn == nil && req.IsActive == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "至少需要提供一个更新字段"})
		return
	}

	var share *Share
	if code != "" {
		share, err = h.store.UpdateShareByCode(code, req.Password, req.MaxDownloads, req.DownloadCount, req.ExpiresIn, req.IsActive, h.cfg.SiteURL)
	} else {
		share, err = h.store.UpdateShare(id, req.Password, req.MaxDownloads, req.DownloadCount, req.ExpiresIn, req.IsActive, h.cfg.SiteURL)
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "更新分享失败"})
		return
	}
	if share == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "分享不存在"})
		return
	}

	writeJSON(w, http.StatusOK, share)
}

// RouteAPI dispatches API requests to the appropriate handler.
func (h *APIHandler) RouteAPI(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	switch {
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
	case matchPrefix(path, "/api/shares/") && r.Method == http.MethodPatch:
		h.UpdateShare(w, r)
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

func validateCode(code string) bool {
	if len(code) != 8 {
		return false
	}
	for _, c := range code {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')) {
			return false
		}
	}
	return true
}

// resolveShareParam extracts a share identifier from the URL path.
// Returns (id, "", nil) for numeric IDs, or (0, code, nil) for share codes.
func resolveShareParam(path, prefix string) (int64, string, error) {
	param := strings.TrimPrefix(path, prefix)
	param = strings.TrimSuffix(param, "/")
	if id, err := strconv.ParseInt(param, 10, 64); err == nil {
		return id, "", nil
	}
	if validateCode(param) {
		return 0, param, nil
	}
	return 0, "", fmt.Errorf("无效的分享标识")
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

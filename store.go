package main

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db       *sql.DB
	filesDir string
}

type File struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	MimeType  string `json:"mime_type"`
	CreatedAt string `json:"created_at"`
	path       string
}

type Share struct {
	ID            int64  `json:"id"`
	FileID        int64  `json:"file_id"`
	FileName      string `json:"file_name"`
	FileSize      int64  `json:"file_size"`
	Code          string `json:"code"`
	HasPassword   bool   `json:"has_password"`
	MaxDownloads  int64  `json:"max_downloads"`
	DownloadCount int64  `json:"download_count"`
	ExpiresAt     string `json:"expires_at"`
	CreatedAt     string `json:"created_at"`
	IsActive      bool   `json:"is_active"`
	ShareURL      string `json:"share_url"`

	passwordHash string
	filePath     string
	mimeType     string
}

func NewStore(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("创建数据目录失败: %w", err)
	}

	filesDir := filepath.Join(dataDir, "files")
	if err := os.MkdirAll(filesDir, 0755); err != nil {
		return nil, fmt.Errorf("创建文件目录失败: %w", err)
	}

	dbPath := filepath.Join(dataDir, "togos.db")
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	s := &Store{db: db, filesDir: filesDir}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("数据库迁移失败: %w", err)
	}

	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS files (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			size INTEGER NOT NULL,
			path TEXT NOT NULL,
			mime_type TEXT NOT NULL DEFAULT 'application/octet-stream',
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
		CREATE TABLE IF NOT EXISTS shares (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			file_id INTEGER NOT NULL,
			code TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL DEFAULT '',
			max_downloads INTEGER NOT NULL DEFAULT 0,
			download_count INTEGER NOT NULL DEFAULT 0,
			expires_at TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			is_active INTEGER NOT NULL DEFAULT 1,
			FOREIGN KEY (file_id) REFERENCES files(id) ON DELETE CASCADE
		);
		CREATE INDEX IF NOT EXISTS idx_shares_code ON shares(code);
		CREATE INDEX IF NOT EXISTS idx_shares_file_id ON shares(file_id);
		PRAGMA journal_mode=WAL;
		PRAGMA foreign_keys=ON;
	`)
	return err
}

// File operations

func (s *Store) CreateFile(name string, size int64, filePath string, mimeType string) (*File, error) {
	r, err := s.db.Exec(
		"INSERT INTO files (name, size, path, mime_type) VALUES (?, ?, ?, ?)",
		name, size, filePath, mimeType,
	)
	if err != nil {
		return nil, err
	}
	id, _ := r.LastInsertId()
	return &File{
		ID:        id,
		Name:      name,
		Size:      size,
		MimeType:  mimeType,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		path:      filePath,
	}, nil
}

func (s *Store) GetFile(id int64) (*File, error) {
	f := &File{}
	err := s.db.QueryRow(
		"SELECT id, name, size, path, mime_type, created_at FROM files WHERE id = ?", id,
	).Scan(&f.ID, &f.Name, &f.Size, &f.path, &f.MimeType, &f.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return f, err
}

func (s *Store) ListFiles() ([]*File, error) {
	rows, err := s.db.Query("SELECT id, name, size, path, mime_type, created_at FROM files ORDER BY id DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []*File
	for rows.Next() {
		f := &File{}
		if err := rows.Scan(&f.ID, &f.Name, &f.Size, &f.path, &f.MimeType, &f.CreatedAt); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

func (s *Store) DeleteFile(id int64) error {
	f, err := s.GetFile(id)
	if err != nil {
		return err
	}
	if f == nil {
		return nil
	}
	// Delete file from disk
	os.Remove(f.path)
	// Delete from database (cascades to shares)
	_, err = s.db.Exec("DELETE FROM files WHERE id = ?", id)
	return err
}

// Share operations

func (s *Store) CreateShare(fileID int64, password string, maxDownloads int64, expiresAt string, siteURL string) (*Share, error) {
	code, err := generateCode(8)
	if err != nil {
		return nil, err
	}

	passwordHash := ""
	if password != "" {
		passwordHash = hashPassword(password)
	}

	_, err = s.db.Exec(
		`INSERT INTO shares (file_id, code, password_hash, max_downloads, expires_at)
		 VALUES (?, ?, ?, ?, ?)`,
		fileID, code, passwordHash, maxDownloads, expiresAt,
	)
	if err != nil {
		// Retry with a new code on collision
		if strings.Contains(err.Error(), "UNIQUE") {
			return s.CreateShare(fileID, password, maxDownloads, expiresAt, siteURL)
		}
		return nil, err
	}

	// Read back the created share
	return s.GetShareByCode(code, siteURL)
}

func (s *Store) GetShare(id int64, siteURL string) (*Share, error) {
	sh := &Share{}
	var passwordHash, expiresAt string
	err := s.db.QueryRow(
		`SELECT s.id, s.file_id, f.name, f.size, s.code, s.password_hash,
		        s.max_downloads, s.download_count, s.expires_at, s.created_at, s.is_active,
		        f.path, f.mime_type
		 FROM shares s JOIN files f ON s.file_id = f.id
		 WHERE s.id = ?`, id,
	).Scan(&sh.ID, &sh.FileID, &sh.FileName, &sh.FileSize, &sh.Code,
		&passwordHash, &sh.MaxDownloads, &sh.DownloadCount, &expiresAt,
		&sh.CreatedAt, &sh.IsActive, &sh.filePath, &sh.mimeType)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	sh.HasPassword = passwordHash != ""
	sh.passwordHash = passwordHash
	if expiresAt != "" {
		sh.ExpiresAt = expiresAt
	}
	sh.ShareURL = siteURL + "/s/" + sh.Code
	return sh, nil
}

func (s *Store) GetShareByCode(code string, siteURL string) (*Share, error) {
	sh := &Share{}
	var passwordHash, expiresAt string
	err := s.db.QueryRow(
		`SELECT s.id, s.file_id, f.name, f.size, s.code, s.password_hash,
		        s.max_downloads, s.download_count, s.expires_at, s.created_at, s.is_active,
		        f.path, f.mime_type
		 FROM shares s JOIN files f ON s.file_id = f.id
		 WHERE s.code = ?`, code,
	).Scan(&sh.ID, &sh.FileID, &sh.FileName, &sh.FileSize, &sh.Code,
		&passwordHash, &sh.MaxDownloads, &sh.DownloadCount, &expiresAt,
		&sh.CreatedAt, &sh.IsActive, &sh.filePath, &sh.mimeType)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	sh.HasPassword = passwordHash != ""
	sh.passwordHash = passwordHash
	if expiresAt != "" {
		sh.ExpiresAt = expiresAt
	}
	if siteURL != "" {
		sh.ShareURL = siteURL + "/s/" + sh.Code
	}
	return sh, nil
}

func (s *Store) ListShares(siteURL string) ([]*Share, error) {
	rows, err := s.db.Query(
		`SELECT s.id, s.file_id, f.name, f.size, s.code, s.password_hash,
		        s.max_downloads, s.download_count, s.expires_at, s.created_at, s.is_active,
		        f.path, f.mime_type
		 FROM shares s JOIN files f ON s.file_id = f.id
		 ORDER BY s.id DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var shares []*Share
	for rows.Next() {
		sh := &Share{}
		var passwordHash, expiresAt string
		if err := rows.Scan(&sh.ID, &sh.FileID, &sh.FileName, &sh.FileSize, &sh.Code,
			&passwordHash, &sh.MaxDownloads, &sh.DownloadCount, &expiresAt,
			&sh.CreatedAt, &sh.IsActive, &sh.filePath, &sh.mimeType); err != nil {
			return nil, err
		}
		sh.HasPassword = passwordHash != ""
		sh.passwordHash = passwordHash
		if expiresAt != "" {
			sh.ExpiresAt = expiresAt
		}
		sh.ShareURL = siteURL + "/s/" + sh.Code
		shares = append(shares, sh)
	}
	return shares, rows.Err()
}

func (s *Store) DeleteShare(id int64) error {
	_, err := s.db.Exec("DELETE FROM shares WHERE id = ?", id)
	return err
}

func (s *Store) IncrementDownloadCount(code string) error {
	_, err := s.db.Exec(
		"UPDATE shares SET download_count = download_count + 1 WHERE code = ?",
		code,
	)
	return err
}

func (s *Store) VerifySharePassword(code, password string) bool {
	sh, err := s.GetShareByCode(code, "")
	if err != nil || sh == nil {
		return false
	}
	if sh.passwordHash == "" {
		return true
	}
	return verifyPassword(password, sh.passwordHash)
}

func (s *Store) GetFilesDir() string {
	return s.filesDir
}

// Password hashing using SHA-256 with salt

func hashPassword(password string) string {
	salt := make([]byte, 32)
	rand.Read(salt)
	h := sha256.Sum256(append(salt, []byte(password)...))
	return hex.EncodeToString(salt) + ":" + hex.EncodeToString(h[:])
}

func verifyPassword(password, hash string) bool {
	parts := strings.SplitN(hash, ":", 2)
	if len(parts) != 2 {
		return false
	}
	salt, err := hex.DecodeString(parts[0])
	if err != nil {
		return false
	}
	expectedHash, err := hex.DecodeString(parts[1])
	if err != nil {
		return false
	}
	h := sha256.Sum256(append(salt, []byte(password)...))
	return hex.EncodeToString(h[:]) == hex.EncodeToString(expectedHash)
}

// Utility

func generateCode(length int) (string, error) {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	result := make([]byte, length)
	for i := range result {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		if err != nil {
			return "", err
		}
		result[i] = chars[n.Int64()]
	}
	return string(result), nil
}

func generateToken(length int) string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	result := make([]byte, length)
	for i := range result {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		result[i] = chars[n.Int64()]
	}
	return string(result)
}

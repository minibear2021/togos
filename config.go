package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	ListenAddr  string
	DataDir     string
	MaxFileSize int64
	SiteURL     string
	AdminToken  string
	TemplateDir string
}

func ParseConfig() *Config {
	cfg := &Config{}

	flag.StringVar(&cfg.ListenAddr, "listen", getEnv("LISTEN_ADDR", ":8080"), "监听地址 (默认 :8080)")
	flag.StringVar(&cfg.DataDir, "data-dir", getEnv("DATA_DIR", "./data"), "数据存储目录")
	flag.Int64Var(&cfg.MaxFileSize, "max-file-size", getEnvInt64("MAX_FILE_SIZE", 100), "最大文件大小 (MB)")
	flag.StringVar(&cfg.SiteURL, "site-url", getEnv("SITE_URL", "http://localhost:8080"), "站点 URL，用于生成分享链接")
	flag.StringVar(&cfg.AdminToken, "admin-token", getEnv("ADMIN_TOKEN", ""), "管理员 Token (为空则自动生成)")
	flag.StringVar(&cfg.TemplateDir, "template-dir", getEnv("TEMPLATE_DIR", ""), "自定义模板目录 (为空则使用内置模板)")

	flag.Parse()

	if cfg.AdminToken == "" {
		cfg.AdminToken = generateToken(32)
		fmt.Printf("已自动生成管理员 Token: %s\n", cfg.AdminToken)
	}

	cfg.MaxFileSize = cfg.MaxFileSize * 1024 * 1024

	return cfg
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func getEnvInt64(key string, defaultVal int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return defaultVal
}

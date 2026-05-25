package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func main() {
	cfg := ParseConfig()

	store, err := NewStore(cfg.DataDir)
	if err != nil {
		log.Fatalf("初始化存储失败: %v", err)
	}
	defer store.Close()

	api := NewAPIHandler(store, cfg)
	share := NewShareHandler(store, cfg)

	// Rate limiter: 30 requests per minute for public endpoints
	limiter := NewRateLimiter(30, time.Minute)

	mux := http.NewServeMux()

	// Public share routes (rate-limited, no auth)
	mux.HandleFunc("/s/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Determine which handler to use
		if strings.HasSuffix(path, "/download") || strings.Contains(path, "/download") {
			share.ServeDownload(w, r)
		} else if r.Method == http.MethodPost {
			share.ServeShareAction(w, r)
		} else {
			share.ServeSharePage(w, r)
		}
	})

	// API routes (auth-protected)
	apiMux := http.NewServeMux()
	apiMux.HandleFunc("/api/", api.RouteAPI)
	apiMux.HandleFunc("/api/docs", api.ServeAPIDocs)

	// Wrap API routes with auth middleware
	apiHandler := AuthMiddleware(cfg.AdminToken)(apiMux)
	mux.Handle("/api/", apiHandler)

	// Root redirect to API docs
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/api/docs", http.StatusFound)
			return
		}
		http.NotFound(w, r)
	})

	// Apply global middleware
	var handler http.Handler = mux
	handler = SecurityHeaders(handler)
	handler = RecoveryMiddleware(handler)
	handler = LoggingMiddleware(handler)
	handler = RateLimitMiddleware(limiter)(handler)

	server := &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("正在关闭服务...")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		server.Shutdown(ctx)
	}()

	log.Printf("Togos 文件分享服务启动于 %s", cfg.ListenAddr)
	log.Printf("管理员 Token: %s", cfg.AdminToken)
	log.Printf("数据目录: %s", cfg.DataDir)
	log.Printf("API 文档: http://%s/api/docs", localAddr(cfg.ListenAddr))

	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("服务启动失败: %v", err)
	}

	log.Println("服务已关闭")
}

func localAddr(addr string) string {
	if strings.HasPrefix(addr, ":") {
		return "localhost" + addr
	}
	return addr
}

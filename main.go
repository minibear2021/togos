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

	// Rate limiter: 30 requests per minute for public endpoints only
	limiter := NewRateLimiter(30, time.Minute)

	mux := http.NewServeMux()

	// Public share routes — rate-limited
	shareHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.HasSuffix(path, "/download") || strings.Contains(path, "/download") {
			share.ServeDownload(w, r)
		} else if r.Method == http.MethodPost {
			share.ServeShareAction(w, r)
		} else {
			share.ServeSharePage(w, r)
		}
	})
	mux.Handle("/s/", RateLimitMiddleware(limiter)(shareHandler))

	// API routes — auth-protected, no rate limit
	apiMux := http.NewServeMux()
	apiMux.HandleFunc("/api/", api.RouteAPI)
	mux.Handle("/api/", AuthMiddleware(cfg.AdminToken)(apiMux))

	// Root
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	// Global middleware (not rate limiting — that's per-route above)
	var handler http.Handler = mux
	handler = SecurityHeaders(handler)
	handler = RecoveryMiddleware(handler)
	handler = LoggingMiddleware(handler)

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

	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("服务启动失败: %v", err)
	}

	log.Println("服务已关闭")
}

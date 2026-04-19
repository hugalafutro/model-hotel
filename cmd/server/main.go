package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/user/llm-proxy/internal/admin"
	"github.com/user/llm-proxy/internal/api"
	"github.com/user/llm-proxy/internal/config"
	"github.com/user/llm-proxy/internal/db"
	"github.com/user/llm-proxy/internal/model"
	"github.com/user/llm-proxy/internal/provider"
	"github.com/user/llm-proxy/internal/proxy"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	log.Printf("Starting LLM-Proxy with configuration:\n%s", cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	database, err := db.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer database.Close()

	if err := database.WaitForReady(ctx, 30); err != nil {
		log.Fatalf("Database not ready: %v", err)
	}

	adminMgr, err := admin.New(cfg.DataDir)
	if err != nil {
		log.Fatalf("Failed to initialize admin manager: %v", err)
	}

	log.Printf("Admin token: %s", adminMgr.Token())

	providerRepo := provider.NewRepository(database.Pool())
	modelRepo := model.NewRepository(database.Pool())

	r := chi.NewRouter()

	// Security middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))
	r.Use(middleware.Compress(5))

	// Security headers
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("X-XSS-Protection", "1; mode=block")
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline';")
			next.ServeHTTP(w, r)
		})
	})

	// CORS for frontend
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			allowedOrigins := []string{
				"http://localhost:5173",
				"http://localhost:8081",
				"http://localhost:3000",
			}

			allowed := false
			for _, allowedOrigin := range allowedOrigins {
				if origin == allowedOrigin {
					allowed = true
					break
				}
			}

			if allowed {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}

			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}

			next.ServeHTTP(w, r)
		})
	})

	// Request size limit
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, 10*1024*1024) // 10MB
			next.ServeHTTP(w, r)
		})
	})

	// API routes
	r.Route("/api", func(r chi.Router) {
		apiHandler := api.NewHandler(cfg, providerRepo, database, adminMgr)
		apiHandler.Register(r)
	})

	// Proxy routes
	r.Route("/v1", func(r chi.Router) {
		proxyHandler := proxy.NewHandler(cfg, providerRepo, modelRepo, database.Pool())
		proxyHandler.Register(r)
	})

	// Root endpoint for health check
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK"))
	})

	// Static file serving for frontend (catch-all)
	r.Handle("/*", http.FileServer(http.Dir("./web/dist")))

	server := &http.Server{
		Addr:    cfg.Port,
		Handler: r,
	}

	go func() {
		log.Printf("Server listening on %s", cfg.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed to start: %v", err)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down server gracefully...")
	shutdownCtx, shutdownCancel := context.WithTimeout(ctx, 10*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("Error during server shutdown: %v", err)
	}

	log.Println("Server stopped")
}

// Simple rate limiter implementation
type RateLimiter struct {
	clients map[string]int
}

func NewRateLimiter() *RateLimiter {
	return &RateLimiter{
		clients: make(map[string]int),
	}
}

func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clientIP := r.RemoteAddr

		// Simple in-memory rate limiting
		// In production, use Redis or similar
		count := rl.clients[clientIP]
		if count >= 100 {
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		rl.clients[clientIP] = count + 1

		next.ServeHTTP(w, r)
	})
}

func (rl *RateLimiter) Cleanup() {
	// Reset rate limits every minute
	ticker := time.NewTicker(1 * time.Minute)
	go func() {
		for range ticker.C {
			rl.clients = make(map[string]int)
		}
	}()
}

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/anthropic"
	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/auth"
	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/dashboard"
	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/joycode"
	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/openai"
	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/proxy"
	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/store"
)

var (
	serveHost       string
	servePort       int
	requestCounter uint64
)

var serveCmd = &cobra.Command{
	Use:     "serve",
	Short:   "启动代理服务器",
	Long:    "启动 OpenAI/Anthropic 兼容的 API 代理服务器，将请求转换为 JoyCode API 格式。",
	GroupID: "core",
	Example: `  # 默认启动（0.0.0.0:34891）
  joycode-proxy serve

  # 指定端口
  joycode-proxy serve -p 8080

  # 启用调试日志
  joycode-proxy -v serve

  # 跳过凭据验证（用于测试）
  joycode-proxy serve --skip-validation`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if os.Getenv("_JOYCODE_DAEMON_CHILD") == "1" {
			runAsDaemonChild()
		}

		client, err := resolveClient()
		if err != nil {
			return err
		}

		// Open store for dashboard
		s, err := store.Open("")
		if err != nil {
			log.Printf("Warning: dashboard store unavailable: %v", err)
		}

		// Migrate historical request_logs: map old api_keys to first account
		if s != nil {
			accounts, _ := s.ListAccounts()
			if len(accounts) > 0 {
				if n, err := s.ReassignLogs([]string{"", "joycode", "default"}, accounts[0].APIKey); err == nil && n > 0 {
					log.Printf("Migrated %d request logs to account %q", n, accounts[0].APIKey)
				}
			}
		}
		if n, err := s.MigrateTokenLogs(); err == nil && n > 0 {
			log.Printf("Migrated %d token-based request logs to account api_keys", n)
		}

		srv := openai.NewServer(client, s)
		anth := anthropic.NewHandler(client, s)

		// Per-request client resolution from database accounts
		if s != nil {
			// Shared transport for connection pooling and limits
			sharedTransport := &http.Transport{
				MaxIdleConnsPerHost: 10,
				MaxConnsPerHost:     20,
				IdleConnTimeout:     90 * time.Second,
			}

			// Background goroutine to sync max_connections setting
			go func() {
				ticker := time.NewTicker(10 * time.Second)
				defer ticker.Stop()
				for range ticker.C {
					maxConns := s.GetIntSetting("max_connections", 20)
					if maxConns < 1 {
						maxConns = 1
					}
					sharedTransport.MaxConnsPerHost = maxConns
					idle := maxConns / 2
					if idle < 2 {
						idle = 2
					}
					sharedTransport.MaxIdleConnsPerHost = idle
				}
			}()

			resolver := func(r *http.Request) *joycode.Client {
				apiKey := r.Header.Get("x-api-key")
				if apiKey == "" {
					auth := r.Header.Get("Authorization")
					if strings.HasPrefix(auth, "Bearer ") {
						apiKey = strings.TrimPrefix(auth, "Bearer ")
					}
				}
				timeout := s.GetIntSetting("request_timeout", 120)
				if timeout < 60 {
					timeout = 60
				}
				if apiKey != "" {
					if account, _ := s.GetAccountByToken(apiKey); account != nil {
						cl := joycode.NewClient(account.PtKey, account.UserID)
						cl.SetTimeout(time.Duration(timeout) * time.Second)
						return cl
					}
					if account, _ := s.GetAccount(apiKey); account != nil {
						cl := joycode.NewClient(account.PtKey, account.UserID)
						cl.SetTimeout(time.Duration(timeout) * time.Second)
						return cl
					}
				}
				if account, _ := s.GetDefaultAccount(); account != nil {
					cl := joycode.NewClient(account.PtKey, account.UserID)
					cl.SetTimeout(time.Duration(timeout) * time.Second)
					cl.SetTransport(sharedTransport)
					return cl
				}
				return client
			}
			srv.Resolver = resolver
			anth.Resolver = resolver
		}

		// Background log cleanup goroutine
		go func() {
			ticker := time.NewTicker(1 * time.Hour)
			defer ticker.Stop()
			if days := s.GetIntSetting("log_retention_days", 30); days > 0 {
				s.CleanupOldLogs(days)
			}
			for range ticker.C {
				if days := s.GetIntSetting("log_retention_days", 30); days > 0 {
					s.CleanupOldLogs(days)
				}
			}
		}()

		mux := http.NewServeMux()
		srv.RegisterRoutes(mux)
		anth.RegisterRoutes(mux)

		// Register dashboard API routes + static file serving
		if s != nil {
			subFS, _ := fs.Sub(staticFiles, "static")
			dash := dashboard.NewHandler(s, subFS)
			dash.RegisterRoutes(mux)
			mux.HandleFunc("/", dash.ServeStatic)
		}

		var handler http.Handler = mux
		if s != nil {
			handler = auth.JWTMiddleware(s, handler)
			handler = requestLogMiddleware(handler, s)
		}
		if verbose {
			handler = loggingMiddleware(handler)
		}

		addr := fmt.Sprintf("%s:%d", serveHost, servePort)
		httpSrv := &http.Server{
			Addr:    addr,
			Handler: handler,
		}

		go func() {
			log.Printf("JoyCode Proxy running on http://%s", addr)
			fmt.Println()
			fmt.Printf("  JoyCode Proxy %s\n", Version)
			fmt.Println("  ─────────────────────────────────────────────────")
			fmt.Println()
			fmt.Println("  Endpoints:")
			fmt.Println("    POST /v1/chat/completions  — Chat (OpenAI format)")
			fmt.Println("    POST /v1/messages          — Chat (Anthropic/Claude Code format)")
			fmt.Println("    POST /v1/web-search        — Web Search")
			fmt.Println("    POST /v1/rerank            — Rerank documents")
			fmt.Println("    GET  /v1/models            — Model list")
			fmt.Println("    GET  /health               — Health check")
			fmt.Println()
			fmt.Println("  Dashboard:")
			fmt.Printf("    http://%s — Web UI\n", addr)
			fmt.Println()
			fmt.Println("  Claude Code setup:")
			fmt.Printf("    export ANTHROPIC_BASE_URL=http://%s\n", addr)
			fmt.Println("    export ANTHROPIC_API_KEY=joycode")
			if verbose {
				fmt.Println()
				fmt.Println("  Verbose logging: enabled")
			}
			fmt.Println()
			if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Fatalf("Server error: %v", err)
			}
		}()

		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		<-quit
		log.Println("Shutting down server...")

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpSrv.Shutdown(ctx); err != nil {
			log.Printf("Server shutdown error: %v", err)
		}
		if s != nil {
			s.Close()
		}
		log.Println("Server stopped")
		return nil
	},
}

func init() {
	serveCmd.Flags().StringVarP(&serveHost, "host", "H", "0.0.0.0", "绑定地址")
	serveCmd.Flags().IntVarP(&servePort, "port", "p", 34891, "绑定端口")
	rootCmd.AddCommand(serveCmd)
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		log.Printf("-> %s %s %s", r.Method, r.URL.Path, r.RemoteAddr)
		next.ServeHTTP(w, r)
		log.Printf("<- %s %s %s", r.Method, r.URL.Path, time.Since(start))
	})
}

func requestLogMiddleware(next http.Handler, s *store.Store) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		r = store.InitTokenUsage(r)
		r = store.InitModel(r)
		r = store.InitAccountModel(r)

		// Track active sessions for /v1/ requests
		if strings.HasPrefix(r.URL.Path, "/v1/") {
			ak := r.Header.Get("x-api-key")
			if ak == "" {
				authHeader := r.Header.Get("Authorization")
				if strings.HasPrefix(authHeader, "Bearer ") {
					ak = strings.TrimPrefix(authHeader, "Bearer ")
				}
			}
			var resolvedKey string
			if ak != "" {
				if account, _ := s.GetAccountByToken(ak); account != nil {
					resolvedKey = account.APIKey
				} else if account, _ := s.GetAccount(ak); account != nil {
					resolvedKey = account.APIKey
				}
			}
			if resolvedKey == "" {
				if a, _ := s.GetDefaultAccount(); a != nil {
					resolvedKey = a.APIKey
				}
			}
			if resolvedKey != "" {
				done := proxy.TrackActive(resolvedKey)
				defer done()
			}
		}

		// Assign request ID for log correlation
		reqID := atomic.AddUint64(&requestCounter, 1)
		r = anthropic.WithRequestID(r, reqID)

		// Resolve account default model for handlers
		if s != nil {
			ak := r.Header.Get("x-api-key")
			if ak == "" {
				if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
					ak = strings.TrimPrefix(auth, "Bearer ")
				}
			}
			var acc *store.Account
			if ak != "" {
				if a, _ := s.GetAccountByToken(ak); a != nil {
					acc = a
				} else if a, _ := s.GetAccount(ak); a != nil {
					acc = a
				}
			}
			if acc == nil {
				if a, _ := s.GetDefaultAccount(); a != nil {
					acc = a
				}
			}
			if acc != nil {
				store.SetAccountDefaultModel(r, acc.DefaultModel)
			}
		}

		// Peek at body to extract model before handler consumes it
		var model string
		if r.Method == "POST" && r.Body != nil {
			bodyBytes, _ := io.ReadAll(io.LimitReader(r.Body, 100<<20))
			r.Body.Close()
			r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			var body map[string]interface{}
			if json.Unmarshal(bodyBytes, &body) == nil {
				if m, ok := body["model"].(string); ok {
					model = m
				}
			}
		}

		rw := &responseWriter{ResponseWriter: w, statusCode: 200}
		next.ServeHTTP(rw, r)

		// Log /v1/ requests
		path := r.URL.Path
		if strings.HasPrefix(path, "/v1/") {
			apiKey := r.Header.Get("x-api-key")
			if apiKey == "" {
				apiKey = r.Header.Get("Authorization")
				if strings.HasPrefix(apiKey, "Bearer ") {
					apiKey = strings.TrimPrefix(apiKey, "Bearer ")
				}
			}
			if apiKey != "" {
				if account, _ := s.GetAccountByToken(apiKey); account != nil {
					apiKey = account.APIKey
				}
			}
			if apiKey == "" {
				if a, _ := s.GetDefaultAccount(); a != nil {
					apiKey = a.APIKey
				}
			}

			isStream := r.URL.Query().Get("stream") != "" || path == "/v1/messages"
			latency := time.Since(start).Milliseconds()

			var errMsg string
			if rw.statusCode >= 400 {
				reqID := atomic.AddUint64(&requestCounter, 1)
				errMsg = fmt.Sprintf("HTTP %d on %s %s", rw.statusCode, r.Method, path)
				slog.Error("proxy error response",
					"request_id", reqID,
					"status", rw.statusCode,
					"method", r.Method,
					"path", path,
					"model", model,
					"latency_ms", latency,
					"api_key", apiKey,
					"error", errMsg,
				)
			}

			var inTk, outTk int
			inTk, outTk = store.GetTokenUsage(r)
			resolvedModel := store.GetModel(r)
				if resolvedModel != "" {
					model = resolvedModel
				}
				if s.GetSetting("enable_request_logging") != "false" {
					go s.LogRequest(apiKey, model, path, isStream, rw.statusCode, latency, errMsg, inTk, outTk)
				}
		}
	})
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

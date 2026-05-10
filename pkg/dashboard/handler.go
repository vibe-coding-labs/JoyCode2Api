package dashboard

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/auth"
	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/joycode"
	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/keepalive"
	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/proxy"
	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/store"
)

type Handler struct {
	store     *store.Store
	staticFS  fs.FS
	modelList []string
	keeper    *keepalive.Keeper
}

func NewHandler(s *store.Store, staticFS fs.FS, k *keepalive.Keeper) *Handler {
	return &Handler{
		store:     s,
		staticFS:  staticFS,
		modelList: joycode.Models,
		keeper:    k,
	}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	// Auth endpoints (no JWT required)
	mux.HandleFunc("/api/auth/status", h.handleAuthStatus)
	mux.HandleFunc("/api/auth/setup", h.handleAuthSetup)
	mux.HandleFunc("/api/auth/login", h.handleAuthLogin)
	mux.HandleFunc("/api/auth/change-password", h.handleChangePassword)

	// Dashboard endpoints (JWT required — enforced by middleware)
	mux.HandleFunc("/api/accounts", h.handleAccounts)
	mux.HandleFunc("/api/accounts/", h.handleAccountAction)
	mux.HandleFunc("/api/accounts-auto-login", h.handleAutoLogin)
	mux.HandleFunc("/api/accounts-clear-all", h.handleClearAllAccounts)
	mux.HandleFunc("/api/clear-joycode-session", h.handleClearJoyCodeSession)
	mux.HandleFunc("/api/browser-login", h.handleBrowserLogin)
	mux.HandleFunc("/api/oauth-callback", h.handleOAuthCallback)
	mux.HandleFunc("/api/qr-login/init", h.handleQRLoginInit)
	mux.HandleFunc("/api/qr-login/status", h.handleQRLoginStatus)
	mux.HandleFunc("/api/models", h.handleModels)
	mux.HandleFunc("/api/stats", h.handleStats)
	mux.HandleFunc("/api/settings", h.handleSettings)
	mux.HandleFunc("/api/health", h.handleHealth)
	mux.HandleFunc("/api/errors", h.handleErrors)
	mux.HandleFunc("/api/github-stars", h.handleGitHubStars)
}

// GitHub Stars cache
var (
	ghStarsCache     int
	ghStarsCacheTime time.Time
	ghStarsMu        sync.Mutex
)

const ghStarsCacheTTL = 1 * time.Hour
const ghRepo = "vibe-coding-labs/JoyCodeProxy"

func (h *Handler) handleGitHubStars(w http.ResponseWriter, r *http.Request) {
	setCors(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	ghStarsMu.Lock()
	if ghStarsCache > 0 && time.Since(ghStarsCacheTime) < ghStarsCacheTTL {
		stars := ghStarsCache
		ghStarsMu.Unlock()
		writeJSON(w, http.StatusOK, map[string]interface{}{"stars": stars})
		return
	}
	ghStarsMu.Unlock()

	resp, err := http.Get("https://api.github.com/repos/" + ghRepo)
	if err != nil {
		slog.Warn("github stars fetch failed", "error", err)
		ghStarsMu.Lock()
		stars := ghStarsCache
		ghStarsMu.Unlock()
		writeJSON(w, http.StatusOK, map[string]interface{}{"stars": stars})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		slog.Warn("github stars non-200", "status", resp.StatusCode)
		ghStarsMu.Lock()
		stars := ghStarsCache
		ghStarsMu.Unlock()
		writeJSON(w, http.StatusOK, map[string]interface{}{"stars": stars})
		return
	}

	var result struct {
		StargazersCount int `json:"stargazers_count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		slog.Warn("github stars decode failed", "error", err)
		ghStarsMu.Lock()
		stars := ghStarsCache
		ghStarsMu.Unlock()
		writeJSON(w, http.StatusOK, map[string]interface{}{"stars": stars})
		return
	}

	ghStarsMu.Lock()
	ghStarsCache = result.StargazersCount
	ghStarsCacheTime = time.Now()
	stars := ghStarsCache
	ghStarsMu.Unlock()

	writeJSON(w, http.StatusOK, map[string]interface{}{"stars": stars})
}

// --- Errors Handler ---

func (h *Handler) handleErrors(w http.ResponseWriter, r *http.Request) {
	setCors(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := fmt.Sscanf(l, "%d", &limit); err == nil && n == 1 && limit > 0 && limit <= 200 {
			// ok
		} else {
			limit = 50
		}
	}
	logs, err := h.store.GetRecentErrors(limit)
	if err != nil {
		slog.Error("get recent errors", "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if logs == nil {
		logs = []store.RequestLog{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"errors": logs, "total": len(logs)})
}

// ServeStatic serves the SPA frontend for non-API routes.
func (h *Handler) ServeStatic(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// Handle JoyCode OAuth callback on root path: /?pt_key=xxx
	if path == "/" && r.URL.Query().Get("pt_key") != "" {
		h.handleOAuthCallback(w, r)
		return
	}

	if path == "/" {
		path = "/index.html"
	}

	// Try exact file
	if f, err := h.staticFS.Open(strings.TrimPrefix(path, "/")); err == nil {
		defer f.Close()
		stat, _ := f.Stat()
		if !stat.IsDir() {
			http.ServeContent(w, r, filepath.Base(path), stat.ModTime(), readFileSeeker{f})
			return
		}
	}

	// SPA fallback
	f, err := h.staticFS.Open("index.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	stat, _ := f.Stat()
	http.ServeContent(w, r, "index.html", stat.ModTime(), readFileSeeker{f})
}

// readFileSeeker wraps fs.File to implement io.ReadSeeker.
type readFileSeeker struct {
	fs.File
}

func (r readFileSeeker) Seek(offset int64, whence int) (int64, error) {
	if seeker, ok := r.File.(io.Seeker); ok {
		return seeker.Seek(offset, whence)
	}
	return 0, fmt.Errorf("not seekable")
}

// --- Helpers ---

// --- Auth Handlers ---

const jwtSecretKey = "auth_jwt_secret"
const passwordHashKey = "auth_password_hash"
const defaultJWTExpiry = 24 * time.Hour

func (h *Handler) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	setCors(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	hash := h.store.GetSetting(passwordHashKey)
	exePath, _ := os.Executable()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"initialized": hash != "",
		"exe_path":    exePath,
	})
}

func (h *Handler) handleAuthSetup(w http.ResponseWriter, r *http.Request) {
	setCors(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if h.store.GetSetting(passwordHashKey) != "" {
		writeError(w, http.StatusConflict, "root password already initialized")
		return
	}

	var body struct {
		Password string `json:"password"`
	}
	if !readJSONBody(w, r, &body) {
		return
	}
	if len(body.Password) < 6 {
		writeError(w, http.StatusBadRequest, "密码长度不能少于 6 位")
		return
	}

	hash, err := auth.HashPassword(body.Password)
	if err != nil {
		slog.Error("auth setup: hash password failed", "error", err)
		writeError(w, http.StatusInternalServerError, "密码加密失败")
		return
	}

	if err := h.store.SetSetting(passwordHashKey, hash); err != nil {
		slog.Error("auth setup: save password failed", "error", err)
		writeError(w, http.StatusInternalServerError, "保存密码失败")
		return
	}

	if h.store.GetSetting(jwtSecretKey) == "" {
		secret := generateRandomHex(32)
		h.store.SetSetting(jwtSecretKey, secret)
	}

	token, err := h.issueJWT()
	if err != nil {
		slog.Error("auth setup: issue JWT failed", "error", err)
		writeError(w, http.StatusInternalServerError, "生成 token 失败")
		return
	}

	slog.Info("auth: root password initialized")
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":    true,
		"token": token,
	})
}

func (h *Handler) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	setCors(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	hash := h.store.GetSetting(passwordHashKey)
	if hash == "" {
		writeError(w, http.StatusConflict, "root password not initialized")
		return
	}

	var body struct {
		Password string `json:"password"`
	}
	if !readJSONBody(w, r, &body) {
		return
	}

	if !auth.CheckPassword(body.Password, hash) {
		writeError(w, http.StatusUnauthorized, "密码错误")
		return
	}

	token, err := h.issueJWT()
	if err != nil {
		slog.Error("auth login: issue JWT failed", "error", err)
		writeError(w, http.StatusInternalServerError, "生成 token 失败")
		return
	}

	slog.Info("auth: root login success")
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":    true,
		"token": token,
	})
}

func (h *Handler) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	setCors(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	hash := h.store.GetSetting(passwordHashKey)
	if hash == "" {
		writeError(w, http.StatusConflict, "root password not initialized")
		return
	}

	var body struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if !readJSONBody(w, r, &body) {
		return
	}

	if !auth.CheckPassword(body.OldPassword, hash) {
		writeError(w, http.StatusUnauthorized, "原密码错误")
		return
	}

	if len(body.NewPassword) < 6 {
		writeError(w, http.StatusBadRequest, "新密码长度不能少于 6 位")
		return
	}

	newHash, err := auth.HashPassword(body.NewPassword)
	if err != nil {
		slog.Error("change password: hash failed", "error", err)
		writeError(w, http.StatusInternalServerError, "密码加密失败")
		return
	}

	if err := h.store.SetSetting(passwordHashKey, newHash); err != nil {
		slog.Error("change password: save failed", "error", err)
		writeError(w, http.StatusInternalServerError, "保存密码失败")
		return
	}

	slog.Info("auth: root password changed")
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}

func (h *Handler) issueJWT() (string, error) {
	secret := h.store.GetSetting(jwtSecretKey)
	if secret == "" {
		return "", fmt.Errorf("JWT secret not configured")
	}
	return auth.GenerateToken("root", secret, defaultJWTExpiry)
}

func generateRandomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func formatKeys(m map[string]interface{}) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

func setCors(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, x-api-key")
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	setCors(w)
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"detail": msg})
}

func readJSONBody(w http.ResponseWriter, r *http.Request, v interface{}) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return false
	}
	return true
}

// --- Account Handlers ---

func (h *Handler) handleAccounts(w http.ResponseWriter, r *http.Request) {
	setCors(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.listAccounts(w, r)
	case http.MethodPost:
		h.addAccount(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) listAccounts(w http.ResponseWriter, r *http.Request) {
	accounts, err := h.store.ListAccounts()
	if err != nil {
		slog.Error("list accounts", "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if accounts == nil {
		accounts = []store.AccountInfo{}
	}
	for i := range accounts {
		accounts[i].ActiveSessions = proxy.GetActiveSessions(accounts[i].UserID)
	}
	h.store.FillAccountStats(accounts)
	if h.keeper != nil {
		statuses := h.keeper.GetAllStatuses()
		for i := range accounts {
			if s, ok := statuses[accounts[i].UserID]; ok {
				if s.Valid { accounts[i].CredentialValid = 1 } else { accounts[i].CredentialValid = 0 }
				accounts[i].CredentialCheckedAt = s.LastChecked.Format("2006-01-02 15:04:05")
				accounts[i].CredentialError = s.ErrorMessage
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"accounts": accounts})
}

func (h *Handler) addAccount(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Nickname     string `json:"nickname"`
		PtKey        string `json:"pt_key"`
		UserID       string `json:"user_id"`
		IsDefault    *bool  `json:"is_default"`
		DefaultModel string `json:"default_model"`
	}
	if !readJSONBody(w, r, &body) {
		return
	}
	if body.UserID == "" || body.PtKey == "" {
		writeError(w, http.StatusBadRequest, "user_id and pt_key are required")
		return
	}

	isDefault := false
	if body.IsDefault != nil {
		isDefault = *body.IsDefault
	}

	if err := h.store.AddAccount(body.UserID, body.PtKey, body.Nickname, isDefault, body.DefaultModel); err != nil {
		slog.Error("add account", "user_id", body.UserID, "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "user_id": body.UserID, "nickname": body.Nickname})
}

func (h *Handler) handleAutoLogin(w http.ResponseWriter, r *http.Request) {
	setCors(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	creds, err := auth.LoadFromSystem()
	if err != nil {
		slog.Error("auto-login: load from system failed", "error", err)
		writeError(w, http.StatusBadRequest, "无法从本机获取 JoyCode 凭据: "+err.Error())
		return
	}

	client := joycode.NewClient(creds.PtKey, creds.UserID)
	userInfo, err := client.UserInfo()
	if err != nil {
		slog.Error("auto-login: userInfo request failed", "user_id", creds.UserID, "error", err)
		writeError(w, http.StatusUnauthorized, "凭据验证失败，请先在 JoyCode IDE 中登录: "+err.Error())
		return
	}

	code, ok := userInfo["code"].(float64)
	if !ok || code != 0 {
		msg := "未知错误"
		if m, ok := userInfo["msg"].(string); ok && m != "" {
			msg = m
		}
		slog.Error("auto-login: credentials invalid", "user_id", creds.UserID, "code", code, "msg", msg)
		writeError(w, http.StatusUnauthorized, "凭据已过期或无效: "+msg)
		return
	}

	// Extract userID from data.userId (more reliable than system creds)
	userID := creds.UserID
	nickname := userID
	realName := ""
	if data, ok := userInfo["data"].(map[string]interface{}); ok {
		if id, ok := data["userId"].(string); ok && id != "" {
			userID = id
		}
		if name, ok := data["realName"].(string); ok && name != "" {
			nickname = name
			realName = name
		}
	}
	if nickname == "" {
		nickname = userID
	}

	if userID == "" {
		slog.Error("auto-login: userId not found from system or API", "creds_user_id", creds.UserID)
		writeError(w, http.StatusBadRequest, "无法获取用户ID，请先在 JoyCode IDE 中登录")
		return
	}

	isDefault := true
	accounts, _ := h.store.ListAccounts()
	for _, a := range accounts {
		if a.IsDefault {
			isDefault = false
			break
		}
	}

	if err := h.store.AddAccount(userID, creds.PtKey, nickname, isDefault, "GLM-5.1"); err != nil {
		slog.Error("auto-login: save account failed", "user_id", userID, "error", err)
		writeError(w, http.StatusInternalServerError, "保存账号失败: "+err.Error())
		return
	}

	slog.Info("auto-login: account saved", "user_id", userID, "nickname", nickname)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":         true,
		"user_id":    userID,
		"nickname":   nickname,
		"real_name":  realName,
		"is_default": isDefault,
	})
}

func (h *Handler) handleClearAllAccounts(w http.ResponseWriter, r *http.Request) {
	setCors(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	n, err := h.store.ClearAllAccounts()
	if err != nil {
		slog.Error("clear-all-accounts: failed", "error", err)
		writeError(w, http.StatusInternalServerError, "清空账号失败: "+err.Error())
		return
	}

	slog.Info("clear-all-accounts: all accounts deleted", "count", n)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":    true,
		"count": n,
	})
}

func (h *Handler) handleBrowserLogin(w http.ResponseWriter, r *http.Request) {
	setCors(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	host := r.Host
	port := "34891"
	if _, p, err := net.SplitHostPort(host); err == nil {
		port = p
	} else if strings.Contains(host, ":") {
		_, port, _ = net.SplitHostPort(host)
	}

	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}
	token := hex.EncodeToString(b)

	loginURL := fmt.Sprintf(
		"https://joycode.jd.com/login/?ideAppName=JoyCode&fromIde=ide&redirect=0&authPort=%s&authKey=%s",
		url.QueryEscape(port), url.QueryEscape(token),
	)

	slog.Info("browser-login: generated login URL", "port", port, "token", token)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":    true,
		"url":   loginURL,
		"token": token,
	})
}

func (h *Handler) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	setCors(w)

	ptKey := r.URL.Query().Get("pt_key")
	loginType := r.URL.Query().Get("login_type")
	tenant := r.URL.Query().Get("tenant")
	authKey := r.URL.Query().Get("authKey")

	slog.Info("oauth-callback: received", "login_type", loginType, "tenant", tenant, "auth_key", authKey, "pt_key_len", len(ptKey))

	if ptKey == "" {
		writeError(w, http.StatusBadRequest, "missing pt_key parameter")
		return
	}

	client := joycode.NewClient(ptKey, "")
	userInfo, err := client.UserInfo()
	if err != nil {
		slog.Error("oauth-callback: userInfo validation failed", "error", err)
		http.Redirect(w, r, "/?login_error="+url.QueryEscape(err.Error()), http.StatusFound)
		return
	}

	code, _ := userInfo["code"].(float64)
	if code != 0 {
		msg, _ := userInfo["msg"].(string)
		slog.Error("oauth-callback: userInfo API error", "code", code, "msg", msg)
		http.Redirect(w, r, "/?login_error="+url.QueryEscape(msg), http.StatusFound)
		return
	}

	userID := ""
	nickname := ""
	realName := ""
	if data, ok := userInfo["data"].(map[string]interface{}); ok {
		if id, ok := data["userId"].(string); ok && id != "" {
			userID = id
		}
		if name, ok := data["realName"].(string); ok && name != "" {
			nickname = name
			realName = name
		}
	}
	if nickname == "" {
		nickname = userID
	}

	if userID == "" {
		slog.Error("oauth-callback: userId not found in userInfo response", "keys", formatKeys(userInfo))
		http.Redirect(w, r, "/?login_error="+url.QueryEscape("无法获取用户ID，请重新授权"), http.StatusFound)
		return
	}

	// Log full userInfo response for debugging user_id availability
	slog.Info("oauth-callback: userInfo response", "user_id", userID, "nickname", nickname, "real_name", realName, "keys", formatKeys(userInfo))
	if data, ok := userInfo["data"].(map[string]interface{}); ok {
		slog.Info("oauth-callback: userInfo.data fields", "keys", formatKeys(data))
	}

	isDefault := true
	accounts, _ := h.store.ListAccounts()
	for _, a := range accounts {
		if a.IsDefault {
			isDefault = false
			break
		}
	}

	if err := h.store.AddAccount(userID, ptKey, nickname, isDefault, "GLM-5.1"); err != nil {
		slog.Error("oauth-callback: save account failed", "user_id", userID, "error", err)
		http.Redirect(w, r, "/?login_error="+url.QueryEscape(err.Error()), http.StatusFound)
		return
	}

	slog.Info("oauth-callback: account saved", "user_id", userID, "nickname", nickname, "real_name", realName)

	// Auto-issue JWT so the frontend dashboard is immediately accessible
	jwtSecret := h.store.GetSetting("auth_jwt_secret")
	if jwtSecret != "" {
		if token, err := auth.GenerateToken(userID, jwtSecret, 7*24*time.Hour); err == nil {
			http.SetCookie(w, &http.Cookie{
				Name:     "joycode_auto_jwt",
				Value:    token,
				Path:     "/",
				MaxAge:   30,
				HttpOnly: false,
				SameSite: http.SameSiteLaxMode,
			})
		}
	}

	http.Redirect(w, r, "/?login_success="+url.QueryEscape(userID), http.StatusFound)
}

func (h *Handler) handleQRLoginInit(w http.ResponseWriter, r *http.Request) {
	setCors(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	sessionID, qrImage, err := auth.QRInit()
	if err != nil {
		slog.Error("qr-login init", "error", err)
		writeError(w, http.StatusInternalServerError, "生成二维码失败: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":         true,
		"session_id": sessionID,
		"qr_image":   "data:image/png;base64," + qrImage,
	})
}

func (h *Handler) handleQRLoginStatus(w http.ResponseWriter, r *http.Request) {
	setCors(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	sessionID := r.URL.Query().Get("session")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "missing session parameter")
		return
	}

	status, result, err := auth.QRPollStatus(sessionID)
	if err != nil {
		slog.Error("qr-login poll", "session", sessionID, "error", err)
		resp := map[string]interface{}{
			"status":  "error",
			"message": err.Error(),
		}
		var verifyErr *auth.QRVerifyNeededError
		if errors.As(err, &verifyErr) {
			resp["status"] = "verification_required"
			resp["verify_url"] = verifyErr.VerifyURL
			resp["risk_code"] = verifyErr.RiskCode
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}

	if status != "confirmed" {
		slog.Debug("qr-login poll", "session", sessionID, "status", status)
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"status": status,
		})
		return
	}

	nickname := result.RealName
	if nickname == "" {
		nickname = result.UserID
	}

	if result.UserID == "" {
		slog.Error("qr-login: userId not found in QR login result")
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"status":  "error",
			"ok":      false,
			"message": "二维码登录未返回有效的用户ID，请重试",
		})
		return
	}

	isDefault := true
	accounts, _ := h.store.ListAccounts()
	for _, a := range accounts {
		if a.IsDefault {
			isDefault = false
			break
		}
	}

	if err := h.store.AddAccount(result.UserID, result.PtKey, nickname, isDefault, "GLM-5.1"); err != nil {
		slog.Error("qr-login save account failed", "user_id", result.UserID, "error", err)
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"status":  "confirmed",
			"ok":      false,
			"user_id": result.UserID,
			"message": "登录成功但保存账号失败: " + err.Error(),
		})
		return
	}

	slog.Info("qr-login: account saved", "user_id", result.UserID, "nickname", nickname)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":    "confirmed",
		"ok":        true,
		"user_id":   result.UserID,
		"nickname":  nickname,
		"real_name": result.RealName,
	})
}

func (h *Handler) handleAccountAction(w http.ResponseWriter, r *http.Request) {
	setCors(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	path := r.URL.Path
	// /api/accounts/{apiKey}/...
	parts := strings.Split(strings.TrimPrefix(path, "/api/accounts/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusBadRequest, "missing api_key")
		return
	}

	apiKey := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	switch {
	case action == "" && apiKey == "reorder" && r.Method == http.MethodPut:
		h.reorderAccounts(w, r)
	case action == "" && r.Method == http.MethodDelete:
		h.removeAccount(w, r, apiKey)
	case action == "default" && r.Method == http.MethodPut:
		h.setDefault(w, r, apiKey)
	case action == "validate" && r.Method == http.MethodPost:
		h.validateAccount(w, r, apiKey)
	case action == "model" && r.Method == http.MethodPut:
		h.updateModel(w, r, apiKey)
	case action == "models" && r.Method == http.MethodGet:
		h.listAccountModels(w, r, apiKey)
	case action == "stats" && r.Method == http.MethodGet:
		h.getAccountStats(w, r, apiKey)
	case action == "logs" && r.Method == http.MethodGet:
		h.getAccountLogs(w, r, apiKey)
	case action == "renew-token" && r.Method == http.MethodPost:
		h.renewToken(w, r, apiKey)
	case action == "remark" && r.Method == http.MethodPut:
		h.updateRemark(w, r, apiKey)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) reorderAccounts(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UserIDs []string `json:"user_ids"`
	}
	if !readJSONBody(w, r, &body) {
		return
	}
	if len(body.UserIDs) == 0 {
		writeError(w, http.StatusBadRequest, "user_ids is required")
		return
	}
	if err := h.store.ReorderAccounts(body.UserIDs); err != nil {
		slog.Error("reorder accounts", "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}

func (h *Handler) removeAccount(w http.ResponseWriter, r *http.Request, apiKey string) {
	if err := h.store.RemoveAccount(apiKey); err != nil {
		slog.Error("remove account", "api_key", apiKey, "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}

func (h *Handler) setDefault(w http.ResponseWriter, r *http.Request, apiKey string) {
	if err := h.store.SetDefault(apiKey); err != nil {
		slog.Error("set default account", "api_key", apiKey, "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}

func (h *Handler) validateAccount(w http.ResponseWriter, r *http.Request, apiKey string) {
	account, err := h.store.GetAccount(apiKey)
	if err != nil {
		slog.Error("get account", "api_key", apiKey, "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if account == nil {
		writeError(w, http.StatusNotFound, "account not found")
		return
	}

	client := joycode.NewClient(account.PtKey, account.UserID)
	valid := true
	if err := client.Validate(); err != nil {
		valid = false
		slog.Error("validate account", "api_key", apiKey, "error", err)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"api_key": apiKey, "valid": valid})
}

func (h *Handler) updateModel(w http.ResponseWriter, r *http.Request, apiKey string) {
	var body struct {
		DefaultModel string `json:"default_model"`
	}
	if !readJSONBody(w, r, &body) {
		return
	}
	if err := h.store.UpdateAccountModel(apiKey, body.DefaultModel); err != nil {
		slog.Error("update account model", "api_key", apiKey, "model", body.DefaultModel, "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "api_key": apiKey, "default_model": body.DefaultModel})
}

func (h *Handler) listAccountModels(w http.ResponseWriter, r *http.Request, apiKey string) {
	account, err := h.store.GetAccount(apiKey)
	if err != nil {
		slog.Error("get account", "api_key", apiKey, "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if account == nil {
		writeError(w, http.StatusNotFound, "account not found")
		return
	}

	client := joycode.NewClient(account.PtKey, account.UserID)
	models, err := client.ListModels()
	if err != nil {
		slog.Error("list account models", "api_key", apiKey, "error", err)
		// Fallback to hardcoded list
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"models": modelInfos(h.modelList),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"models": models})
}

func (h *Handler) getAccountStats(w http.ResponseWriter, r *http.Request, apiKey string) {
	stats, err := h.store.GetAccountStats(apiKey)
	if err != nil {
		slog.Error("get account stats", "api_key", apiKey, "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if stats.ByModel == nil {
		stats.ByModel = []store.ModelCount{}
	}
	if stats.ByEndpoint == nil {
		stats.ByEndpoint = []store.EndpointCount{}
	}
	writeJSON(w, http.StatusOK, stats)
}

func (h *Handler) getAccountLogs(w http.ResponseWriter, r *http.Request, apiKey string) {
	limit := 200
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := fmt.Sscanf(l, "%d", &limit); err == nil && n == 1 && limit > 0 && limit <= 1000 {
			// ok
		} else {
			limit = 200
		}
	}
	logs, err := h.store.GetAccountLogs(apiKey, limit)
	if err != nil {
		slog.Error("get account logs", "api_key", apiKey, "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if logs == nil {
		logs = []store.RequestLog{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"logs": logs, "total": len(logs)})
}

func (h *Handler) renewToken(w http.ResponseWriter, r *http.Request, apiKey string) {
	token, err := h.store.RenewToken(apiKey)
	if err != nil {
		slog.Error("renew token", "api_key", apiKey, "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "api_token": token})
}

func (h *Handler) updateRemark(w http.ResponseWriter, r *http.Request, userID string) {
	var body struct {
		Remark string `json:"remark"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.store.UpdateRemark(userID, body.Remark); err != nil {
		slog.Error("update remark", "user_id", userID, "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "user_id": userID, "remark": body.Remark})
}

func (h *Handler) handleClearJoyCodeSession(w http.ResponseWriter, r *http.Request) {
	setCors(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	home, err := os.UserHomeDir()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot determine home directory")
		return
	}
	dbPath := filepath.Join(home, "Library", "Application Support", "JoyCode", "User", "globalStorage", "state.vscdb")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		writeError(w, http.StatusNotFound, "JoyCode 本地数据库不存在，请先安装 JoyCode IDE")
		return
	}

	db, err := sql.Open("sqlite3", dbPath+"?mode=rw")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "无法打开 JoyCode 数据库: "+err.Error())
		return
	}
	defer db.Close()

	result, err := db.Exec("DELETE FROM ItemTable WHERE key IN ('JoyCoder.IDE', 'joycode.storageUser')")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "清除会话失败: "+err.Error())
		return
	}
	n, _ := result.RowsAffected()

	// Also clear jdhLoginInfo from JoyCode.joycoder-editor to prevent auto-restore
	var editorVal string
	if err := db.QueryRow("SELECT value FROM ItemTable WHERE key = 'JoyCode.joycoder-editor'").Scan(&editorVal); err == nil {
		var editor map[string]interface{}
		if json.Unmarshal([]byte(editorVal), &editor) == nil {
			delete(editor, "jdhLoginInfo")
			if newVal, err := json.Marshal(editor); err == nil {
				db.Exec("UPDATE ItemTable SET value = ? WHERE key = 'JoyCode.joycoder-editor'", string(newVal))
				n++
			}
		}
	}

	slog.Info("clear-joycode-session: cleared", "rows_affected", n)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":      true,
		"message": fmt.Sprintf("JoyCode 本地会话已彻底清除（影响 %d 条记录），请重新打开 JoyCode IDE 登录", n),
	})
}

// --- Model Handlers ---

func (h *Handler) handleModels(w http.ResponseWriter, r *http.Request) {
	setCors(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"models": modelInfos(h.modelList),
	})
}

func modelInfos(models []string) []map[string]string {
	result := make([]map[string]string, len(models))
	for i, m := range models {
		result[i] = map[string]string{"id": m, "name": m}
	}
	return result
}

// --- Stats Handler ---

func (h *Handler) handleStats(w http.ResponseWriter, r *http.Request) {
	setCors(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	stats, err := h.store.GetStats()
	if err != nil {
		slog.Error("get global stats", "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if stats.ByModel == nil {
		stats.ByModel = []store.ModelCount{}
	}
	if stats.ByAccount == nil {
		stats.ByAccount = []store.AccountCount{}
	}

	totals, _ := h.store.GetAllTimeTotals()
	hourly, _ := h.store.GetHourlyStats()
	if hourly == nil {
		hourly = []store.HourlyData{}
	}

	resp := map[string]interface{}{
		"total_requests":       stats.TotalRequests,
		"total_input_tokens":   stats.TotalInputTk,
		"total_output_tokens":  stats.TotalOutputTk,
		"accounts_count":       stats.AccountsCount,
		"avg_latency_ms":       stats.AvgLatencyMs,
		"error_count":          stats.ErrorCount,
		"stream_count":         stats.StreamCount,
		"success_count":        stats.SuccessCount,
		"by_model":             stats.ByModel,
		"by_account":           stats.ByAccount,
		"all_time":             totals,
		"hourly":               hourly,
	}
	writeJSON(w, http.StatusOK, resp)
}

// --- Settings Handler ---

func (h *Handler) handleSettings(w http.ResponseWriter, r *http.Request) {
	setCors(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	switch r.Method {
	case http.MethodGet:
		settings, err := h.store.GetSettings()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if settings == nil {
			settings = map[string]string{}
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"settings": settings})

	case http.MethodPut:
		var settings map[string]string
		if !readJSONBody(w, r, &settings) {
			return
		}
		if err := h.store.SetSettings(settings); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// --- Health Handler ---

func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	setCors(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	accounts, _ := h.store.ListAccounts()
	count := 0
	if accounts != nil {
		count = len(accounts)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":   "ok",
		"accounts": count,
		"version":  "0.3.0",
	})
}

package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const (
	DefaultDBDir  = ".joycode-proxy"
	DefaultDBName = "proxy.db"
	encKeyFile    = ".enc_key"
)

type Account struct {
	UserID       string `json:"user_id"`
	Nickname     string `json:"nickname"`
	Remark       string `json:"remark"`
	APIToken     string `json:"api_token"`
	PtKey        string `json:"-"`
	IsDefault    bool   `json:"is_default"`
	DefaultModel string `json:"default_model"`
	CreatedAt    string `json:"created_at,omitempty"`
}

func (a *Account) DisplayName() string {
	if a.Remark != "" {
		return a.Remark
	}
	if a.Nickname != "" {
		return a.Nickname
	}
	return a.UserID
}

type AccountInfo struct {
	UserID          string `json:"user_id"`
	Nickname        string `json:"nickname"`
	Remark          string `json:"remark"`
	APIToken        string `json:"api_token"`
	IsDefault       bool   `json:"is_default"`
	DefaultModel    string `json:"default_model"`
	CreatedAt       string `json:"created_at,omitempty"`
	DisplayOrder    int    `json:"display_order"`
	ActiveSessions  int64  `json:"active_sessions"`
	TotalRequests   int    `json:"total_requests"`
	TodayRequests   int    `json:"today_requests"`
	TotalTokens     int    `json:"total_tokens"`
	TodayTokens     int    `json:"today_tokens"`
	CredentialValid      int    `json:"credential_valid"`               // -1=unknown, 0=expired, 1=valid
	CredentialCheckedAt string `json:"credential_checked_at,omitempty"`
	CredentialRefreshAt string `json:"credential_refreshed_at,omitempty"`
	CredentialError     string `json:"credential_error,omitempty"`
}

func (a *AccountInfo) DisplayName() string {
	if a.Remark != "" {
		return a.Remark
	}
	if a.Nickname != "" {
		return a.Nickname
	}
	return a.UserID
}

type Stats struct {
	TotalRequests int            `json:"total_requests"`
	TotalInputTk  int            `json:"total_input_tokens"`
	TotalOutputTk int            `json:"total_output_tokens"`
	AccountsCount int            `json:"accounts_count"`
	AvgLatencyMs  float64        `json:"avg_latency_ms"`
	ErrorCount    int            `json:"error_count"`
	StreamCount   int            `json:"stream_count"`
	SuccessCount  int            `json:"success_count"`
	ByModel       []ModelCount   `json:"by_model"`
	ByAccount     []AccountCount `json:"by_account"`
}

type ModelCount struct {
	Model string `json:"model"`
	Count int    `json:"count"`
}

type AccountCount struct {
	UserID     string `json:"user_id"`
	Nickname   string `json:"nickname"`
	Remark     string `json:"remark"`
	Count      int    `json:"count"`
}

func (a *AccountCount) DisplayName() string {
	if a.Remark != "" {
		return a.Remark
	}
	if a.Nickname != "" {
		return a.Nickname
	}
	return a.UserID
}

type AccountStats struct {
	UserID        string          `json:"user_id"`
	Nickname      string          `json:"nickname"`
	Remark        string          `json:"remark"`
	TotalRequests int             `json:"total_requests"`
	TotalInputTk  int             `json:"total_input_tokens"`
	TotalOutputTk int             `json:"total_output_tokens"`
	SuccessCount  int             `json:"success_count"`
	StreamCount   int             `json:"stream_count"`
	ByModel       []ModelCount    `json:"by_model"`
	ByEndpoint    []EndpointCount `json:"by_endpoint"`
	AvgLatencyMs  float64         `json:"avg_latency_ms"`
	ErrorCount    int             `json:"error_count"`
	AllTime       *AllTimeTotals  `json:"all_time"`
	Hourly        []HourlyData    `json:"hourly"`
}

type EndpointCount struct {
	Endpoint string `json:"endpoint"`
	Count    int    `json:"count"`
}

type AllTimeTotals struct {
	TotalRequests int `json:"total_requests"`
	TotalInputTk  int `json:"total_input_tokens"`
	TotalOutputTk int `json:"total_output_tokens"`
	ErrorCount    int `json:"error_count"`
}

type HourlyData struct {
	Hour        string `json:"hour"`
	Count       int    `json:"count"`
	InputTokens int    `json:"input_tokens"`
	OutputTokens int   `json:"output_tokens"`
	Errors      int    `json:"errors"`
}

type RequestLog struct {
	ID           int64  `json:"id"`
	UserID       string `json:"user_id"`
	Model        string `json:"model"`
	Endpoint     string `json:"endpoint"`
	Stream       bool   `json:"stream"`
	StatusCode   int    `json:"status_code"`
	LatencyMs    int64  `json:"latency_ms"`
	ErrorMessage string `json:"error_message"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
	CreatedAt    string `json:"created_at"`
}

type Store struct {
	db     *sql.DB
	enc    cipher.AEAD
	mu     sync.Mutex
	dbPath string
}

func DefaultDBPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, DefaultDBDir)
	return filepath.Join(dir, DefaultDBName), nil
}

func Open(dbPath string) (*Store, error) {
	if dbPath == "" {
		var err error
		dbPath, err = DefaultDBPath()
		if err != nil {
			return nil, err
		}
	}

	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create db directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	s := &Store{db: db, dbPath: dbPath}

	encKey, err := s.loadOrCreateEncKey(dir)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("encryption key: %w", err)
	}

	block, err := aes.NewCipher(encKey)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("create cipher: %w", err)
	}

	s.enc, err = cipher.NewGCM(block)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("create GCM: %w", err)
	}

	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func generateToken() string {
	b := make([]byte, 32)
	io.ReadFull(rand.Reader, b)
	return "sk-joy-" + hex.EncodeToString(b)
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS accounts (
			user_id TEXT PRIMARY KEY,
			nickname TEXT DEFAULT '',
			remark TEXT DEFAULT '',
			api_token TEXT NOT NULL DEFAULT '',
			pt_key TEXT NOT NULL,
			is_default INTEGER DEFAULT 0,
			default_model TEXT DEFAULT '',
			created_at TEXT DEFAULT (datetime('now', 'localtime')),
			updated_at TEXT DEFAULT (datetime('now', 'localtime')),
			credential_refreshed_at TEXT DEFAULT '',
			credential_valid INTEGER DEFAULT -1,
			display_order INTEGER DEFAULT 0
		);
		CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at TEXT DEFAULT (datetime('now', 'localtime'))
		);
		CREATE TABLE IF NOT EXISTS request_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			api_key TEXT,
			model TEXT,
			endpoint TEXT,
			stream INTEGER DEFAULT 0,
			status_code INTEGER,
			latency_ms INTEGER,
			created_at TEXT DEFAULT (datetime('now', 'localtime'))
		);
	`)
	if err != nil {
		return err
	}

	// Migration: add error_message column to request_logs
	if err := addColumnIfMissing(s.db, "request_logs", "error_message", "TEXT DEFAULT ''"); err != nil {
		return err
	}

	// Migration: add token columns to request_logs
	if err := addColumnIfMissing(s.db, "request_logs", "input_tokens", "INTEGER DEFAULT 0"); err != nil {
		return err
	}
	if err := addColumnIfMissing(s.db, "request_logs", "output_tokens", "INTEGER DEFAULT 0"); err != nil {
		return err
	}

	// Migration: add display_order column to accounts
	if err := addColumnIfMissing(s.db, "accounts", "display_order", "INTEGER DEFAULT 0"); err != nil {
		return err
	}

	// Migration: migrate old schema (api_key as PK) to new schema (user_id as PK)
	s.migrateUserIDAsPK()

	// Migration: fix historical UTC timestamps to localtime
	s.migrateUTCTimestamps()

	// Migration: initialize display_order for existing accounts
	s.migrateDisplayOrder()

	// Indexes for request_logs (added here so they exist for both fresh and
	// upgraded databases; CREATE INDEX IF NOT EXISTS is a no-op if present).
	if _, err := s.db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_request_logs_api_key ON request_logs(api_key);
		CREATE INDEX IF NOT EXISTS idx_request_logs_created_at ON request_logs(created_at);
		CREATE INDEX IF NOT EXISTS idx_request_logs_created_at_api_key ON request_logs(created_at, api_key);
	`); err != nil {
		slog.Warn("store: create request_logs indexes failed", "error", err)
	}

	return nil
}

// addColumnIfMissing runs `ALTER TABLE ... ADD COLUMN` and ignores the
// "duplicate column name" error that SQLite returns when the column already
// exists. Any other error (disk full, locked DB, ...) is returned to the
// caller so migrations don't silently fail.
func addColumnIfMissing(db *sql.DB, table, column, typeDef string) error {
	_, err := db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, typeDef))
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "duplicate column name") {
		return nil
	}
	return fmt.Errorf("migrate: add column %s.%s: %w", table, column, err)
}

// migrateUserIDAsPK migrates the old accounts table (api_key as PK) to the new
// schema where user_id is the primary key. If the table already uses user_id as
// PK this is a no-op.
func (s *Store) migrateUserIDAsPK() {
	// Check if migration is needed: old schema has api_key as PK
	var colName string
	err := s.db.QueryRow("SELECT name FROM pragma_table_info('accounts') WHERE pk = 1").Scan(&colName)
	if err != nil {
		slog.Error("store: migrateUserIDAsPK pragma check failed", "error", err)
		return
	}
	if colName == "user_id" {
		// Already on new schema
		return
	}
	slog.Info("store: migrating accounts table from api_key PK to user_id PK")

	// Create new table
	_, err = s.db.Exec(`
		CREATE TABLE accounts_new (
			user_id TEXT PRIMARY KEY,
			nickname TEXT DEFAULT '',
			remark TEXT DEFAULT '',
			api_token TEXT NOT NULL DEFAULT '',
			pt_key TEXT NOT NULL,
			is_default INTEGER DEFAULT 0,
			default_model TEXT DEFAULT '',
			created_at TEXT DEFAULT (datetime('now', 'localtime')),
			updated_at TEXT DEFAULT (datetime('now', 'localtime')),
			credential_refreshed_at TEXT DEFAULT '',
			credential_valid INTEGER DEFAULT -1,
			display_order INTEGER DEFAULT 0
		)`)
	if err != nil {
		slog.Error("store: migrateUserIDAsPK create accounts_new failed", "error", err)
		return
	}

	// Copy data from old table:
	//   user_id = COALESCE(NULLIF(old.user_id, ''), 'local_' || old.api_key)
	//   nickname = old.api_key (the old display name becomes the nickname)
	//   api_token, pt_key, is_default, default_model, created_at, updated_at,
	//   credential_refreshed_at, credential_valid carried over
	_, err = s.db.Exec(`
		INSERT INTO accounts_new (user_id, nickname, api_token, pt_key, is_default, default_model, created_at, updated_at, credential_refreshed_at, credential_valid, display_order)
		SELECT
			CASE WHEN user_id = '' OR user_id IS NULL THEN 'local_' || api_key ELSE user_id END,
			api_key,
			COALESCE(api_token, ''),
			pt_key,
			is_default,
			COALESCE(default_model, ''),
			created_at,
			COALESCE(updated_at, created_at),
			COALESCE(credential_refreshed_at, ''),
			COALESCE(credential_valid, -1),
			COALESCE(display_order, 0)
		FROM accounts`)
	if err != nil {
		slog.Error("store: migrateUserIDAsPK copy data failed", "error", err)
		return
	}

	// Build a mapping of old api_key -> new user_id from the old table for log migration
	type mapping struct {
		oldAPIKey string
		newUserID string
	}
	rows, err := s.db.Query(`
		SELECT api_key,
			CASE WHEN user_id = '' OR user_id IS NULL THEN 'local_' || api_key ELSE user_id END
		FROM accounts`)
	if err == nil {
		var mappings []mapping
		for rows.Next() {
			var m mapping
			if rows.Scan(&m.oldAPIKey, &m.newUserID) == nil {
				mappings = append(mappings, m)
			}
		}
		rows.Close()

		// Migrate request_logs.api_key from old display names to new user_ids
		for _, m := range mappings {
			if m.oldAPIKey != m.newUserID {
				s.db.Exec("UPDATE request_logs SET api_key = ? WHERE api_key = ?", m.newUserID, m.oldAPIKey)
			}
		}
	}

	// Swap tables
	_, err = s.db.Exec("DROP TABLE accounts")
	if err != nil {
		slog.Error("store: migrateUserIDAsPK drop old table failed", "error", err)
		return
	}
	_, err = s.db.Exec("ALTER TABLE accounts_new RENAME TO accounts")
	if err != nil {
		slog.Error("store: migrateUserIDAsPK rename new table failed", "error", err)
		return
	}
	slog.Info("store: accounts table migrated to user_id PK successfully")
}

// migrateUTCTimestamps converts existing UTC timestamps to localtime.
//
// Heuristic: if the newest request_log's created_at is roughly |offset| hours
// behind (positive offset) or ahead (negative offset) of current localtime,
// the data was stored in UTC and needs to be shifted by the offset.
//
// The check is against the NEWEST record so the migration is idempotent:
// after shifting, the newest record sits at localtime and won't match again.
// Supports non-integer-hour offsets (e.g. UTC+5:30) via fractional hours.
func (s *Store) migrateUTCTimestamps() {
	_, offset := time.Now().Zone()
	if offset == 0 {
		return
	}
	hours := float64(offset) / 3600.0
	absHours := hours
	if absHours < 0 {
		absHours = -absHours
	}
	// SQLite datetime modifiers accept fractional hours, e.g. "+8.5 hours".
	// Format with enough precision for any real-world timezone offset.
	shift := fmt.Sprintf("%+g hours", hours)

	// Find the newest record. If it's more than ~30min off from localtime in
	// the direction of the offset, treat all existing data as UTC-stored.
	var newest string
	err := s.db.QueryRow("SELECT MAX(created_at) FROM request_logs").Scan(&newest)
	if err != nil || newest == "" {
		return
	}

	// diffExpr = localtime_now - newest (in hours, as a float string).
	// For UTC+8 with UTC-stored data: newest is ~8h behind → diff ≈ +8.
	// For UTC-5 with UTC-stored data: newest is ~5h ahead  → diff ≈ -5.
	// We trigger when |diff| > (absHours - 0.5), i.e. the gap is close to the
	// full offset and can't be explained by normal time passage.
	var diff float64
	err = s.db.QueryRow(
		"SELECT (strftime('%s','now','localtime') - strftime('%s', ?)) / 3600.0",
		newest,
	).Scan(&diff)
	if err != nil {
		slog.Warn("migrateUTCTimestamps: diff query failed", "error", err)
		return
	}
	// Same-sign check: positive offset expects positive diff (UTC behind local),
	// negative offset expects negative diff (UTC ahead of local).
	if (hours > 0 && diff < absHours-0.5) || (hours < 0 && diff > -absHours+0.5) {
		return
	}

	tx, err := s.db.Begin()
	if err != nil {
		slog.Warn("migrateUTCTimestamps: begin tx failed", "error", err)
		return
	}
	defer tx.Rollback()

	var fixed int64
	for _, q := range []string{
		"UPDATE request_logs SET created_at = datetime(created_at, '" + shift + "')",
		"UPDATE accounts SET created_at = datetime(created_at, '" + shift + "')",
		"UPDATE settings SET updated_at = datetime(updated_at, '" + shift + "')",
	} {
		res, err := tx.Exec(q)
		if err != nil {
			slog.Warn("migrateUTCTimestamps: update failed", "query", q, "error", err)
			return
		}
		if n, err := res.RowsAffected(); err == nil {
			fixed += n
		}
	}
	if err := tx.Commit(); err != nil {
		slog.Warn("migrateUTCTimestamps: commit failed", "error", err)
		return
	}
	slog.Info("migrated UTC timestamps to localtime", "offset_hours", hours, "records_fixed", fixed)
}

func (s *Store) migrateDisplayOrder() {
	var count int
	s.db.QueryRow("SELECT COUNT(*) FROM accounts WHERE display_order = 0").Scan(&count)
	if count == 0 {
		return
	}
	slog.Info("store: initializing display_order for existing accounts", "count", count)
	rows, err := s.db.Query("SELECT user_id FROM accounts ORDER BY created_at")
	if err != nil {
		slog.Error("store: migrateDisplayOrder query failed", "error", err)
		return
	}
	defer rows.Close()
	order := 1
	for rows.Next() {
		var userID string
		if err := rows.Scan(&userID); err != nil {
			continue
		}
		s.db.Exec("UPDATE accounts SET display_order = ? WHERE user_id = ?", order, userID)
		order++
	}
}

// --- Encryption ---

func (s *Store) loadOrCreateEncKey(dir string) ([]byte, error) {
	keyPath := filepath.Join(dir, encKeyFile)
	data, err := os.ReadFile(keyPath)
	if err == nil {
		key, err := hex.DecodeString(string(data))
		if err == nil && len(key) == 32 {
			return key, nil
		}
	}

	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}

	if err := os.WriteFile(keyPath, []byte(hex.EncodeToString(key)), 0600); err != nil {
		return nil, fmt.Errorf("write key: %w", err)
	}
	return key, nil
}

func (s *Store) encrypt(plaintext string) (string, error) {
	nonce := make([]byte, s.enc.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := s.enc.Seal(nonce, nonce, []byte(plaintext), nil)
	return hex.EncodeToString(ciphertext), nil
}

func (s *Store) decrypt(ciphertext string) (string, error) {
	data, err := hex.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}
	nonceSize := s.enc.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}
	nonce, ct := data[:nonceSize], data[nonceSize:]
	plaintext, err := s.enc.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

// --- Account CRUD ---

const MaxAccounts = 10

func (s *Store) AddAccount(userID, ptKey, nickname string, isDefault bool, defaultModel string) error {
	if userID == "" {
		return fmt.Errorf("user_id cannot be empty")
	}
	if ptKey == "" {
		return fmt.Errorf("pt_key cannot be empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if account already exists — updates bypass the limit
	var existingToken string
	err := s.db.QueryRow("SELECT api_token FROM accounts WHERE user_id = ?", userID).Scan(&existingToken)
	if err == nil {
		encPtKey, err := s.encrypt(ptKey)
		if err != nil {
			slog.Error("store: encrypt pt_key failed", "user_id", userID, "error", err)
			return fmt.Errorf("encrypt pt_key: %w", err)
		}
		_, err = s.db.Exec(
			"UPDATE accounts SET pt_key = ?, nickname = CASE WHEN nickname = '' OR nickname IS NULL THEN ? ELSE nickname END, updated_at = datetime('now', 'localtime') WHERE user_id = ?",
			encPtKey, nickname, userID,
		)
		if err != nil {
			slog.Error("store: update account failed", "user_id", userID, "error", err)
			return err
		}
		slog.Info("store: updated existing account credentials", "user_id", userID)
		return nil
	}

		// Check if another account already has the same pt_key (dedup by credential)
		rows, err := s.db.Query("SELECT user_id, pt_key FROM accounts")
		if err == nil {
			for rows.Next() {
				var existingUserID, encExistingPtKey string
				if rows.Scan(&existingUserID, &encExistingPtKey) != nil {
					continue
				}
				existingPtKey, decErr := s.decrypt(encExistingPtKey)
				if decErr != nil {
					continue
				}
				if existingPtKey == ptKey {
					rows.Close()
					encPtKey, encErr := s.encrypt(ptKey)
					if encErr != nil {
						slog.Error("store: encrypt pt_key failed", "user_id", userID, "error", encErr)
						return fmt.Errorf("encrypt pt_key: %w", encErr)
					}
					_, err = s.db.Exec(
						"UPDATE accounts SET user_id = ?, pt_key = ?, nickname = CASE WHEN nickname = '' OR nickname IS NULL THEN ? ELSE nickname END, updated_at = datetime('now', 'localtime') WHERE user_id = ?",
						userID, encPtKey, nickname, existingUserID,
					)
					if err != nil {
						slog.Error("store: update account (pt_key dedup) failed", "old_user_id", existingUserID, "new_user_id", userID, "error", err)
						return err
					}
					slog.Info("store: merged account by pt_key dedup", "old_user_id", existingUserID, "new_user_id", userID)
					return nil
				}
			}
			rows.Close()
		}

	// New account — enforce limit
	var count int
	s.db.QueryRow("SELECT COUNT(*) FROM accounts").Scan(&count)
	if count >= MaxAccounts {
		return fmt.Errorf("账号数量已达上限（%d 个）。本工具仅供个人学习和研究使用，禁止用于商业转售、API 中转服务或任何违法违规用途", MaxAccounts)
	}

	encPtKey, err := s.encrypt(ptKey)
	if err != nil {
		slog.Error("store: encrypt pt_key failed", "user_id", userID, "error", err)
		return fmt.Errorf("encrypt pt_key: %w", err)
	}

	// New account
	if isDefault {
		s.db.Exec("UPDATE accounts SET is_default = 0 WHERE is_default = 1")
	}

	def := 0
	if isDefault {
		def = 1
	}

	// Get max display_order
	var maxOrder int
	s.db.QueryRow("SELECT COALESCE(MAX(display_order), 0) FROM accounts").Scan(&maxOrder)

	token := generateToken()
	_, err = s.db.Exec(
		"INSERT INTO accounts (user_id, nickname, api_token, pt_key, is_default, default_model, display_order) VALUES (?, ?, ?, ?, ?, ?, ?)",
		userID, nickname, token, encPtKey, def, defaultModel, maxOrder+1,
	)
	if err != nil {
		slog.Error("store: add account failed", "user_id", userID, "error", err)
		return err
	}
	return nil
}

func (s *Store) ListAccounts() ([]AccountInfo, error) {
	rows, err := s.db.Query("SELECT user_id, nickname, remark, api_token, is_default, default_model, created_at, credential_valid, credential_refreshed_at, COALESCE(display_order, 0) FROM accounts ORDER BY display_order, created_at")
	if err != nil {
		slog.Error("store: list accounts query failed", "error", err)
		return nil, err
	}
	defer rows.Close()

	var accounts []AccountInfo
	for rows.Next() {
		var a AccountInfo
		var isDef int
		if err := rows.Scan(&a.UserID, &a.Nickname, &a.Remark, &a.APIToken, &isDef, &a.DefaultModel, &a.CreatedAt, &a.CredentialValid, &a.CredentialRefreshAt, &a.DisplayOrder); err != nil {
			slog.Error("store: list accounts scan failed", "error", err)
			return nil, err
		}
		a.IsDefault = isDef == 1
		a.CredentialCheckedAt = a.CredentialRefreshAt
		accounts = append(accounts, a)
	}
	return accounts, rows.Err()
}

// FillAccountStats populates request/token statistics for each account using batch queries.
func (s *Store) FillAccountStats(accounts []AccountInfo) {
	if len(accounts) == 0 {
		return
	}

	// Single query with conditional aggregation for both all-time and today.
	rows, err := s.db.Query(`
		SELECT api_key,
			COUNT(*) as total_req,
			COALESCE(SUM(input_tokens + output_tokens), 0) as total_tokens,
			SUM(CASE WHEN date(created_at) = date('now', 'localtime') THEN 1 ELSE 0 END) as today_req,
			COALESCE(SUM(CASE WHEN date(created_at) = date('now', 'localtime') THEN input_tokens + output_tokens ELSE 0 END), 0) as today_tokens
		FROM request_logs
		GROUP BY api_key`)
	if err != nil {
		slog.Warn("store: fill account stats query failed", "error", err)
		return
	}
	defer rows.Close()

	type stats struct{ totalReq, totalTok, todayReq, todayTok int }
	m := make(map[string]stats)
	for rows.Next() {
		var key string
		var st stats
		if err := rows.Scan(&key, &st.totalReq, &st.totalTok, &st.todayReq, &st.todayTok); err == nil {
			m[key] = st
		}
	}

	for i := range accounts {
		if v, ok := m[accounts[i].UserID]; ok {
			accounts[i].TotalRequests = v.totalReq
			accounts[i].TotalTokens = v.totalTok
			accounts[i].TodayRequests = v.todayReq
			accounts[i].TodayTokens = v.todayTok
		}
	}
}

func (s *Store) GetAccount(userID string) (*Account, error) {
	var a Account
	var encPtKey string
	var isDef int
	err := s.db.QueryRow(
		"SELECT user_id, nickname, remark, api_token, pt_key, is_default, default_model, created_at FROM accounts WHERE user_id = ?",
		userID,
	).Scan(&a.UserID, &a.Nickname, &a.Remark, &a.APIToken, &encPtKey, &isDef, &a.DefaultModel, &a.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		slog.Error("store: get account query failed", "user_id", userID, "error", err)
		return nil, err
	}

	ptKey, err := s.decrypt(encPtKey)
	if err != nil {
		slog.Error("store: decrypt pt_key failed", "user_id", userID, "error", err)
		return nil, fmt.Errorf("decrypt pt_key: %w", err)
	}
	a.PtKey = ptKey
	a.IsDefault = isDef == 1
	return &a, nil
}

func (s *Store) GetAccountByToken(token string) (*Account, error) {
	var a Account
	var encPtKey string
	var isDef int
	err := s.db.QueryRow(
		"SELECT user_id, nickname, remark, api_token, pt_key, is_default, default_model, created_at FROM accounts WHERE api_token = ?",
		token,
	).Scan(&a.UserID, &a.Nickname, &a.Remark, &a.APIToken, &encPtKey, &isDef, &a.DefaultModel, &a.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		slog.Error("store: get account by token query failed", "error", err)
		return nil, err
	}

	ptKey, err := s.decrypt(encPtKey)
	if err != nil {
		slog.Error("store: decrypt pt_key by token failed", "error", err)
		return nil, fmt.Errorf("decrypt pt_key: %w", err)
	}
	a.PtKey = ptKey
	a.IsDefault = isDef == 1
	return &a, nil
}

func (s *Store) RenewToken(userID string) (string, error) {
	token := generateToken()
	_, err := s.db.Exec("UPDATE accounts SET api_token = ?, updated_at = datetime('now', 'localtime') WHERE user_id = ?", token, userID)
	if err != nil {
		slog.Error("store: renew token failed", "user_id", userID, "error", err)
		return "", err
	}
	return token, nil
}

func (s *Store) GetDefaultAccount() (*Account, error) {
	var a Account
	var encPtKey string
	err := s.db.QueryRow(
		"SELECT user_id, nickname, remark, api_token, pt_key, is_default, default_model, created_at FROM accounts WHERE is_default = 1 LIMIT 1",
	).Scan(&a.UserID, &a.Nickname, &a.Remark, &a.APIToken, &encPtKey, new(int), &a.DefaultModel, &a.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		slog.Error("store: get default account query failed", "error", err)
		return nil, err
	}

	ptKey, err := s.decrypt(encPtKey)
	if err != nil {
		slog.Error("store: decrypt default account pt_key failed", "error", err)
		return nil, fmt.Errorf("decrypt pt_key: %w", err)
	}
	a.PtKey = ptKey
	a.IsDefault = true
	return &a, nil
}

func (s *Store) ReorderAccounts(userIDs []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	for i, uid := range userIDs {
		if _, err := tx.Exec("UPDATE accounts SET display_order = ? WHERE user_id = ?", i+1, uid); err != nil {
			return fmt.Errorf("update display_order for %s: %w", uid, err)
		}
	}
	return tx.Commit()
}

func (s *Store) RemoveAccount(userID string) error {
	_, err := s.db.Exec("DELETE FROM accounts WHERE user_id = ?", userID)
	if err != nil {
		slog.Error("store: remove account failed", "user_id", userID, "error", err)
	}
	return err
}

func (s *Store) ClearAllAccounts() (int, error) {
	result, err := s.db.Exec("DELETE FROM accounts")
	if err != nil {
		slog.Error("store: clear all accounts failed", "error", err)
		return 0, err
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}

// UpdatePtKey updates the encrypted pt_key for an account.
func (s *Store) UpdatePtKey(userID, ptKey string) error {
	encPtKey, err := s.encrypt(ptKey)
	if err != nil {
		slog.Error("store: encrypt pt_key for update failed", "user_id", userID, "error", err)
		return fmt.Errorf("encrypt pt_key: %w", err)
	}
	result, err := s.db.Exec(
		"UPDATE accounts SET pt_key = ?, updated_at = datetime('now', 'localtime'), credential_refreshed_at = datetime('now', 'localtime') WHERE user_id = ?",
		encPtKey, userID,
	)
	if err != nil {
		slog.Error("store: update pt_key failed", "user_id", userID, "error", err)
		return err
	}
	rows, _ := result.RowsAffected()
	slog.Info("store: pt_key updated",
		"user_id", userID,
		"rows_affected", rows,
	)
	return nil
}

// UpdateCredentialRefreshedAt sets credential_refreshed_at to now for an account
// that was validated but did not need a pt_key refresh.
func (s *Store) UpdateCredentialRefreshedAt(userID string) {
	s.db.Exec(
		"UPDATE accounts SET credential_refreshed_at = datetime('now', 'localtime') WHERE user_id = ?",
		userID,
	)
}

// SetCredentialValid updates the credential_valid status for an account.
func (s *Store) SetCredentialValid(userID string, valid bool) {
	v := 0
	if valid {
		v = 1
	}
	s.db.Exec("UPDATE accounts SET credential_valid = ? WHERE user_id = ?", v, userID)
}

// ListStaleAccounts returns accounts that need credential checking.
// Valid accounts (credential_valid=1) use the normal threshold.
// Failed accounts (credential_valid=0) use a 4x longer backoff threshold.
// Unknown accounts (credential_valid=-1) or never-refreshed are always included.
func (s *Store) ListStaleAccounts(threshold time.Duration) ([]Account, error) {
	normalCutoff := time.Now().Add(-threshold).Format("2006-01-02 15:04:05")
	backoffCutoff := time.Now().Add(-threshold * 4).Format("2006-01-02 15:04:05")
	rows, err := s.db.Query(
		`SELECT user_id, nickname, pt_key, default_model FROM accounts
		 WHERE credential_refreshed_at = ''
		    OR credential_valid = -1
		    OR (credential_valid = 1 AND credential_refreshed_at < ?)
		    OR (credential_valid = 0 AND credential_refreshed_at < ?)
		 ORDER BY created_at`,
		normalCutoff, backoffCutoff,
	)
	if err != nil {
		slog.Error("store: list stale accounts query failed", "error", err)
		return nil, err
	}
	defer rows.Close()

	var accounts []Account
	for rows.Next() {
		var a Account
		var encPtKey string
		if err := rows.Scan(&a.UserID, &a.Nickname, &encPtKey, &a.DefaultModel); err != nil {
			slog.Error("store: list stale accounts scan failed", "error", err)
			return nil, err
		}
		ptKey, err := s.decrypt(encPtKey)
		if err != nil {
			slog.Error("store: decrypt pt_key failed for stale account", "user_id", a.UserID, "error", err)
			continue
		}
		a.PtKey = ptKey
		accounts = append(accounts, a)
	}
	return accounts, nil
}

// ListAllAccountsWithCredentials returns all accounts with decrypted pt_keys.
func (s *Store) ListAllAccountsWithCredentials() ([]Account, error) {
	rows, err := s.db.Query("SELECT user_id, nickname, pt_key, default_model FROM accounts ORDER BY created_at")
	if err != nil {
		slog.Error("store: list accounts with credentials query failed", "error", err)
		return nil, err
	}
	defer rows.Close()

	var accounts []Account
	for rows.Next() {
		var a Account
		var encPtKey string
		if err := rows.Scan(&a.UserID, &a.Nickname, &encPtKey, &a.DefaultModel); err != nil {
			slog.Error("store: list accounts with credentials scan failed", "error", err)
			return nil, err
		}
		ptKey, err := s.decrypt(encPtKey)
		if err != nil {
			slog.Error("store: decrypt pt_key failed for keepalive", "user_id", a.UserID, "error", err)
			continue
		}
		a.PtKey = ptKey
		accounts = append(accounts, a)
	}
	return accounts, rows.Err()
}

// UpdateRemark updates the remark for an account.
func (s *Store) UpdateRemark(userID, remark string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec("UPDATE accounts SET remark = ?, updated_at = datetime('now', 'localtime') WHERE user_id = ?", remark, userID)
	if err != nil {
		slog.Error("store: update remark failed", "user_id", userID, "error", err)
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("account %q not found", userID)
	}
	return nil
}

func (s *Store) SetDefault(userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		slog.Error("store: set default begin tx failed", "user_id", userID, "error", err)
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("UPDATE accounts SET is_default = 0, updated_at = datetime('now', 'localtime')"); err != nil {
		slog.Error("store: set default clear failed", "error", err)
		return err
	}
	if _, err := tx.Exec("UPDATE accounts SET is_default = 1, updated_at = datetime('now', 'localtime') WHERE user_id = ?", userID); err != nil {
		slog.Error("store: set default assign failed", "user_id", userID, "error", err)
		return err
	}
	return tx.Commit()
}

func (s *Store) UpdateAccountModel(userID, model string) error {
	_, err := s.db.Exec(
		"UPDATE accounts SET default_model = ?, updated_at = datetime('now', 'localtime') WHERE user_id = ?",
		model, userID,
	)
	if err != nil {
		slog.Error("store: update account model failed", "user_id", userID, "model", model, "error", err)
	}
	return err
}

// --- Settings ---

func (s *Store) GetSettings() (map[string]string, error) {
	rows, err := s.db.Query("SELECT key, value FROM settings")
	if err != nil {
		slog.Error("store: get settings query failed", "error", err)
		return nil, err
	}
	defer rows.Close()

	m := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			slog.Error("store: get settings scan failed", "error", err)
			return nil, err
		}
		m[k] = v
	}
	return m, rows.Err()
}

func (s *Store) GetSetting(key string) string {
	var val string
	err := s.db.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&val)
	if err != nil && err != sql.ErrNoRows {
		slog.Error("store: get setting failed", "key", key, "error", err)
	}
	return val
}

func (s *Store) GetIntSetting(key string, defaultVal int) int {
	v := s.GetSetting(key)
	if v == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return defaultVal
	}
	return n
}

func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(
		"INSERT OR REPLACE INTO settings (key, value, updated_at) VALUES (?, ?, datetime('now', 'localtime'))",
		key, value,
	)
	if err != nil {
		slog.Error("store: set setting failed", "key", key, "error", err)
	}
	return err
}

func (s *Store) SetSettings(settings map[string]string) error {
	tx, err := s.db.Begin()
	if err != nil {
		slog.Error("store: set settings begin tx failed", "error", err)
		return err
	}
	defer tx.Rollback()

	for k, v := range settings {
		if _, err := tx.Exec(
			"INSERT OR REPLACE INTO settings (key, value, updated_at) VALUES (?, ?, datetime('now', 'localtime'))",
			k, v,
		); err != nil {
			slog.Error("store: set settings exec failed", "key", k, "error", err)
			return err
		}
	}
	return tx.Commit()
}

// --- Request Logging ---

func (s *Store) LogRequest(userID, model, endpoint string, stream bool, statusCode int, latencyMs int64, errMsg string, inputTokens, outputTokens int) error {
	sInt := 0
	if stream {
		sInt = 1
	}
	_, err := s.db.Exec(
		"INSERT INTO request_logs (api_key, model, endpoint, stream, status_code, latency_ms, error_message, input_tokens, output_tokens) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		userID, model, endpoint, sInt, statusCode, latencyMs, errMsg, inputTokens, outputTokens,
	)
	if err != nil {
		slog.Error("store: log request failed", "user_id", userID, "endpoint", endpoint, "error", err)
	}
	return err
}

func (s *Store) GetStats() (*Stats, error) {
	stats := &Stats{}
	// created_at is stored as local time (DEFAULT datetime('now','localtime')),
	// so compare its date directly against today's local date — applying
	// 'localtime' to created_at would convert it a second time and drop rows
	// near the day boundary for non-UTC servers.
	tf := "date(created_at) = date('now', 'localtime')"

	// Single aggregate query for all per-day totals (replaces 7 round-trips).
	err := s.db.QueryRow(`
		SELECT
			COUNT(*),
			COALESCE(AVG(latency_ms), 0),
			COALESCE(SUM(CASE WHEN status_code >= 400 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN stream = 1 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status_code < 400 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(output_tokens), 0)
		FROM request_logs WHERE `+tf).Scan(
		&stats.TotalRequests, &stats.AvgLatencyMs, &stats.ErrorCount,
		&stats.StreamCount, &stats.SuccessCount,
		&stats.TotalInputTk, &stats.TotalOutputTk,
	)
	if err != nil {
		slog.Error("store: get stats aggregate failed", "error", err)
		return nil, err
	}

	if err := s.db.QueryRow("SELECT COUNT(*) FROM accounts").Scan(&stats.AccountsCount); err != nil {
		slog.Error("store: get accounts count failed", "error", err)
		return nil, err
	}

	rows, err := s.db.Query("SELECT model, COUNT(*) as cnt FROM request_logs WHERE "+tf+" AND model != '' GROUP BY model ORDER BY cnt DESC")
	if err != nil {
		slog.Error("store: get stats by model query failed", "error", err)
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var mc ModelCount
		if err := rows.Scan(&mc.Model, &mc.Count); err != nil {
			return nil, err
		}
		stats.ByModel = append(stats.ByModel, mc)
	}

	validKeys := make(map[string]bool)
	accounts, _ := s.ListAccounts()
	for _, a := range accounts {
		validKeys[a.UserID] = true
	}

	rows2, err := s.db.Query("SELECT api_key, COUNT(*) as cnt FROM request_logs WHERE "+tf+" GROUP BY api_key ORDER BY cnt DESC")
	if err != nil {
		slog.Error("store: get stats by account query failed", "error", err)
		return nil, err
	}
	defer rows2.Close()
	otherCount := 0
	for rows2.Next() {
		var ac AccountCount
		if err := rows2.Scan(&ac.UserID, &ac.Count); err != nil {
			return nil, err
		}
		if validKeys[ac.UserID] {
			stats.ByAccount = append(stats.ByAccount, ac)
		} else {
			otherCount += ac.Count
		}
	}
	if otherCount > 0 {
		stats.ByAccount = append(stats.ByAccount, AccountCount{UserID: "其他", Count: otherCount})
	}

	return stats, nil
}

func (s *Store) GetAllTimeTotals() (*AllTimeTotals, error) {
	t := &AllTimeTotals{}
	err := s.db.QueryRow(`
		SELECT
			COUNT(*),
			COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(CASE WHEN status_code >= 400 THEN 1 ELSE 0 END), 0)
		FROM request_logs`).Scan(&t.TotalRequests, &t.TotalInputTk, &t.TotalOutputTk, &t.ErrorCount)
	if err != nil {
		slog.Error("store: get all-time totals failed", "error", err)
		return nil, err
	}
	return t, nil
}

func (s *Store) GetHourlyStats() ([]HourlyData, error) {
	rows, err := s.db.Query(`
		SELECT strftime('%m-%d %H', created_at) as hour,
			COUNT(*) as count,
			COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(output_tokens), 0),
			SUM(CASE WHEN status_code >= 400 THEN 1 ELSE 0 END)
		FROM request_logs
		WHERE created_at >= datetime('now', 'localtime', '-24 hours')
		GROUP BY hour ORDER BY hour`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []HourlyData
	for rows.Next() {
		var h HourlyData
		if err := rows.Scan(&h.Hour, &h.Count, &h.InputTokens, &h.OutputTokens, &h.Errors); err != nil {
			return nil, err
		}
		result = append(result, h)
	}
	return result, rows.Err()
}

func (s *Store) GetAccountStats(userID string) (*AccountStats, error) {
	as := &AccountStats{UserID: userID}
	tf := "created_at >= datetime('now', 'localtime', '-24 hours')"

	err := s.db.QueryRow(`
		SELECT
			COUNT(*),
			COALESCE(AVG(latency_ms), 0),
			COALESCE(SUM(CASE WHEN stream = 1 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status_code >= 400 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status_code < 400 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(output_tokens), 0)
		FROM request_logs WHERE api_key = ? AND `+tf, userID).Scan(
		&as.TotalRequests, &as.AvgLatencyMs, &as.StreamCount,
		&as.ErrorCount, &as.SuccessCount,
		&as.TotalInputTk, &as.TotalOutputTk,
	)
	if err != nil {
		slog.Error("store: get account stats failed", "user_id", userID, "error", err)
		return nil, err
	}

	rows, err := s.db.Query("SELECT model, COUNT(*) as cnt FROM request_logs WHERE api_key = ? AND "+tf+" GROUP BY model ORDER BY cnt DESC", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var mc ModelCount
		if err := rows.Scan(&mc.Model, &mc.Count); err != nil {
			return nil, err
		}
		as.ByModel = append(as.ByModel, mc)
	}

	rows2, err := s.db.Query("SELECT endpoint, COUNT(*) as cnt FROM request_logs WHERE api_key = ? AND "+tf+" GROUP BY endpoint ORDER BY cnt DESC", userID)
	if err != nil {
		return nil, err
	}
	defer rows2.Close()
	for rows2.Next() {
		var ec EndpointCount
		if err := rows2.Scan(&ec.Endpoint, &ec.Count); err != nil {
			return nil, err
		}
		as.ByEndpoint = append(as.ByEndpoint, ec)
	}

	// All-time totals
	allTime := &AllTimeTotals{}
	s.db.QueryRow("SELECT COUNT(*) FROM request_logs WHERE api_key = ?", userID).Scan(&allTime.TotalRequests)
	s.db.QueryRow("SELECT COALESCE(SUM(input_tokens), 0) FROM request_logs WHERE api_key = ?", userID).Scan(&allTime.TotalInputTk)
	s.db.QueryRow("SELECT COALESCE(SUM(output_tokens), 0) FROM request_logs WHERE api_key = ?", userID).Scan(&allTime.TotalOutputTk)
	s.db.QueryRow("SELECT COUNT(*) FROM request_logs WHERE api_key = ? AND status_code >= 400", userID).Scan(&allTime.ErrorCount)
	as.AllTime = allTime

	// Hourly breakdown for last 24 hours
	hRows, err := s.db.Query(`
		SELECT strftime('%m-%d %H', created_at) as hour,
			COUNT(*) as count,
			COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(output_tokens), 0),
			SUM(CASE WHEN status_code >= 400 THEN 1 ELSE 0 END)
		FROM request_logs
		WHERE api_key = ? AND `+tf+`
		GROUP BY hour ORDER BY hour`, userID)
	if err == nil {
		defer hRows.Close()
		for hRows.Next() {
			var h HourlyData
			if hRows.Scan(&h.Hour, &h.Count, &h.InputTokens, &h.OutputTokens, &h.Errors) == nil {
				as.Hourly = append(as.Hourly, h)
			}
		}
	}

	return as, nil
}

func (s *Store) GetAccountLogs(userID string, limit int) ([]RequestLog, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.Query(
		"SELECT id, api_key, model, endpoint, stream, status_code, latency_ms, COALESCE(error_message, ''), COALESCE(input_tokens, 0), COALESCE(output_tokens, 0), created_at FROM request_logs WHERE api_key = ? ORDER BY id DESC LIMIT ?",
		userID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []RequestLog
	for rows.Next() {
		var l RequestLog
		var streamInt int
		if err := rows.Scan(&l.ID, &l.UserID, &l.Model, &l.Endpoint, &streamInt, &l.StatusCode, &l.LatencyMs, &l.ErrorMessage, &l.InputTokens, &l.OutputTokens, &l.CreatedAt); err != nil {
			return nil, err
		}
		l.Stream = streamInt == 1
		logs = append(logs, l)
	}
	return logs, rows.Err()
}

func (s *Store) GetRecentLogs(limit int) ([]RequestLog, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.Query(
		"SELECT id, api_key, model, endpoint, stream, status_code, latency_ms, COALESCE(error_message, ''), COALESCE(input_tokens, 0), COALESCE(output_tokens, 0), created_at FROM request_logs ORDER BY id DESC LIMIT ?",
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []RequestLog
	for rows.Next() {
		var l RequestLog
		var streamInt int
		if err := rows.Scan(&l.ID, &l.UserID, &l.Model, &l.Endpoint, &streamInt, &l.StatusCode, &l.LatencyMs, &l.ErrorMessage, &l.InputTokens, &l.OutputTokens, &l.CreatedAt); err != nil {
			return nil, err
		}
		l.Stream = streamInt == 1
		logs = append(logs, l)
	}
	return logs, rows.Err()
}


// GetRecentErrors returns request logs with status_code >= 400.
func (s *Store) GetRecentErrors(limit int) ([]RequestLog, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(
		"SELECT id, api_key, model, endpoint, stream, status_code, latency_ms, COALESCE(error_message, ''), COALESCE(input_tokens, 0), COALESCE(output_tokens, 0), created_at FROM request_logs WHERE status_code >= 400 ORDER BY id DESC LIMIT ?",
		limit,
	)
	if err != nil {
		slog.Error("store: get recent errors query failed", "error", err)
		return nil, err
	}
	defer rows.Close()

	var logs []RequestLog
	for rows.Next() {
		var l RequestLog
		var streamInt int
		if err := rows.Scan(&l.ID, &l.UserID, &l.Model, &l.Endpoint, &streamInt, &l.StatusCode, &l.LatencyMs, &l.ErrorMessage, &l.InputTokens, &l.OutputTokens, &l.CreatedAt); err != nil {
			slog.Error("store: get recent errors scan failed", "error", err)
			return nil, err
		}
		l.Stream = streamInt == 1
		logs = append(logs, l)
	}
	if err := rows.Err(); err != nil {
		slog.Error("store: get recent errors iteration failed", "error", err)
		return nil, err
	}
	return logs, nil
}
// CleanupOldLogs deletes request logs older than the specified number of days.
func (s *Store) CleanupOldLogs(days int) (int64, error) {
	if days <= 0 {
		return 0, nil
	}
	result, err := s.db.Exec(
		"DELETE FROM request_logs WHERE created_at < datetime('now', 'localtime', '-' || ? || ' days')",
		days,
	)
	if err != nil {
		slog.Error("store: cleanup old logs failed", "days", days, "error", err)
		return 0, err
	}
	affected, _ := result.RowsAffected()
	if affected > 0 {
		slog.Info("store: cleaned up old logs", "days", days, "deleted", affected)
	}
	return affected, nil
}

// MigrateTokenLogs reassigns request_logs stored under api_token values to the account's user_id.
func (s *Store) MigrateTokenLogs() (int64, error) {
	result, err := s.db.Exec(`
		UPDATE request_logs SET api_key = (
			SELECT a.user_id FROM accounts a WHERE a.api_token = request_logs.api_key
		) WHERE api_key LIKE 'sk-joy-%' AND EXISTS (
			SELECT 1 FROM accounts a WHERE a.api_token = request_logs.api_key
		)`)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// ReassignLogs maps old api_key values in request_logs to a new api_key.
func (s *Store) ReassignLogs(oldKeys []string, newKey string) (int64, error) {
	ph := "?"
	for i := 1; i < len(oldKeys); i++ {
		ph += ",?"
	}
	args := make([]interface{}, len(oldKeys)+1)
	args[0] = newKey
	for i, k := range oldKeys {
		args[i+1] = k
	}
	result, err := s.db.Exec("UPDATE request_logs SET api_key = ? WHERE api_key IN ("+ph+")", args...)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// EnsureDataDir ensures the data directory exists with correct permissions.
func EnsureDataDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, DefaultDBDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return dir, nil
}

// ExportAccountItem is the format for account export/import.
type ExportAccountItem struct {
	UserID       string `json:"user_id"`
	Nickname     string `json:"nickname"`
	Remark       string `json:"remark"`
	PtKey        string `json:"pt_key"`
	IsDefault    bool   `json:"is_default"`
	DefaultModel string `json:"default_model"`
	DisplayOrder int    `json:"display_order"`
}

// ExportAccounts returns all accounts with decrypted pt_keys for export.
func (s *Store) ExportAccounts() ([]ExportAccountItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(
		"SELECT user_id, nickname, remark, pt_key, is_default, default_model, COALESCE(display_order, 0) FROM accounts ORDER BY display_order, created_at",
	)
	if err != nil {
		return nil, fmt.Errorf("query accounts for export: %w", err)
	}
	defer rows.Close()

	var items []ExportAccountItem
	for rows.Next() {
		var item ExportAccountItem
		var encPtKey string
		var isDef int
		if err := rows.Scan(&item.UserID, &item.Nickname, &item.Remark, &encPtKey, &isDef, &item.DefaultModel, &item.DisplayOrder); err != nil {
			return nil, fmt.Errorf("scan account for export: %w", err)
		}
		ptKey, err := s.decrypt(encPtKey)
		if err != nil {
			slog.Warn("store: skip account in export, decrypt failed", "user_id", item.UserID, "error", err)
			continue
		}
		item.PtKey = ptKey
		item.IsDefault = isDef == 1
		items = append(items, item)
	}
	if items == nil {
		items = []ExportAccountItem{}
	}
	return items, nil
}

// ImportAccounts imports accounts from export data. Existing accounts are updated (pt_key only).
func (s *Store) ImportAccounts(items []ExportAccountItem) (added int, updated int, err error) {
	for _, item := range items {
		if item.UserID == "" || item.PtKey == "" {
			continue
		}
		var existing int
		s.mu.Lock()
		e := s.db.QueryRow("SELECT COUNT(*) FROM accounts WHERE user_id = ?", item.UserID).Scan(&existing)
		s.mu.Unlock()
		if e != nil {
			return added, updated, fmt.Errorf("check existing account %s: %w", item.UserID, e)
		}
		if err := s.AddAccount(item.UserID, item.PtKey, item.Nickname, item.IsDefault, item.DefaultModel); err != nil {
			return added, updated, fmt.Errorf("import account %s: %w", item.UserID, err)
		}
		if existing > 0 {
			updated++
		} else {
			added++
		}
	}
	return added, updated, nil
}

// Copy from os.ReadFile pattern -- used to check if DB exists.
func DBExists() bool {
	path, err := DefaultDBPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

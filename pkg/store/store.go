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
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
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

	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
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
			credential_valid INTEGER DEFAULT -1
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
	s.db.Exec("ALTER TABLE request_logs ADD COLUMN error_message TEXT DEFAULT ''")

	// Migration: add token columns to request_logs
	s.db.Exec("ALTER TABLE request_logs ADD COLUMN input_tokens INTEGER DEFAULT 0")
	s.db.Exec("ALTER TABLE request_logs ADD COLUMN output_tokens INTEGER DEFAULT 0")

	// Migration: add display_order column to accounts
	s.db.Exec("ALTER TABLE accounts ADD COLUMN display_order INTEGER DEFAULT 0")

	// Migration: migrate old schema (api_key as PK) to new schema (user_id as PK)
	s.migrateUserIDAsPK()

	// Migration: fix historical UTC timestamps to localtime
	s.migrateUTCTimestamps()

	// Migration: initialize display_order for existing accounts
	s.migrateDisplayOrder()

	return nil
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
			credential_valid INTEGER DEFAULT -1
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
		INSERT INTO accounts_new (user_id, nickname, api_token, pt_key, is_default, default_model, created_at, updated_at, credential_refreshed_at, credential_valid)
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
			COALESCE(credential_valid, -1)
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
// Uses SQLite to check if the newest record's time is behind local now by more than
// 30 minutes -- if so, the data was stored in UTC and needs +offset hours.
func (s *Store) migrateUTCTimestamps() {
	_, offset := time.Now().Zone()
	hours := offset / 3600
	if hours <= 0 {
		return
	}
	// Check if the newest request_log is behind localtime by roughly the offset
	var count int
	s.db.QueryRow(fmt.Sprintf(
		"SELECT COUNT(*) FROM request_logs WHERE created_at < datetime('now', 'localtime') - INTERVAL IS NOT SUPPORTED AND created_at < datetime('now', 'localtime', '-30 minutes')",
	)).Scan(&count)
	if count == 0 {
		return
	}
	s.db.Exec(fmt.Sprintf("UPDATE request_logs SET created_at = datetime(created_at, '+%d hours') WHERE created_at < datetime('now', 'localtime', '-30 minutes')", hours))
	s.db.Exec(fmt.Sprintf("UPDATE accounts SET created_at = datetime(created_at, '+%d hours') WHERE created_at < datetime('now', 'localtime', '-30 minutes')", hours))
	s.db.Exec(fmt.Sprintf("UPDATE settings SET updated_at = datetime(updated_at, '+%d hours') WHERE updated_at < datetime('now', 'localtime', '-30 minutes')", hours))
	slog.Info("migrated UTC timestamps to localtime", "offset_hours", hours, "records_fixed", count)
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

func (s *Store) AddAccount(userID, ptKey, nickname string, isDefault bool, defaultModel string) error {
	if userID == "" {
		return fmt.Errorf("user_id cannot be empty")
	}
	if ptKey == "" {
		return fmt.Errorf("pt_key cannot be empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	encPtKey, err := s.encrypt(ptKey)
	if err != nil {
		slog.Error("store: encrypt pt_key failed", "user_id", userID, "error", err)
		return fmt.Errorf("encrypt pt_key: %w", err)
	}

	// Check if account already exists by user_id
	var existingToken string
	err = s.db.QueryRow(
		"SELECT api_token FROM accounts WHERE user_id = ?", userID,
	).Scan(&existingToken)
	if err == nil {
		// Account exists: only update pt_key; preserve nickname, remark, display_order, etc.
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

	// All-time stats: single GROUP BY query
	allRows, err := s.db.Query(`
		SELECT api_key,
			COUNT(*) as req_count,
			COALESCE(SUM(input_tokens + output_tokens), 0) as token_sum
		FROM request_logs
		GROUP BY api_key`)
	if err != nil {
		return
	}
	allMap := make(map[string][2]int)
	for allRows.Next() {
		var key string
		var reqCount, tokenSum int
		if allRows.Scan(&key, &reqCount, &tokenSum) == nil {
			allMap[key] = [2]int{reqCount, tokenSum}
		}
	}
	allRows.Close()

	// Today stats: single GROUP BY query
	todayRows, err := s.db.Query(`
		SELECT api_key,
			COUNT(*) as req_count,
			COALESCE(SUM(input_tokens + output_tokens), 0) as token_sum
		FROM request_logs
		WHERE date(created_at, 'localtime') = date('now', 'localtime')
		GROUP BY api_key`)
	if err != nil {
		return
	}
	todayMap := make(map[string][2]int)
	for todayRows.Next() {
		var key string
		var reqCount, tokenSum int
		if todayRows.Scan(&key, &reqCount, &tokenSum) == nil {
			todayMap[key] = [2]int{reqCount, tokenSum}
		}
	}
	todayRows.Close()

	for i := range accounts {
		if v, ok := allMap[accounts[i].UserID]; ok {
			accounts[i].TotalRequests = v[0]
			accounts[i].TotalTokens = v[1]
		}
		if v, ok := todayMap[accounts[i].UserID]; ok {
			accounts[i].TodayRequests = v[0]
			accounts[i].TodayTokens = v[1]
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
	s.db.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&val)
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
	tf := "date(created_at, 'localtime') = date('now', 'localtime')"

	err := s.db.QueryRow("SELECT COUNT(*) FROM request_logs WHERE "+tf).Scan(&stats.TotalRequests)
	if err != nil {
		slog.Error("store: get stats count failed", "error", err)
		return nil, err
	}

	s.db.QueryRow("SELECT COALESCE(AVG(latency_ms), 0) FROM request_logs WHERE "+tf).Scan(&stats.AvgLatencyMs)
	s.db.QueryRow("SELECT COUNT(*) FROM accounts").Scan(&stats.AccountsCount)
	s.db.QueryRow("SELECT COUNT(*) FROM request_logs WHERE "+tf+" AND status_code >= 400").Scan(&stats.ErrorCount)
	s.db.QueryRow("SELECT COUNT(*) FROM request_logs WHERE "+tf+" AND stream = 1").Scan(&stats.StreamCount)
	s.db.QueryRow("SELECT COUNT(*) FROM request_logs WHERE "+tf+" AND status_code < 400").Scan(&stats.SuccessCount)
	s.db.QueryRow("SELECT COALESCE(SUM(input_tokens), 0) FROM request_logs WHERE "+tf).Scan(&stats.TotalInputTk)
	s.db.QueryRow("SELECT COALESCE(SUM(output_tokens), 0) FROM request_logs WHERE "+tf).Scan(&stats.TotalOutputTk)

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
	s.db.QueryRow("SELECT COUNT(*) FROM request_logs").Scan(&t.TotalRequests)
	s.db.QueryRow("SELECT COALESCE(SUM(input_tokens), 0) FROM request_logs").Scan(&t.TotalInputTk)
	s.db.QueryRow("SELECT COALESCE(SUM(output_tokens), 0) FROM request_logs").Scan(&t.TotalOutputTk)
	s.db.QueryRow("SELECT COUNT(*) FROM request_logs WHERE status_code >= 400").Scan(&t.ErrorCount)
	return t, nil
}

func (s *Store) GetHourlyStats() ([]HourlyData, error) {
	rows, err := s.db.Query(`
		SELECT strftime('%m-%d %H', created_at, 'localtime') as hour,
			COUNT(*) as count,
			COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(output_tokens), 0),
			SUM(CASE WHEN status_code >= 400 THEN 1 ELSE 0 END)
		FROM request_logs
		WHERE created_at >= datetime('now', '-24 hours')
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
	tf := "created_at >= datetime('now', '-24 hours')"

	s.db.QueryRow("SELECT COUNT(*) FROM request_logs WHERE api_key = ? AND "+tf, userID).Scan(&as.TotalRequests)
	s.db.QueryRow("SELECT COALESCE(AVG(latency_ms), 0) FROM request_logs WHERE api_key = ? AND "+tf, userID).Scan(&as.AvgLatencyMs)
	s.db.QueryRow("SELECT COUNT(*) FROM request_logs WHERE api_key = ? AND stream = 1 AND "+tf, userID).Scan(&as.StreamCount)
	s.db.QueryRow("SELECT COUNT(*) FROM request_logs WHERE api_key = ? AND status_code >= 400 AND "+tf, userID).Scan(&as.ErrorCount)
	s.db.QueryRow("SELECT COUNT(*) FROM request_logs WHERE api_key = ? AND status_code < 400 AND "+tf, userID).Scan(&as.SuccessCount)
	s.db.QueryRow("SELECT COALESCE(SUM(input_tokens), 0) FROM request_logs WHERE api_key = ? AND "+tf, userID).Scan(&as.TotalInputTk)
	s.db.QueryRow("SELECT COALESCE(SUM(output_tokens), 0) FROM request_logs WHERE api_key = ? AND "+tf, userID).Scan(&as.TotalOutputTk)

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
		SELECT strftime('%m-%d %H', created_at, 'localtime') as hour,
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
		"DELETE FROM request_logs WHERE created_at < datetime('now', '-' || ? || ' days')",
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

// Copy from os.ReadFile pattern -- used to check if DB exists.
func DBExists() bool {
	path, err := DefaultDBPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

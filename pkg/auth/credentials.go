package auth

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	_ "modernc.org/sqlite"
)

// Credentials holds JoyCode authentication data.
type Credentials struct {
	PtKey         string
	UserID        string
	ColorBaseURL  string
	MasterBaseURL string
	Tenant        string
	LoginType     string
	OrgFullName   string
}

type stateData struct {
	JoyCoderUser struct {
		PtKey         string `json:"ptKey"`
		UserID        string `json:"userId"`
		ColorBaseURL  string `json:"colorBaseUrl"`
		MasterBaseURL string `json:"masterBaseUrl"`
		Tenant        string `json:"tenant"`
		LoginType     string `json:"loginType"`
		OrgFullName   string `json:"orgFullName"`
	} `json:"joyCoderUser"`
}

const (
	stateDBEnv       = "JOYCODE_STATE_DB"
	containerStateDB = "/root/.joycode-ide/state.vscdb"
)

// LoadFromSystem reads ptKey from local JoyCode state database (macOS/Windows/Linux).
func LoadFromSystem() (*Credentials, error) {
	if dbPath := os.Getenv(stateDBEnv); dbPath != "" {
		return loadFromStateDB(dbPath)
	}
	if _, err := os.Stat(containerStateDB); err == nil {
		return loadFromStateDB(containerStateDB)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("cannot determine home directory: %w", err)
	}

	var dbPath string
	switch runtime.GOOS {
	case "darwin":
		dbPath = filepath.Join(home,
			"Library", "Application Support",
			"JoyCode", "User", "globalStorage", "state.vscdb")
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			appData = filepath.Join(home, "AppData", "Roaming")
		}
		dbPath = filepath.Join(appData,
			"JoyCode", "User", "globalStorage", "state.vscdb")
	case "linux":
		dbPath = filepath.Join(home,
			".config", "JoyCode", "User", "globalStorage", "state.vscdb")
	default:
		return nil, fmt.Errorf("auto credential extraction not supported on %s; set %s", runtime.GOOS, stateDBEnv)
	}

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("JoyCode state database not found at %s\n  Please install and log in to JoyCode IDE first", dbPath)
	}

	return loadFromStateDB(dbPath)
}

func loadFromStateDB(dbPath string) (*Credentials, error) {
	if _, err := os.Stat(dbPath); err != nil {
		return nil, fmt.Errorf("JoyCode state database not found at %s: %w", dbPath, err)
	}

	// modernc 不支持 mattn 的 ?mode=ro query；用 SQLite URI 以真正的只读共享锁
	// 打开 JoyCode IDE 的库，避免 IDE 运行时拿不到锁。
	db, err := sql.Open("sqlite", sqliteReadOnlyURI(dbPath))
	if err != nil {
		return nil, fmt.Errorf("cannot open JoyCode database: %w", err)
	}
	defer db.Close()

	var value string
	if err := db.QueryRow(
		"SELECT value FROM ItemTable WHERE key='JoyCoder.IDE'",
	).Scan(&value); err != nil {
		return nil, fmt.Errorf("login info not found in database\n  Please log in to JoyCode IDE first")
	}

	var data stateData
	if err := json.Unmarshal([]byte(value), &data); err != nil {
		return nil, fmt.Errorf("cannot parse login data from database: %w", err)
	}
	if data.JoyCoderUser.PtKey == "" {
		return nil, fmt.Errorf("ptKey is empty in stored credentials\n  Please re-login to JoyCode IDE")
	}
	if data.JoyCoderUser.UserID == "" {
		return nil, fmt.Errorf("userId is empty in stored credentials\n  Please re-login to JoyCode IDE")
	}
	return &Credentials{
		PtKey:         data.JoyCoderUser.PtKey,
		UserID:        data.JoyCoderUser.UserID,
		ColorBaseURL:  data.JoyCoderUser.ColorBaseURL,
		MasterBaseURL: data.JoyCoderUser.MasterBaseURL,
		Tenant:        data.JoyCoderUser.Tenant,
		LoginType:     data.JoyCoderUser.LoginType,
		OrgFullName:   data.JoyCoderUser.OrgFullName,
	}, nil
}

// sqliteReadOnlyURI builds a cross-platform SQLite URI with read-only mode.
// On Windows, absolute paths like C:\Users\... must become /C:/Users/... in URI form.
func sqliteReadOnlyURI(path string) string {
	p := filepath.ToSlash(path)
	if len(p) >= 2 && p[1] == ':' {
		p = "/" + p
	}
	return "file:" + p + "?mode=ro"
}

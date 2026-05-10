package auth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"

)

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

const (
	qrShowURL   = "https://qr.m.jd.com/show?appid=133&size=147&t=%d"
	qrCheckURL  = "https://qr.m.jd.com/check?appid=133&token=%s&callback=jsonpCallback&_=%d"
	qrValidURL  = "https://passport.jd.com/uc/qrCodeTicketValidation?t=%s"
	jdUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36"
)

// QRSession holds the state for an in-progress QR login session.
type QRSession struct {
	ID        string
	Token     string
	CreatedAt time.Time
	client    *http.Client
}

// QRLoginResult holds the result of a successful QR login.
type QRLoginResult struct {
	PtKey    string
	PtPin    string
	UserID   string
	RealName string
}

// QRVerifyNeededError indicates JD requires additional security verification.
type QRVerifyNeededError struct {
	RiskCode  int
	VerifyURL string
}

func (e *QRVerifyNeededError) Error() string {
	return fmt.Sprintf("JD 风控验证 (riskCode=%d)，请在浏览器中完成安全验证", e.RiskCode)
}

var (
	qrSessions   = make(map[string]*QRSession)
	qrSessionsMu sync.Mutex
)

// QRInit starts a new QR code login session. Returns session ID and QR code PNG base64.
func QRInit() (sessionID, qrImageBase64 string, err error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return "", "", fmt.Errorf("create cookie jar: %w", err)
	}
	client := &http.Client{Jar: jar, Timeout: 30 * time.Second}

	reqURL := fmt.Sprintf(qrShowURL, time.Now().UnixMilli())
	req, _ := http.NewRequest("GET", reqURL, nil)
	req.Header.Set("User-Agent", jdUserAgent)
	req.Header.Set("Referer", "https://passport.jd.com/new/login.aspx")
	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("request QR code: %w", err)
	}
	pngData, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return "", "", fmt.Errorf("read QR image: %w", err)
	}

	var token string
	for _, c := range client.Jar.Cookies(&url.URL{Scheme: "https", Host: "qr.m.jd.com"}) {
		if c.Name == "wlfstk_smdl" {
			token = c.Value
			break
		}
	}
	if token == "" {
		return "", "", fmt.Errorf("wlfstk_smdl cookie not found")
	}

	sessionID = fmt.Sprintf("qr_%d", time.Now().UnixNano())

	qrSessionsMu.Lock()
	qrSessions[sessionID] = &QRSession{
		ID: sessionID, Token: token,
		CreatedAt: time.Now(), client: client,
	}
	qrSessionsMu.Unlock()

	return sessionID, base64.StdEncoding.EncodeToString(pngData), nil
}

// QRPollStatus checks the scan status of a QR login session.
// Returns: status ("waiting"|"scanned"|"confirmed"|"expired"|"error"), result on success.
func QRPollStatus(sessionID string) (status string, result *QRLoginResult, err error) {
	qrSessionsMu.Lock()
	session, ok := qrSessions[sessionID]
	qrSessionsMu.Unlock()
	if !ok {
		return "expired", nil, fmt.Errorf("session not found")
	}
	if time.Since(session.CreatedAt) > 3*time.Minute {
		QRCleanup(sessionID)
		return "expired", nil, nil
	}

	reqURL := fmt.Sprintf(qrCheckURL, url.QueryEscape(session.Token), time.Now().UnixMilli())
	req, _ := http.NewRequest("GET", reqURL, nil)
	req.Header.Set("User-Agent", jdUserAgent)
	req.Header.Set("Referer", "https://passport.jd.com/new/login.aspx")
	resp, err := session.client.Do(req)
	if err != nil {
		slog.Error("qr-check request failed", "session", sessionID, "error", err)
		return "error", nil, err
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	str := string(body)
	start := strings.Index(str, "(")
	end := strings.LastIndex(str, ")")
	if start < 0 || end < 0 {
		slog.Warn("qr-check response not JSONP", "session", sessionID, "body", str[:minInt(len(str), 200)])
		return "waiting", nil, nil
	}

	var check struct {
		Code   int    `json:"code"`
		Ticket string `json:"ticket,omitempty"`
	}
	if err := json.Unmarshal([]byte(str[start+1:end]), &check); err != nil {
		slog.Warn("qr-check JSONP parse failed", "session", sessionID, "payload", str[start+1:end])
		return "waiting", nil, nil
	}

	slog.Debug("qr-check result", "session", sessionID, "code", check.Code, "has_ticket", check.Ticket != "")

	switch check.Code {
	case 200:
		if check.Ticket == "" {
			slog.Error("qr-check code 200 but empty ticket", "session", sessionID)
			return "error", nil, fmt.Errorf("ticket is empty")
		}
		loginResult, err := validateAndFetchInfo(session.client, check.Ticket)
		if err != nil {
			slog.Error("qr-validate failed", "session", sessionID, "error", err)
			return "error", nil, err
		}
		slog.Info("qr-login confirmed", "session", sessionID, "user_id", loginResult.UserID, "real_name", loginResult.RealName)
		QRCleanup(sessionID)
		return "confirmed", loginResult, nil
	case 201:
		return "waiting", nil, nil
	case 202:
		return "scanned", nil, nil
	case 203, 204:
		slog.Info("qr-code expired", "session", sessionID, "code", check.Code)
		QRCleanup(sessionID)
		return "expired", nil, nil
	case 205:
		slog.Info("qr-login canceled by user", "session", sessionID)
		QRCleanup(sessionID)
		return "expired", nil, nil
	case 257:
		slog.Error("qr-check parameter error", "session", sessionID, "msg", check.Ticket)
		QRCleanup(sessionID)
		return "error", nil, fmt.Errorf("JD 服务端参数异常 (code 257)")
	default:
		slog.Warn("qr-check unknown code", "session", sessionID, "code", check.Code)
		return "error", nil, fmt.Errorf("未知状态码: %d", check.Code)
	}
}

func extractPtKey(jar http.CookieJar) (ptKey, ptPin string) {
	for _, host := range []string{
		"www.jd.com", "passport.jd.com", "home.jd.com",
		"jd.com", "plogin.m.jd.com", "m.jd.com",
	} {
		for _, c := range jar.Cookies(&url.URL{Scheme: "https", Host: host}) {
			switch c.Name {
			case "pt_key":
				ptKey = c.Value
			case "pt_pin":
				ptPin = c.Value
			}
		}
	}
	return
}

func dumpAllCookies(jar http.CookieJar) {
	hosts := []string{
		"www.jd.com", "passport.jd.com", "home.jd.com",
		"jd.com", "plogin.m.jd.com", "m.jd.com",
		"qr.m.jd.com",
	}
	for _, host := range hosts {
		cookies := jar.Cookies(&url.URL{Scheme: "https", Host: host})
		for _, c := range cookies {
			slog.Info("cookie-jar-dump", "host", host, "name", c.Name, "value_len", len(c.Value), "domain", c.Domain)
		}
		if len(cookies) == 0 {
			slog.Info("cookie-jar-dump", "host", host, "count", 0)
		}
	}
}

func validateAndFetchInfo(client *http.Client, ticket string) (*QRLoginResult, error) {
	reqURL := fmt.Sprintf(qrValidURL, url.QueryEscape(ticket))
	req, _ := http.NewRequest("GET", reqURL, nil)
	req.Header.Set("User-Agent", jdUserAgent)
	req.Header.Set("Referer", "https://passport.jd.com/new/login.aspx")

	// Log redirect chain for diagnostics
	originalCheckRedirect := client.CheckRedirect
	var redirectChain []string
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		from := via[len(via)-1].URL.String()
		to := req.URL.String()
		slog.Info("qr-validate redirect", "from", from, "to", to, "step", len(via))
		redirectChain = append(redirectChain, from+" -> "+to)
		if len(via) >= 10 {
			return fmt.Errorf("too many redirects (%d)", len(via))
		}
		return nil
	}

	resp, err := client.Do(req)
	client.CheckRedirect = originalCheckRedirect
	if err != nil {
		return nil, fmt.Errorf("validate ticket: %w", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	slog.Info("qr-validate response", "status", resp.StatusCode, "redirects", len(redirectChain), "body", string(body[:minInt(len(body), 500)]))
	slog.Info("qr-validate resp-headers", "set-cookie", resp.Header.Values("Set-Cookie"))

	// Step 1: Check cookies from the entire request chain (redirects may have set pt_key)
	ptKey, ptPin := extractPtKey(client.Jar)
	if ptKey != "" {
		slog.Info("qr-validate pt_key found from request chain", "redirects", len(redirectChain), "pt_key_len", len(ptKey))
		return buildLoginResult(ptKey, ptPin)
	}

	// Step 2: Parse JSON response
	var vResult struct {
		ReturnCode int    `json:"returnCode"`
		RiskCode   int    `json:"riskCode"`
		URL        string `json:"url,omitempty"`
	}
	if err := json.Unmarshal(body, &vResult); err != nil {
		// Not JSON — might be HTML from a redirect-based flow
		slog.Warn("qr-validate response not JSON, dumping cookies", "body_preview", string(body[:minInt(len(body), 200)]))
		dumpAllCookies(client.Jar)
		return nil, fmt.Errorf("pt_key not found, response not JSON (status=%d)", resp.StatusCode)
	}
	if vResult.ReturnCode != 0 {
		return nil, fmt.Errorf("ticket validation failed (code=%d)", vResult.ReturnCode)
	}
	if vResult.RiskCode != 0 {
		slog.Warn("qr-validate risk control triggered", "riskCode", vResult.RiskCode, "url", vResult.URL)
		return nil, &QRVerifyNeededError{
			RiskCode:  vResult.RiskCode,
			VerifyURL: vResult.URL,
		}
	}

	// Step 3: Follow URL from JSON response
	if vResult.URL != "" {
		slog.Info("qr-validate following URL", "url", vResult.URL)
		followURL := vResult.URL
		if strings.HasPrefix(followURL, "http://") {
			followURL = "https://" + followURL[7:]
		}
		rReq, _ := http.NewRequest("GET", followURL, nil)
		rReq.Header.Set("User-Agent", jdUserAgent)
		rReq.Header.Set("Referer", "https://passport.jd.com/new/login.aspx")
		rReq.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
		rResp, err := client.Do(rReq)
		if err != nil {
			slog.Warn("qr-validate URL follow failed", "error", err)
		} else {
			slog.Info("qr-validate URL resp", "status", rResp.StatusCode, "set-cookie", rResp.Header.Values("Set-Cookie"))
			rResp.Body.Close()
		}
		ptKey, ptPin = extractPtKey(client.Jar)
	}

	// Step 4: Final check
	if ptKey == "" {
		slog.Error("qr-validate pt_key not found after all attempts")
		dumpAllCookies(client.Jar)
		return nil, fmt.Errorf("pt_key cookie not found after validation")
	}

	return buildLoginResult(ptKey, ptPin)
}

func buildLoginResult(ptKey, ptPin string) (*QRLoginResult, error) {
	slog.Info("qr-login cookies extracted", "pt_key_len", len(ptKey), "pt_pin_len", len(ptPin))

	userInfo, err := fetchUserInfoWithPtKey(ptKey)
	if err != nil {
		return nil, err
	}

	userID := ""
	realName := ""
	if data, ok := userInfo["data"].(map[string]interface{}); ok {
		if id, ok := data["userId"].(string); ok && id != "" {
			userID = id
		}
		if name, ok := data["realName"].(string); ok && name != "" {
			realName = name
		}
	}


	if userID == "" {
		slog.Error("qr-login: userId not found in userInfo response")
		return nil, fmt.Errorf("无法从 JoyCode API 获取用户ID")
	}

	return &QRLoginResult{PtKey: ptKey, PtPin: ptPin, UserID: userID, RealName: realName}, nil
}

func fetchUserInfoWithPtKey(ptKey string) (map[string]interface{}, error) {
	body := map[string]interface{}{
		"tenant": "JOYCODE", "userId": "",
		"client": "JoyCode", "clientVersion": "2.4.5",
		"sessionId": "qr-login-session",
	}
	data, _ := json.Marshal(body)
	req, err := http.NewRequest("POST", "https://joycode-api.jd.com/api/saas/user/v1/userInfo", strings.NewReader(string(data)))
	if err != nil {
		return nil, err
	}
	req.Header = http.Header{
		"Content-Type": {"application/json; charset=UTF-8"},
		"ptKey":        {ptKey},
		"loginType":    {"N_PIN_PC"},
		"User-Agent":   {"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) JoyCode/2.4.5 Chrome/133.0.0.0 Electron/35.2.0 Safari/537.36"},
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("userInfo request: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("parse userInfo: %w", err)
	}
	code, _ := result["code"].(float64)
	if code != 0 {
		msg, _ := result["msg"].(string)
		return nil, fmt.Errorf("userInfo error (code=%.0f): %s", code, msg)
	}
	return result, nil
}

// QRCleanup removes a QR login session.
func QRCleanup(sessionID string) {
	qrSessionsMu.Lock()
	delete(qrSessions, sessionID)
	qrSessionsMu.Unlock()
	slog.Debug("qr session cleaned up", "session_id", sessionID)
}

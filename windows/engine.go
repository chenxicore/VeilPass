package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	_ "time/tzdata"
)

type ProxyMode string

const (
	ProxyModeSystem ProxyMode = "系统代理"
	ProxyModeManual ProxyMode = "手动代理"
	ProxyModeDirect ProxyMode = "直连网络"
)

type BrowserEnvironment struct {
	Version             int      `json:"version"`
	ProxyKey            string   `json:"proxy_key"`
	ExitIP              string   `json:"exit_ip"`
	ExitCountry         string   `json:"exit_country"`
	ExitRegion          string   `json:"exit_region"`
	ExitCity            string   `json:"exit_city"`
	ExitSource          string   `json:"exit_source"`
	Locale              string   `json:"locale"`
	Languages           []string `json:"languages"`
	AcceptLanguage      string   `json:"accept_language"`
	Timezone            string   `json:"timezone"`
	ViewportWidth       int      `json:"viewport_width"`
	ViewportHeight      int      `json:"viewport_height"`
	ScreenWidth         int      `json:"screen_width"`
	ScreenHeight        int      `json:"screen_height"`
	DeviceScaleFactor   float64  `json:"device_scale_factor"`
	HardwareConcurrency int      `json:"hardware_concurrency"`
	DeviceMemory        int      `json:"device_memory"`
}

type ProfileMeta struct {
	ProfileID          string              `json:"profile_id"`
	ProfileDir         string              `json:"profile_dir"`
	DevtoolsPort       int                 `json:"devtools_port"`
	BrowserPID         int                 `json:"browser_pid"`
	UseProxy           bool                `json:"use_proxy"`
	ForceDirect        bool                `json:"force_direct"`
	ProxyUsed          string              `json:"proxy_used"`
	ChromeProxyServer  string              `json:"chrome_proxy_server"`
	BrowserEnvironment *BrowserEnvironment `json:"browser_environment,omitempty"`
	UpdatedAt          string              `json:"updated_at"`
}

type ProxyExitInfo struct {
	IP       string
	Country  string
	Region   string
	City     string
	Timezone string
	Source   string
}

type ActiveInstance struct {
	ProfileID    string
	DevtoolsPort int
	BrowserPID   int
	ProxyUsed    string
	UseProxy     bool
	ForceDirect  bool
	ExitCountry  string
	ExitCity     string
	Locale       string
	Timezone     string
	UpdatedAt    string
}

func (a ActiveInstance) RegionSummary() string {
	var regionParts []string
	if c := strings.TrimSpace(a.ExitCountry); c != "" {
		regionParts = append(regionParts, c)
	}
	if c := strings.TrimSpace(a.ExitCity); c != "" {
		regionParts = append(regionParts, c)
	}
	var parts []string
	if len(regionParts) > 0 {
		parts = append(parts, strings.Join(regionParts, " / "))
	}
	if tz := strings.TrimSpace(a.Timezone); tz != "" {
		parts = append(parts, tz)
	}
	if len(parts) == 0 {
		parts = append(parts, a.ProfileID)
	}
	return strings.Join(parts, " · ")
}

func (a ActiveInstance) ProxySummary() string {
	if a.ProxyUsed != "" {
		return a.ProxyUsed
	}
	if a.ForceDirect {
		return "直连（无代理）"
	}
	if a.UseProxy {
		return "系统代理"
	}
	return "直连（无代理）"
}

type LaunchOutcome struct {
	ProfileID          string
	DevtoolsPort       int
	ProxyLabel         string
	TimeSummary        string
	EnvironmentSummary string
}

var countryLocales = map[string]string{
	"AE": "ar-AE", "AR": "es-AR", "AU": "en-AU", "BR": "pt-BR", "CA": "en-CA",
	"CH": "de-CH", "CL": "es-CL", "CO": "es-CO", "DE": "de-DE", "ES": "es-ES",
	"FR": "fr-FR", "GB": "en-GB", "HK": "zh-HK", "ID": "id-ID", "IN": "en-IN",
	"IT": "it-IT", "JP": "ja-JP", "KR": "ko-KR", "MO": "zh-MO", "MX": "es-MX",
	"MY": "ms-MY", "NL": "nl-NL", "NZ": "en-NZ", "PH": "en-PH", "PL": "pl-PL",
	"PT": "pt-PT", "RU": "ru-RU", "SE": "sv-SE", "SG": "en-SG", "TH": "th-TH",
	"TR": "tr-TR", "TW": "zh-TW", "UA": "uk-UA", "US": "en-US", "VN": "vi-VN",
}

var countryTimezones = map[string]string{
	"AE": "Asia/Dubai", "AR": "America/Argentina/Buenos_Aires", "AU": "Australia/Sydney",
	"BR": "America/Sao_Paulo", "CA": "America/Toronto", "CH": "Europe/Zurich",
	"CL": "America/Santiago", "CO": "America/Bogota", "DE": "Europe/Berlin",
	"ES": "Europe/Madrid", "FR": "Europe/Paris", "GB": "Europe/London",
	"HK": "Asia/Hong_Kong", "ID": "Asia/Jakarta", "IN": "Asia/Kolkata",
	"IT": "Europe/Rome", "JP": "Asia/Tokyo", "KR": "Asia/Seoul",
	"MO": "Asia/Macau", "MX": "America/Mexico_City", "MY": "Asia/Kuala_Lumpur",
	"NL": "Europe/Amsterdam", "NZ": "Pacific/Auckland", "PH": "Asia/Manila",
	"PL": "Europe/Warsaw", "PT": "Europe/Lisbon", "RU": "Europe/Moscow",
	"SE": "Europe/Stockholm", "SG": "Asia/Singapore", "TH": "Asia/Bangkok",
	"TR": "Europe/Istanbul", "TW": "Asia/Taipei", "UA": "Europe/Kyiv",
	"US": "America/New_York", "VN": "Asia/Ho_Chi_Minh",
}

var viewports = [][3]int{
	{1280, 720, 100},
	{1365, 768, 100},
	{1440, 900, 100},
	{1536, 864, 125},
	{1600, 900, 100},
	{1920, 1080, 100},
}

func NormalizeManualProxy(raw string) (string, error) {
	input := strings.TrimSpace(raw)
	if input == "" {
		return "", errors.New("请输入代理地址，例如 socks5://127.0.0.1:1080。")
	}
	if strings.ContainsAny(input, " \t\r\n") {
		return "", errors.New("代理格式无效，请填写地址和端口，例如 socks5://127.0.0.1:1080。")
	}
	candidate := input
	if !strings.Contains(candidate, "://") {
		candidate = "http://" + candidate
	}
	u, err := url.Parse(candidate)
	if err != nil {
		return "", errors.New("代理格式无效，请填写地址和端口，例如 socks5://127.0.0.1:1080。")
	}
	scheme := strings.ToLower(u.Scheme)
	switch scheme {
	case "http", "https", "socks4", "socks5", "socks5h":
	default:
		return "", errors.New("仅支持 http / https / socks4 / socks5 代理。")
	}
	if u.User != nil {
		return "", errors.New("手动填写仅支持无需账号密码的代理地址。")
	}
	host := u.Hostname()
	portStr := u.Port()
	port, err := strconv.Atoi(portStr)
	if host == "" || err != nil || port < 1 || port > 65535 {
		return "", errors.New("代理格式无效，请填写有效主机和端口。")
	}
	if (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("代理地址不能包含额外路径或查询参数。")
	}
	u.Scheme = scheme
	u.Path = ""
	return u.String(), nil
}

func ResolveTargetURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "about:blank"
	}
	if strings.Contains(trimmed, "://") ||
		strings.HasPrefix(trimmed, "about:") ||
		strings.HasPrefix(trimmed, "chrome:") ||
		strings.HasPrefix(trimmed, "file:") {
		return trimmed
	}
	return "https://" + trimmed
}

func CountryHintFromProxy(proxy string) string {
	re1 := regexp.MustCompile(`(?i)(?:region[-_=]|country[-_=])([a-z]{2})(?:\b|[-_])`)
	if m := re1.FindStringSubmatch(proxy); len(m) == 2 {
		return strings.ToUpper(m[1])
	}
	re2 := regexp.MustCompile(`(?i)(?:^|[.:/@_-])(jp|us|sg|kr|tw|gb|de|fr|hk)(?:[.:/_-]|$)`)
	if m := re2.FindStringSubmatch(proxy); len(m) == 2 {
		return strings.ToUpper(m[1])
	}
	return "US"
}

func BuildBrowserEnvironment(profileID, proxy string, exitInfo ProxyExitInfo) BrowserEnvironment {
	country := strings.ToUpper(strings.TrimSpace(exitInfo.Country))
	if country == "" {
		country = "US"
	}
	locale, ok := countryLocales[country]
	if !ok {
		locale = "en-US"
	}
	baseLang := strings.SplitN(locale, "-", 2)[0]
	languages := []string{locale}
	if baseLang != "" && baseLang != locale {
		languages = append(languages, baseLang)
	}
	var acceptParts []string
	for i, lang := range languages {
		if i == 0 {
			acceptParts = append(acceptParts, lang)
		} else {
			q := 0.9 - float64(i)*0.1
			if q < 0.5 {
				q = 0.5
			}
			acceptParts = append(acceptParts, fmt.Sprintf("%s;q=%.1f", lang, q))
		}
	}

	tz := strings.TrimSpace(exitInfo.Timezone)
	if tz == "" {
		if mapped, exists := countryTimezones[country]; exists {
			tz = mapped
		} else {
			tz = "UTC"
		}
	}

	proxyHash := sha256.Sum256([]byte(strings.TrimSpace(proxy)))
	proxyKey := hex.EncodeToString(proxyHash[:])[:20]

	seedInput := fmt.Sprintf("browser-environment-v1|%s|%s|%s", strings.ToLower(profileID), proxyKey, country)
	seed := sha256.Sum256([]byte(seedInput))

	vp := viewports[int(seed[0])%len(viewports)]
	coresChoices := []int{4, 8, 8, 12, 16}
	memChoices := []int{4, 8, 8, 16}

	return BrowserEnvironment{
		Version:             1,
		ProxyKey:            proxyKey,
		ExitIP:              exitInfo.IP,
		ExitCountry:         country,
		ExitRegion:          exitInfo.Region,
		ExitCity:            exitInfo.City,
		ExitSource:          exitInfo.Source,
		Locale:              locale,
		Languages:           languages,
		AcceptLanguage:      strings.Join(acceptParts, ","),
		Timezone:            tz,
		ViewportWidth:       vp[0],
		ViewportHeight:      vp[1],
		ScreenWidth:         vp[0],
		ScreenHeight:        vp[1],
		DeviceScaleFactor:   float64(vp[2]) / 100.0,
		HardwareConcurrency: coresChoices[int(seed[1])%len(coresChoices)],
		DeviceMemory:        memChoices[int(seed[2])%len(memChoices)],
	}
}

func FormatProxyLocalTime(tzID string) string {
	tzID = strings.TrimSpace(tzID)
	if tzID == "" {
		return ""
	}
	loc, err := time.LoadLocation(tzID)
	if err != nil {
		return tzID
	}
	now := time.Now().In(loc)
	_, offset := now.Zone()
	sign := "+"
	if offset < 0 {
		sign = "-"
		offset = -offset
	}
	hours := offset / 3600
	mins := (offset % 3600) / 60
	gmt := fmt.Sprintf("GMT%s%d", sign, hours)
	if mins != 0 {
		gmt = fmt.Sprintf("GMT%s%d:%02d", sign, hours, mins)
	}
	return fmt.Sprintf("%s (%s) · %s", now.Format("2006-01-02 15:04:05"), gmt, tzID)
}

func FormatShortTime(tzID string) string {
	tzID = strings.TrimSpace(tzID)
	if tzID == "" {
		return ""
	}
	loc, err := time.LoadLocation(tzID)
	if err != nil {
		return ""
	}
	return time.Now().In(loc).Format("01-02 15:04")
}

// MARK: - HTTP Exit Detection (Pure Go Stdlib + SOCKS5 Remote DNS)

func dialSOCKS5(ctx context.Context, proxyAddr, targetAddr string, timeout time.Duration) (net.Conn, error) {
	host, portStr, err := net.SplitHostPort(targetAddr)
	if err != nil {
		return nil, err
	}
	portNum, err := strconv.Atoi(portStr)
	if err != nil || portNum < 1 || portNum > 65535 {
		return nil, fmt.Errorf("invalid port: %s", portStr)
	}
	if len(host) == 0 || len(host) > 255 {
		return nil, fmt.Errorf("invalid host: %s", host)
	}

	var d net.Dialer
	d.Timeout = timeout
	conn, err := d.DialContext(ctx, "tcp", proxyAddr)
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(timeout))

	if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		conn.Close()
		return nil, err
	}
	var greetResp [2]byte
	if _, err := io.ReadFull(conn, greetResp[:]); err != nil {
		conn.Close()
		return nil, err
	}
	if greetResp[0] != 0x05 || greetResp[1] != 0x00 {
		conn.Close()
		return nil, fmt.Errorf("socks5 auth rejected: %v", greetResp)
	}

	req := make([]byte, 0, 7+len(host))
	req = append(req, 0x05, 0x01, 0x00, 0x03, byte(len(host)))
	req = append(req, host...)
	req = append(req, byte(portNum>>8), byte(portNum&0xff))
	if _, err := conn.Write(req); err != nil {
		conn.Close()
		return nil, err
	}

	var hdr [4]byte
	if _, err := io.ReadFull(conn, hdr[:]); err != nil {
		conn.Close()
		return nil, err
	}
	if hdr[0] != 0x05 || hdr[1] != 0x00 {
		conn.Close()
		return nil, fmt.Errorf("socks5 connect failed: rep=%d", hdr[1])
	}

	var addrLen int
	switch hdr[3] {
	case 0x01:
		addrLen = 4
	case 0x04:
		addrLen = 16
	case 0x03:
		var l [1]byte
		if _, err := io.ReadFull(conn, l[:]); err != nil {
			conn.Close()
			return nil, err
		}
		addrLen = int(l[0])
	default:
		conn.Close()
		return nil, fmt.Errorf("socks5 unknown atyp: %d", hdr[3])
	}
	discard := make([]byte, addrLen+2)
	if _, err := io.ReadFull(conn, discard); err != nil {
		conn.Close()
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, nil
}

func makeExitHTTPClient(proxyURL string, forceDirect bool, timeout time.Duration) *http.Client {
	transport := &http.Transport{
		DialContext: (&net.Dialer{Timeout: timeout}).DialContext,
	}
	if forceDirect {
		transport.Proxy = nil
	} else if proxyURL == "" {
		transport.Proxy = http.ProxyFromEnvironment
	} else if u, err := url.Parse(proxyURL); err == nil {
		switch strings.ToLower(u.Scheme) {
		case "socks5", "socks5h":
			proxyAddr := u.Host
			transport.Proxy = nil
			transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
				return dialSOCKS5(ctx, proxyAddr, addr, timeout)
			}
		default:
			transport.Proxy = http.ProxyURL(u)
		}
	}
	return &http.Client{Transport: transport, Timeout: timeout}
}

func httpGetText(client *http.Client, targetURL string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, targetURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "curl/8.7.1")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func DetectProxyExit(proxyURL string, forceDirect bool) ProxyExitInfo {
	client := makeExitHTTPClient(proxyURL, forceDirect, 6*time.Second)

	if raw, err := httpGetText(client, "https://ipinfo.io/json"); err == nil {
		var payload struct {
			IP       string `json:"ip"`
			Country  string `json:"country"`
			Region   string `json:"region"`
			City     string `json:"city"`
			Timezone string `json:"timezone"`
		}
		if json.Unmarshal([]byte(raw), &payload) == nil {
			country := strings.ToUpper(strings.TrimSpace(payload.Country))
			if len(country) == 2 && country[0] >= 'A' && country[0] <= 'Z' && country[1] >= 'A' && country[1] <= 'Z' {
				return ProxyExitInfo{
					IP:       strings.TrimSpace(payload.IP),
					Country:  country,
					Region:   strings.TrimSpace(payload.Region),
					City:     strings.TrimSpace(payload.City),
					Timezone: strings.TrimSpace(payload.Timezone),
					Source:   "ipinfo",
				}
			}
		}
	}

	if raw, err := httpGetText(client, "https://www.cloudflare.com/cdn-cgi/trace"); err == nil {
		trace := map[string]string{}
		for _, line := range strings.Split(raw, "\n") {
			if parts := strings.SplitN(line, "=", 2); len(parts) == 2 {
				trace[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
			}
		}
		country := strings.ToUpper(trace["loc"])
		if len(country) == 2 && country[0] >= 'A' && country[0] <= 'Z' && country[1] >= 'A' && country[1] <= 'Z' {
			return ProxyExitInfo{
				IP:      trace["ip"],
				Country: country,
				Source:  "cloudflare",
			}
		}
	}

	return ProxyExitInfo{
		Country: CountryHintFromProxy(proxyURL),
		Source:  "fallback",
	}
}

// MARK: - Minimal RFC 6455 WebSocket CDP Client

type cdpMsg struct {
	ID        int             `json:"id,omitempty"`
	Method    string          `json:"method,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
	Params    json.RawMessage `json:"params,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     json.RawMessage `json:"error,omitempty"`
}

type CDPSession struct {
	conn      net.Conn
	reader    *bufio.Reader
	writeMu   sync.Mutex
	pendingMu sync.Mutex
	pending   map[int]chan cdpMsg
	nextID    int
	closed    chan struct{}
	closeOnce sync.Once
	onEvent   func(cdpMsg)
}

func DialCDP(wsURL string, timeout time.Duration) (*CDPSession, error) {
	u, err := url.Parse(wsURL)
	if err != nil {
		return nil, err
	}
	host := u.Host
	if !strings.Contains(host, ":") {
		host += ":80"
	}
	conn, err := net.DialTimeout("tcp", host, timeout)
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(timeout))

	var keyBytes [16]byte
	_, _ = rand.Read(keyBytes[:])
	secKey := base64.StdEncoding.EncodeToString(keyBytes[:])

	path := u.RequestURI()
	if path == "" {
		path = "/"
	}
	reqStr := fmt.Sprintf(
		"GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n",
		path, u.Host, secKey,
	)
	if _, err := conn.Write([]byte(reqStr)); err != nil {
		conn.Close()
		return nil, err
	}

	reader := bufio.NewReader(conn)
	statusLine, err := reader.ReadString('\n')
	if err != nil || !strings.Contains(statusLine, " 101 ") {
		conn.Close()
		return nil, fmt.Errorf("websocket handshake failed: %s (%v)", strings.TrimSpace(statusLine), err)
	}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			conn.Close()
			return nil, err
		}
		if line == "\r\n" || line == "\n" {
			break
		}
	}
	_ = sha1.New()
	_ = conn.SetDeadline(time.Time{})

	s := &CDPSession{
		conn:    conn,
		reader:  reader,
		pending: map[int]chan cdpMsg{},
		nextID:  1,
		closed:  make(chan struct{}),
	}
	go s.readLoop()
	return s, nil
}

func (s *CDPSession) Close() {
	s.closeOnce.Do(func() {
		close(s.closed)
		_ = s.conn.Close()
	})
}

func (s *CDPSession) writeFrame(opcode byte, payload []byte) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	var hdr []byte
	hdr = append(hdr, 0x80|opcode)
	n := len(payload)
	switch {
	case n <= 125:
		hdr = append(hdr, 0x80|byte(n))
	case n <= 65535:
		hdr = append(hdr, 0x80|126, byte(n>>8), byte(n))
	default:
		var lenBuf [8]byte
		binary.BigEndian.PutUint64(lenBuf[:], uint64(n))
		hdr = append(hdr, 0x80|127)
		hdr = append(hdr, lenBuf[:]...)
	}

	var mask [4]byte
	_, _ = rand.Read(mask[:])
	hdr = append(hdr, mask[:]...)

	masked := make([]byte, n)
	for i := 0; i < n; i++ {
		masked[i] = payload[i] ^ mask[i%4]
	}

	if _, err := s.conn.Write(hdr); err != nil {
		return err
	}
	if n > 0 {
		if _, err := s.conn.Write(masked); err != nil {
			return err
		}
	}
	return nil
}

func (s *CDPSession) readFrame() (byte, []byte, error) {
	var msgBuf []byte
	var firstOp byte
	for {
		var h [2]byte
		if _, err := io.ReadFull(s.reader, h[:]); err != nil {
			return 0, nil, err
		}
		fin := (h[0] & 0x80) != 0
		op := h[0] & 0x0f
		masked := (h[1] & 0x80) != 0
		length := uint64(h[1] & 0x7f)

		if length == 126 {
			var ext [2]byte
			if _, err := io.ReadFull(s.reader, ext[:]); err != nil {
				return 0, nil, err
			}
			length = uint64(binary.BigEndian.Uint16(ext[:]))
		} else if length == 127 {
			var ext [8]byte
			if _, err := io.ReadFull(s.reader, ext[:]); err != nil {
				return 0, nil, err
			}
			length = binary.BigEndian.Uint64(ext[:])
		}

		var mask [4]byte
		if masked {
			if _, err := io.ReadFull(s.reader, mask[:]); err != nil {
				return 0, nil, err
			}
		}

		if length > 32*1024*1024 {
			return 0, nil, errors.New("websocket frame too large")
		}
		payload := make([]byte, length)
		if length > 0 {
			if _, err := io.ReadFull(s.reader, payload); err != nil {
				return 0, nil, err
			}
			if masked {
				for i := range payload {
					payload[i] ^= mask[i%4]
				}
			}
		}

		if op == 0x8 { // close
			return 0x8, payload, io.EOF
		}
		if op == 0x9 { // ping
			_ = s.writeFrame(0xA, payload)
			continue
		}
		if op == 0xA { // pong
			continue
		}

		if op != 0x0 {
			firstOp = op
		}
		msgBuf = append(msgBuf, payload...)
		if fin {
			return firstOp, msgBuf, nil
		}
	}
}

func (s *CDPSession) readLoop() {
	defer s.Close()
	for {
		op, payload, err := s.readFrame()
		if err != nil || op == 0x8 {
			return
		}
		var msg cdpMsg
		if json.Unmarshal(payload, &msg) != nil {
			continue
		}
		if msg.ID > 0 {
			s.pendingMu.Lock()
			ch := s.pending[msg.ID]
			delete(s.pending, msg.ID)
			s.pendingMu.Unlock()
			if ch != nil {
				ch <- msg
			}
		} else if s.onEvent != nil {
			s.onEvent(msg)
		}
	}
}

func (s *CDPSession) Call(method string, params any, sessionID string, timeout time.Duration) (cdpMsg, error) {
	s.pendingMu.Lock()
	id := s.nextID
	s.nextID++
	ch := make(chan cdpMsg, 1)
	s.pending[id] = ch
	s.pendingMu.Unlock()

	reqObj := map[string]any{
		"id":     id,
		"method": method,
		"params": params,
	}
	if sessionID != "" {
		reqObj["sessionId"] = sessionID
	}
	raw, err := json.Marshal(reqObj)
	if err != nil {
		return cdpMsg{}, err
	}
	if err := s.writeFrame(0x1, raw); err != nil {
		s.pendingMu.Lock()
		delete(s.pending, id)
		s.pendingMu.Unlock()
		return cdpMsg{}, err
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case resp := <-ch:
		if len(resp.Error) > 0 && string(resp.Error) != "null" {
			return resp, fmt.Errorf("%s: %s", method, string(resp.Error))
		}
		return resp, nil
	case <-timer.C:
		s.pendingMu.Lock()
		delete(s.pending, id)
		s.pendingMu.Unlock()
		return cdpMsg{}, fmt.Errorf("DevTools %s 超时", method)
	case <-s.closed:
		return cdpMsg{}, errors.New("DevTools 连接已关闭")
	}
}

// MARK: - Isolated Browser Engine

type IsolatedBrowserEngine struct {
	mu       sync.Mutex
	sessions map[string][]*CDPSession
}

var Engine = &IsolatedBrowserEngine{
	sessions: map[string][]*CDPSession{},
}

var localhostHTTP = &http.Client{
	Timeout: 3 * time.Second,
	Transport: &http.Transport{
		Proxy: nil,
	},
}

func (e *IsolatedBrowserEngine) ProfilesRootDir() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		if dir, err := os.UserConfigDir(); err == nil && dir != "" {
			base = dir
		} else if home, err := os.UserHomeDir(); err == nil {
			base = filepath.Join(home, "AppData", "Local")
		} else {
			base = "."
		}
	}
	return filepath.Join(base, "IsolatedBrowser", "Profiles")
}

func (e *IsolatedBrowserEngine) metaPath(profileDir string) string {
	return filepath.Join(profileDir, ".stable-browser-meta.json")
}

func (e *IsolatedBrowserEngine) readMeta(profileDir string) *ProfileMeta {
	raw, err := os.ReadFile(e.metaPath(profileDir))
	if err != nil {
		return nil
	}
	var meta ProfileMeta
	if json.Unmarshal(raw, &meta) != nil {
		return nil
	}
	return &meta
}

func (e *IsolatedBrowserEngine) writeMeta(profileDir string, meta ProfileMeta) {
	_ = os.MkdirAll(profileDir, 0o755)
	if raw, err := json.MarshalIndent(meta, "", "  "); err == nil {
		_ = os.WriteFile(e.metaPath(profileDir), raw, 0o644)
	}
}

func (e *IsolatedBrowserEngine) LaunchChrome(targetURL string, mode ProxyMode, manualProxy string) (*LaunchOutcome, error) {
	chromePath, err := locateChromeExecutable()
	if err != nil {
		return nil, err
	}

	var selectedProxy string
	var useProxy bool
	var forceDirect bool

	switch mode {
	case ProxyModeSystem:
		if sysProxy := readSystemProxyURL(); sysProxy != "" {
			selectedProxy = sysProxy
			useProxy = true
		}
	case ProxyModeManual:
		normalized, err := NormalizeManualProxy(manualProxy)
		if err != nil {
			return nil, err
		}
		selectedProxy = normalized
		useProxy = true
	case ProxyModeDirect:
		forceDirect = true
	}

	profileID := fmt.Sprintf("browser-%d", time.Now().Unix())
	profileDir := filepath.Join(e.ProfilesRootDir(), profileID)
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建浏览器目录失败: %w", err)
	}

	exitInfo := DetectProxyExit(selectedProxy, forceDirect)
	env := BuildBrowserEnvironment(profileID, selectedProxy, exitInfo)
	syncChromeProfileLanguage(profileDir, env)

	port, err := findFreePort()
	if err != nil {
		return nil, fmt.Errorf("分配调试端口失败: %w", err)
	}

	chromeProxy := chromeProxyServerArg(selectedProxy)
	args := []string{
		"--user-data-dir=" + profileDir,
		"--remote-debugging-address=127.0.0.1",
		fmt.Sprintf("--remote-debugging-port=%d", port),
		"--no-first-run",
		"--no-default-browser-check",
		"--new-window",
		"--lang=" + env.Locale,
		"--accept-lang=" + strings.Join(env.Languages, ","),
		fmt.Sprintf("--window-size=%d,%d", env.ViewportWidth, env.ViewportHeight),
	}
	if chromeProxy != "" {
		args = append(args, "--proxy-server="+chromeProxy)
	} else if forceDirect {
		args = append(args, "--no-proxy-server")
	}
	args = append(args, "about:blank")

	cmd := exec.Command(chromePath, args...)
	cmd.Env = append(os.Environ(), "TZ="+env.Timezone)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("启动 Chrome 失败: %w", err)
	}
	pid := cmd.Process.Pid
	go func() { _ = cmd.Wait() }()

	if err := waitForDevTools(port, 20*time.Second); err != nil {
		_ = cmd.Process.Kill()
		return nil, err
	}

	pages, _ := fetchPageTargets(port)
	page := pickPageTarget(pages, "about:blank")
	if page == nil {
		page = createNewPage(port)
	}
	if page == nil || page.WebSocketDebuggerURL == "" {
		return nil, errors.New("未找到活动的 Chrome 页面标签")
	}
	activatePage(port, page.ID)

	pageCDP, err := DialCDP(page.WebSocketDebuggerURL, 8*time.Second)
	if err != nil {
		return nil, fmt.Errorf("连接页面调试通道失败: %w", err)
	}
	if err := applyBrowserEnvironment(pageCDP, "", env); err != nil {
		pageCDP.Close()
		return nil, fmt.Errorf("注入浏览器环境失败: %w", err)
	}

	var keptSessions []*CDPSession
	keptSessions = append(keptSessions, pageCDP)

	// Attach browser-level auto-attach so new tabs opened later also inherit timezone/locale/headers.
	if browserWS := fetchBrowserWS(port); browserWS != "" {
		if bCDP, err := DialCDP(browserWS, 5*time.Second); err == nil {
			bCDP.onEvent = func(ev cdpMsg) {
				if ev.Method != "Target.attachedToTarget" {
					return
				}
				var p struct {
					SessionID  string `json:"sessionId"`
					TargetInfo struct {
						Type string `json:"type"`
						URL  string `json:"url"`
					} `json:"targetInfo"`
				}
				if json.Unmarshal(ev.Params, &p) == nil && p.SessionID != "" && p.TargetInfo.Type == "page" {
					go func(sid string) {
						_ = applyBrowserEnvironment(bCDP, sid, env)
					}(p.SessionID)
				}
			}
			_, _ = bCDP.Call("Target.setAutoAttach", map[string]any{
				"autoAttach":             true,
				"waitForDebuggerOnStart": false,
				"flatten":                true,
			}, "", 5*time.Second)
			keptSessions = append(keptSessions, bCDP)
		}
	}

	e.mu.Lock()
	for _, old := range e.sessions[profileID] {
		old.Close()
	}
	e.sessions[profileID] = keptSessions
	e.mu.Unlock()

	meta := ProfileMeta{
		ProfileID:          profileID,
		ProfileDir:         profileDir,
		DevtoolsPort:       port,
		BrowserPID:         pid,
		UseProxy:           useProxy,
		ForceDirect:        forceDirect,
		ProxyUsed:          selectedProxy,
		ChromeProxyServer:  chromeProxy,
		BrowserEnvironment: &env,
		UpdatedAt:          time.Now().Format("2006-01-02T15:04:05"),
	}
	e.writeMeta(profileDir, meta)

	if targetURL != "about:blank" {
		resp, err := pageCDP.Call("Page.navigate", map[string]any{"url": targetURL}, "", 20*time.Second)
		if err != nil {
			return nil, err
		}
		var navRes struct {
			ErrorText string `json:"errorText"`
		}
		if json.Unmarshal(resp.Result, &navRes) == nil && navRes.ErrorText != "" {
			return nil, fmt.Errorf("页面加载失败: %s", navRes.ErrorText)
		}
	}

	meta.UpdatedAt = time.Now().Format("2006-01-02T15:04:05")
	e.writeMeta(profileDir, meta)

	proxyLabel := "默认网络（无系统代理）"
	if selectedProxy != "" {
		proxyLabel = selectedProxy
	} else if forceDirect {
		proxyLabel = "直连（绕过系统代理）"
	}

	var regionParts []string
	if env.ExitCountry != "" {
		regionParts = append(regionParts, env.ExitCountry)
	}
	if env.ExitCity != "" {
		regionParts = append(regionParts, env.ExitCity)
	}
	envParts := []string{}
	if len(regionParts) > 0 {
		envParts = append(envParts, strings.Join(regionParts, " / "))
	}
	envParts = append(envParts, env.Locale, fmt.Sprintf("%d×%d", env.ViewportWidth, env.ViewportHeight))

	return &LaunchOutcome{
		ProfileID:          profileID,
		DevtoolsPort:       port,
		ProxyLabel:         proxyLabel,
		TimeSummary:        FormatProxyLocalTime(env.Timezone),
		EnvironmentSummary: strings.Join(envParts, " · "),
	}, nil
}

func (e *IsolatedBrowserEngine) ListActiveInstances() []ActiveInstance {
	root := e.ProfilesRootDir()
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}

	var items []ActiveInstance
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		meta := e.readMeta(dir)
		if meta == nil || meta.DevtoolsPort <= 0 {
			continue
		}
		if !isDevToolsAlive(meta.DevtoolsPort) {
			continue
		}
		var country, city, locale, tz string
		if meta.BrowserEnvironment != nil {
			country = meta.BrowserEnvironment.ExitCountry
			city = meta.BrowserEnvironment.ExitCity
			locale = meta.BrowserEnvironment.Locale
			tz = meta.BrowserEnvironment.Timezone
		}
		items = append(items, ActiveInstance{
			ProfileID:    meta.ProfileID,
			DevtoolsPort: meta.DevtoolsPort,
			BrowserPID:   meta.BrowserPID,
			ProxyUsed:    meta.ProxyUsed,
			UseProxy:     meta.UseProxy,
			ForceDirect:  meta.ForceDirect,
			ExitCountry:  country,
			ExitCity:     city,
			Locale:       locale,
			Timezone:     tz,
			UpdatedAt:    meta.UpdatedAt,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].UpdatedAt > items[j].UpdatedAt
	})
	return items
}

func (e *IsolatedBrowserEngine) CloseInstance(profileID string) error {
	profileDir := filepath.Join(e.ProfilesRootDir(), profileID)
	meta := e.readMeta(profileDir)
	port := 0
	pid := 0
	if meta != nil {
		port = meta.DevtoolsPort
		pid = meta.BrowserPID
	}

	e.mu.Lock()
	sessions := e.sessions[profileID]
	delete(e.sessions, profileID)
	e.mu.Unlock()
	for _, s := range sessions {
		s.Close()
	}

	var closeErr error
	if port > 0 && isDevToolsAlive(port) {
		if wsURL := fetchBrowserWS(port); wsURL != "" {
			if cdp, err := DialCDP(wsURL, 3*time.Second); err == nil {
				_, closeErr = cdp.Call("Browser.close", map[string]any{}, "", 3*time.Second)
				cdp.Close()
			} else {
				closeErr = err
			}
		}
	}

	closed := waitUntilDevToolsClosed(port, 5*time.Second)
	if !closed && pid > 0 {
		_ = terminateProcessByPID(pid)
		closed = waitUntilDevToolsClosed(port, 3*time.Second)
	}
	if !closed {
		if closeErr != nil {
			return closeErr
		}
		return fmt.Errorf("Chrome 进程未能关闭（端口 %d）", port)
	}

	if meta != nil {
		meta.DevtoolsPort = 0
		meta.BrowserPID = 0
		meta.UpdatedAt = time.Now().Format("2006-01-02T15:04:05")
		e.writeMeta(profileDir, *meta)
	}
	return nil
}

// MARK: - Helpers

func chromeProxyServerArg(proxy string) string {
	if proxy == "" {
		return ""
	}
	if strings.HasPrefix(proxy, "socks5h://") {
		return "socks5://" + strings.TrimPrefix(proxy, "socks5h://")
	}
	return proxy
}

func syncChromeProfileLanguage(profileDir string, env BrowserEnvironment) {
	langs := strings.Join(env.Languages, ",")
	defDir := filepath.Join(profileDir, "Default")
	_ = os.MkdirAll(defDir, 0o755)
	updateJSONSection(filepath.Join(defDir, "Preferences"), "intl", map[string]any{
		"accept_languages":   langs,
		"selected_languages": langs,
	})
	updateJSONSection(filepath.Join(profileDir, "Local State"), "intl", map[string]any{
		"app_locale": env.Locale,
	})
}

func updateJSONSection(filePath, section string, values map[string]any) {
	root := map[string]any{}
	if raw, err := os.ReadFile(filePath); err == nil {
		_ = json.Unmarshal(raw, &root)
	}
	sec, _ := root[section].(map[string]any)
	if sec == nil {
		sec = map[string]any{}
	}
	for k, v := range values {
		sec[k] = v
	}
	root[section] = sec
	if out, err := json.Marshal(root); err == nil {
		_ = os.WriteFile(filePath, out, 0o644)
	}
}

func applyBrowserEnvironment(cdp *CDPSession, sessionID string, env BrowserEnvironment) error {
	script := buildInjectionScript(env)
	cmds := []struct {
		method string
		params map[string]any
	}{
		{"Page.enable", map[string]any{}},
		{"Network.enable", map[string]any{}},
		{"Emulation.setTimezoneOverride", map[string]any{"timezoneId": env.Timezone}},
		{"Emulation.setLocaleOverride", map[string]any{"locale": env.Locale}},
		{"Emulation.setDeviceMetricsOverride", map[string]any{
			"width":             env.ViewportWidth,
			"height":            env.ViewportHeight,
			"deviceScaleFactor": env.DeviceScaleFactor,
			"mobile":            false,
			"screenWidth":       env.ScreenWidth,
			"screenHeight":      env.ScreenHeight,
		}},
		{"Network.setExtraHTTPHeaders", map[string]any{
			"headers": map[string]string{"Accept-Language": env.AcceptLanguage},
		}},
		{"Page.addScriptToEvaluateOnNewDocument", map[string]any{"source": script}},
		{"Runtime.evaluate", map[string]any{
			"expression":    script,
			"awaitPromise":  true,
			"returnByValue": true,
		}},
	}
	for _, cmd := range cmds {
		if _, err := cdp.Call(cmd.method, cmd.params, sessionID, 8*time.Second); err != nil {
			return err
		}
	}
	return nil
}

func buildInjectionScript(env BrowserEnvironment) string {
	payload, _ := json.Marshal(map[string]any{
		"languages":           env.Languages,
		"hardwareConcurrency": env.HardwareConcurrency,
		"deviceMemory":        env.DeviceMemory,
		"screenWidth":         env.ScreenWidth,
		"screenHeight":        env.ScreenHeight,
	})
	return fmt.Sprintf(`(() => {
  const fp = %s;
  const defineGetter = (target, name, value) => {
    try { Object.defineProperty(target, name, { get: () => value, configurable: true }); } catch (_) {}
  };
  defineGetter(Navigator.prototype, 'webdriver', undefined);
  defineGetter(Navigator.prototype, 'language', fp.languages[0]);
  defineGetter(Navigator.prototype, 'languages', fp.languages);
  defineGetter(Navigator.prototype, 'hardwareConcurrency', fp.hardwareConcurrency);
  defineGetter(Navigator.prototype, 'deviceMemory', fp.deviceMemory);
  defineGetter(Screen.prototype, 'width', fp.screenWidth);
  defineGetter(Screen.prototype, 'height', fp.screenHeight);
  defineGetter(Screen.prototype, 'availWidth', fp.screenWidth);
  defineGetter(Screen.prototype, 'availHeight', fp.screenHeight - 40);
})();`, string(payload))
}

func findFreePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

type pageTarget struct {
	ID                   string `json:"id"`
	Type                 string `json:"type"`
	URL                  string `json:"url"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

func fetchBrowserWS(port int) string {
	resp, err := localhostHTTP.Get(fmt.Sprintf("http://127.0.0.1:%d/json/version", port))
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var data struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if json.NewDecoder(resp.Body).Decode(&data) == nil {
		return data.WebSocketDebuggerURL
	}
	return ""
}

func isDevToolsAlive(port int) bool {
	if port <= 0 {
		return false
	}
	return fetchBrowserWS(port) != ""
}

func waitForDevTools(port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if isDevToolsAlive(port) {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("Chrome 调试端口 %d 启动超时，请重试", port)
}

func waitUntilDevToolsClosed(port int, timeout time.Duration) bool {
	if port <= 0 {
		return true
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !isDevToolsAlive(port) {
			return true
		}
		time.Sleep(250 * time.Millisecond)
	}
	return !isDevToolsAlive(port)
}

func fetchPageTargets(port int) ([]pageTarget, error) {
	resp, err := localhostHTTP.Get(fmt.Sprintf("http://127.0.0.1:%d/json/list", port))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var pages []pageTarget
	if err := json.NewDecoder(resp.Body).Decode(&pages); err != nil {
		return nil, err
	}
	return pages, nil
}

func pickPageTarget(pages []pageTarget, preferredURL string) *pageTarget {
	var candidates []pageTarget
	for _, p := range pages {
		if p.Type != "page" || p.WebSocketDebuggerURL == "" {
			continue
		}
		if strings.HasPrefix(p.URL, "chrome-extension://") || strings.HasPrefix(p.URL, "devtools://") || strings.HasPrefix(p.URL, "chrome://") {
			continue
		}
		if p.URL == preferredURL {
			cp := p
			return &cp
		}
		candidates = append(candidates, p)
	}
	if len(candidates) > 0 {
		return &candidates[0]
	}
	return nil
}

func createNewPage(port int) *pageTarget {
	req, err := http.NewRequest(http.MethodPut, fmt.Sprintf("http://127.0.0.1:%d/json/new?about:blank", port), nil)
	if err != nil {
		return nil
	}
	resp, err := localhostHTTP.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	var p pageTarget
	if json.NewDecoder(resp.Body).Decode(&p) == nil && p.WebSocketDebuggerURL != "" {
		return &p
	}
	return nil
}

func activatePage(port int, targetID string) {
	if targetID == "" {
		return
	}
	resp, err := localhostHTTP.Get(fmt.Sprintf("http://127.0.0.1:%d/json/activate/%s", port, targetID))
	if err == nil {
		resp.Body.Close()
	}
}

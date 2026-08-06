package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	AuthModeMock   = "mock"
	AuthModeTikTok = "tiktok"
)

type Config struct {
	HTTPAddr           string
	GRPCAddr           string
	StoreDriver        string // memory | mysql
	MySQLDSN           string
	SessionSecret      string
	SessionTTL         time.Duration
	ServiceToken       string
	TopNDefault        int
	TopNMax            int
	RefreshSec         int
	Timezone           string // UTC or Asia/Shanghai etc.
	LogLevel           string            // trace|debug|info|error|fatal (RANK_LOG_LEVEL)
	AuthMode           string            // raw RANK_AUTH_MODE (csv or single)
	AuthModes          []string          // parsed enabled modes
	AuthChannelMap     map[string]string // channel -> mode (from RANK_AUTH_CHANNEL_MAP)
	TikTokAppID        string            // legacy single TikTok Developer App ID (optional)
	TikTokClientKey    string            // legacy single client_key (optional default)
	TikTokClientSecret string            // legacy single client_secret
	// TikTokClientSecrets maps client_key → client_secret for multi-game OAuth.
	// Profile / token exchange uses the client_key from the HTTP body, not a hardcoded env key.
	TikTokClientSecrets map[string]string
	DefaultAppID        string // optional default game app_id
}

func Load() Config {
	rawMode := getenv("RANK_AUTH_MODE", AuthModeMock)
	legacyKey := cleanEnvValue(os.Getenv("RANK_TT_CLIENT_KEY"))
	legacySecret := cleanEnvValue(os.Getenv("RANK_TT_CLIENT_SECRET"))
	secrets := ParseClientSecrets(os.Getenv("RANK_TT_CLIENT_SECRETS"))
	if legacyKey != "" && legacySecret != "" {
		if secrets == nil {
			secrets = map[string]string{}
		}
		// Legacy pair seeds the map; explicit RANK_TT_CLIENT_SECRETS entry wins if already set.
		if _, exists := secrets[legacyKey]; !exists {
			secrets[legacyKey] = legacySecret
		}
	}
	cfg := Config{
		HTTPAddr:            getenv("RANK_HTTP_ADDR", ":8000"),
		GRPCAddr:            getenv("RANK_GRPC_ADDR", ":8001"),
		StoreDriver:         getenv("RANK_STORE", "memory"),
		MySQLDSN:            getenv("RANK_MYSQL_DSN", ""),
		SessionSecret:       getenv("RANK_SESSION_SECRET", "dev-session-secret-change-me"),
		SessionTTL:          time.Duration(getenvInt("RANK_SESSION_TTL_SEC", 86400)) * time.Second,
		ServiceToken:        getenv("RANK_SERVICE_TOKEN", "dev-service-token"),
		TopNDefault:         getenvInt("RANK_TOPN_DEFAULT", 100),
		TopNMax:             getenvInt("RANK_TOPN_MAX", 100),
		RefreshSec:          getenvInt("RANK_REFRESH_SEC", 30),
		Timezone:            getenv("RANK_TZ", "UTC"),
		LogLevel:            getenv("RANK_LOG_LEVEL", "info"),
		AuthMode:            rawMode,
		AuthModes:           ParseAuthModes(rawMode),
		AuthChannelMap:      ParseChannelMap(os.Getenv("RANK_AUTH_CHANNEL_MAP")),
		TikTokAppID:         cleanEnvValue(os.Getenv("RANK_TT_APP_ID")),
		TikTokClientKey:     legacyKey,
		TikTokClientSecret:  legacySecret,
		TikTokClientSecrets: secrets,
		DefaultAppID:        getenv("RANK_DEFAULT_APP_ID", "parking_smart_brain"),
	}
	return cfg
}

// ParseAuthModes parses RANK_AUTH_MODE as comma-separated list (case-insensitive).
// Empty / unknown tokens are skipped; if nothing left, defaults to mock.
func ParseAuthModes(raw string) []string {
	parts := splitCSV(raw)
	seen := map[string]bool{}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		m := normalizeAuthMode(p)
		if m == "" || seen[m] {
			continue
		}
		seen[m] = true
		out = append(out, m)
	}
	if len(out) == 0 {
		return []string{AuthModeMock}
	}
	return out
}

// ParseChannelMap parses "ch1:mode1,ch2:mode2".
func ParseChannelMap(raw string) map[string]string {
	out := map[string]string{}
	for _, part := range splitCSV(raw) {
		i := strings.Index(part, ":")
		if i <= 0 || i >= len(part)-1 {
			continue
		}
		ch := strings.ToLower(strings.TrimSpace(part[:i]))
		mode := normalizeAuthMode(part[i+1:])
		if ch == "" || mode == "" {
			continue
		}
		out[ch] = mode
	}
	return out
}

// ParseClientSecrets parses multi-game TikTok secrets:
//
//	"client_key1:secret1,client_key2:secret2"
//	"client_key1=secret1,client_key2=secret2"
//
// Colons inside secrets are supported when using '=' separator.
func ParseClientSecrets(raw string) map[string]string {
	out := map[string]string{}
	for _, part := range splitCSV(raw) {
		part = cleanEnvValue(part)
		if part == "" {
			continue
		}
		var key, secret string
		if i := strings.Index(part, "="); i > 0 {
			key = cleanEnvValue(part[:i])
			secret = cleanEnvValue(part[i+1:])
		} else if i := strings.Index(part, ":"); i > 0 {
			key = cleanEnvValue(part[:i])
			secret = cleanEnvValue(part[i+1:])
		} else {
			continue
		}
		if key == "" || secret == "" {
			continue
		}
		out[key] = secret
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// SecretForClientKey returns the server-side client_secret for a client-provided client_key.
func (c Config) SecretForClientKey(clientKey string) (string, bool) {
	clientKey = strings.TrimSpace(clientKey)
	if clientKey == "" {
		return "", false
	}
	if c.TikTokClientSecrets != nil {
		if s := strings.TrimSpace(c.TikTokClientSecrets[clientKey]); s != "" {
			return s, true
		}
	}
	if clientKey == strings.TrimSpace(c.TikTokClientKey) {
		if s := strings.TrimSpace(c.TikTokClientSecret); s != "" {
			return s, true
		}
	}
	return "", false
}

func normalizeAuthMode(s string) string {
	switch strings.ToLower(cleanEnvValue(s)) {
	case AuthModeMock, "dev", "test":
		return AuthModeMock
	case AuthModeTikTok, "tt", "tiktok_minis":
		return AuthModeTikTok
	default:
		return ""
	}
}

// cleanEnvValue strips CR/LF, surrounding quotes — common when env files are edited on Windows
// or EnvironmentFile values keep quotes.
func cleanEnvValue(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "\"'")
	s = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(s, "\r", ""), "\n", ""))
	return s
}

func splitCSV(raw string) []string {
	raw = cleanEnvValue(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = cleanEnvValue(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// AuthModeEnabled reports whether mode is in the enabled list.
func (c Config) AuthModeEnabled(mode string) bool {
	mode = normalizeAuthMode(mode)
	for _, m := range c.AuthModes {
		if m == mode {
			return true
		}
	}
	// Compat: if AuthModes empty but AuthMode set as single value.
	if len(c.AuthModes) == 0 {
		return normalizeAuthMode(c.AuthMode) == mode
	}
	return false
}

// TikTokReady is true when at least one TikTok client_key/secret pair is available.
func (c Config) TikTokReady() bool {
	if c.TikTokClientSecrets != nil {
		for _, s := range c.TikTokClientSecrets {
			if strings.TrimSpace(s) != "" {
				return true
			}
		}
	}
	return strings.TrimSpace(c.TikTokClientKey) != "" && strings.TrimSpace(c.TikTokClientSecret) != ""
}

// ModeForChannel returns the auth mode for a channel.
// Lookup order: AuthChannelMap → defaults (tiktok_minis/douyin→tiktok) → mock.
// If chosen mode is not enabled or tiktok lacks credentials, falls back to mock.
func (c Config) ModeForChannel(channel string) string {
	ch := strings.ToLower(strings.TrimSpace(channel))
	mode := ""
	if c.AuthChannelMap != nil {
		if m, ok := c.AuthChannelMap[ch]; ok {
			mode = m
		}
	}
	if mode == "" {
		switch ch {
		case "tiktok_minis", "douyin":
			mode = AuthModeTikTok
		default:
			mode = AuthModeMock
		}
	}
	mode = normalizeAuthMode(mode)
	if mode == "" {
		mode = AuthModeMock
	}
	if mode == AuthModeTikTok {
		if !c.AuthModeEnabled(AuthModeTikTok) || !c.TikTokReady() {
			return AuthModeMock
		}
		return AuthModeTikTok
	}
	if !c.AuthModeEnabled(mode) {
		return AuthModeMock
	}
	return mode
}

func getenv(k, def string) string {
	if v := cleanEnvValue(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

func getenvInt(k string, def int) int {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

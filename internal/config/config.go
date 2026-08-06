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
	AuthMode           string            // raw RANK_AUTH_MODE (csv or single)
	AuthModes          []string          // parsed enabled modes
	AuthChannelMap     map[string]string // channel -> mode (from RANK_AUTH_CHANNEL_MAP)
	TikTokAppID        string            // TikTok Developer App ID (platform)
	TikTokClientKey    string
	TikTokClientSecret string
	DefaultAppID       string // optional default game app_id
}

func Load() Config {
	rawMode := getenv("RANK_AUTH_MODE", AuthModeMock)
	cfg := Config{
		HTTPAddr:           getenv("RANK_HTTP_ADDR", ":8000"),
		GRPCAddr:           getenv("RANK_GRPC_ADDR", ":8001"),
		StoreDriver:        getenv("RANK_STORE", "memory"),
		MySQLDSN:           getenv("RANK_MYSQL_DSN", ""),
		SessionSecret:      getenv("RANK_SESSION_SECRET", "dev-session-secret-change-me"),
		SessionTTL:         time.Duration(getenvInt("RANK_SESSION_TTL_SEC", 86400)) * time.Second,
		ServiceToken:       getenv("RANK_SERVICE_TOKEN", "dev-service-token"),
		TopNDefault:        getenvInt("RANK_TOPN_DEFAULT", 100),
		TopNMax:            getenvInt("RANK_TOPN_MAX", 100),
		RefreshSec:         getenvInt("RANK_REFRESH_SEC", 30),
		Timezone:           getenv("RANK_TZ", "UTC"),
		AuthMode:           rawMode,
		AuthModes:          ParseAuthModes(rawMode),
		AuthChannelMap:     ParseChannelMap(os.Getenv("RANK_AUTH_CHANNEL_MAP")),
		TikTokAppID:        os.Getenv("RANK_TT_APP_ID"),
		TikTokClientKey:    os.Getenv("RANK_TT_CLIENT_KEY"),
		TikTokClientSecret: os.Getenv("RANK_TT_CLIENT_SECRET"),
		DefaultAppID:       getenv("RANK_DEFAULT_APP_ID", "parking_smart_brain"),
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

func normalizeAuthMode(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case AuthModeMock, "dev", "test":
		return AuthModeMock
	case AuthModeTikTok, "tt", "tiktok_minis":
		return AuthModeTikTok
	default:
		return ""
	}
}

func splitCSV(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
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

// TikTokReady is true when TikTok credentials are present.
func (c Config) TikTokReady() bool {
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
	if v := os.Getenv(k); v != "" {
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

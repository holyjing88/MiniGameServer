package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	HTTPAddr       string
	GRPCAddr       string
	StoreDriver    string // memory | mysql
	MySQLDSN       string
	SessionSecret  string
	SessionTTL     time.Duration
	ServiceToken   string
	TopNDefault    int
	TopNMax        int
	RefreshSec     int
	Timezone       string // UTC or Asia/Shanghai etc.
	AuthMode       string // mock | tiktok
	TikTokClientKey    string
	TikTokClientSecret string
}

func Load() Config {
	return Config{
		HTTPAddr:           getenv("RANK_HTTP_ADDR", ":8080"),
		GRPCAddr:           getenv("RANK_GRPC_ADDR", ":9090"),
		StoreDriver:        getenv("RANK_STORE", "memory"),
		MySQLDSN:           getenv("RANK_MYSQL_DSN", ""),
		SessionSecret:      getenv("RANK_SESSION_SECRET", "dev-session-secret-change-me"),
		SessionTTL:         time.Duration(getenvInt("RANK_SESSION_TTL_SEC", 86400)) * time.Second,
		ServiceToken:       getenv("RANK_SERVICE_TOKEN", "dev-service-token"),
		TopNDefault:        getenvInt("RANK_TOPN_DEFAULT", 100),
		TopNMax:            getenvInt("RANK_TOPN_MAX", 100),
		RefreshSec:         getenvInt("RANK_REFRESH_SEC", 30),
		Timezone:           getenv("RANK_TZ", "UTC"),
		AuthMode:           getenv("RANK_AUTH_MODE", "mock"),
		TikTokClientKey:    os.Getenv("RANK_TT_CLIENT_KEY"),
		TikTokClientSecret: os.Getenv("RANK_TT_CLIENT_SECRET"),
	}
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

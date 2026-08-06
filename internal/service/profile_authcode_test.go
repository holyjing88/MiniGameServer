package service_test

import (
	"context"
	"testing"

	"minigameserver/internal/auth"
	"minigameserver/internal/config"
	"minigameserver/internal/hotcache"
	"minigameserver/internal/service"
	"minigameserver/internal/store/memory"
)

func TestFetchProfileByAuthCode_Mock(t *testing.T) {
	cfg := config.Config{DefaultAppID: "vampire_dormitory", AuthMode: "mock"}
	st := memory.New()
	sessions := auth.NewSessionManager("test-secret", 3600)
	svc := service.New(cfg, st, hotcache.New(), sessions, auth.MockResolver{})
	svc.SetProfileProvider(auth.MockProfileProvider{})

	out, err := svc.FetchProfileByAuthCode(context.Background(), service.FetchTTProfileInput{
		Code:    "mock:player1",
		Channel: "tiktok_minis",
	})
	if err != nil {
		t.Fatalf("FetchProfileByAuthCode: %v", err)
	}
	if out.Nickname == "" || out.AvatarURL == "" {
		t.Fatalf("empty profile: %+v", out)
	}
	if out.OpenID != "player1" {
		t.Fatalf("open_id=%q", out.OpenID)
	}
	if out.Source != "oauth" {
		t.Fatalf("source=%q", out.Source)
	}
}

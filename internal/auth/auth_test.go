package auth_test

import (
	"testing"
	"time"

	"minigameserver/internal/auth"
)

func TestSessionIssueParse(t *testing.T) {
	m := auth.NewSessionManager("secret", time.Hour)
	now := time.Unix(1_700_000_000, 0)
	tok, exp, err := m.Issue("app", "tiktok_minis", "openid-1", "tiktok_minis_openid-1", now)
	if err != nil || exp != 3600 || tok == "" {
		t.Fatalf("issue: tok=%q exp=%d err=%v", tok, exp, err)
	}
	c, err := m.Parse(tok, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if c.PlayerID != "tiktok_minis_openid-1" || c.OpenID != "openid-1" || c.AppID != "app" {
		t.Fatalf("%+v", c)
	}
	_, err = m.Parse(tok, now.Add(2*time.Hour))
	if err != auth.ErrExpiredToken {
		t.Fatalf("want expired, got %v", err)
	}
}

func TestMockResolver(t *testing.T) {
	var r auth.MockResolver
	id, err := r.Resolve("a", "c", "mock:oid")
	if err != nil || id != "oid" {
		t.Fatalf("%s %v", id, err)
	}
}

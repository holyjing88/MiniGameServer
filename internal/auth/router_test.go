package auth

import (
	"context"
	"fmt"
	"testing"

	"minigameserver/internal/config"
)

type fixedSelector string

func (s fixedSelector) ModeForChannel(string) string { return string(s) }

type mapSelector map[string]string

func (m mapSelector) ModeForChannel(ch string) string {
	if v, ok := m[ch]; ok {
		return v
	}
	return config.AuthModeMock
}

type recordingProvider struct {
	name string
	last string
}

func (p *recordingProvider) FetchProfile(_ context.Context, _, channel, openID, _ string) (*UserProfile, error) {
	p.last = "fetch:" + channel + ":" + openID
	return &UserProfile{OpenID: openID, Nickname: p.name + "_" + openID, AvatarURL: "http://x/" + p.name}, nil
}

func (p *recordingProvider) ExchangeCode(_ context.Context, _, channel, code string) (string, string, *UserProfile, error) {
	p.last = "exchange:" + channel + ":" + code
	oid := code
	return "tok-" + p.name, oid, &UserProfile{OpenID: oid, Nickname: p.name, AvatarURL: "http://x"}, nil
}

func TestMultiProfileProvider_RoutesByChannel(t *testing.T) {
	mockP := &recordingProvider{name: "mock"}
	ttP := &recordingProvider{name: "tiktok"}
	mp := NewMultiProfileProvider(mapSelector{
		"tiktok_minis": config.AuthModeTikTok,
		"weixin":       config.AuthModeMock,
	}, map[string]ProfileProvider{
		config.AuthModeMock:   mockP,
		config.AuthModeTikTok: ttP,
	}, MockProfileProvider{})

	prof, err := mp.FetchProfile(context.Background(), "app", "tiktok_minis", "u1", "")
	if err != nil || prof.Nickname != "tiktok_u1" {
		t.Fatalf("tiktok channel: %+v %v", prof, err)
	}
	if ttP.last == "" || mockP.last != "" {
		t.Fatalf("expected tiktok provider used, tt=%q mock=%q", ttP.last, mockP.last)
	}

	mockP.last, ttP.last = "", ""
	prof, err = mp.FetchProfile(context.Background(), "app", "weixin", "u2", "")
	if err != nil || prof.Nickname != "mock_u2" {
		t.Fatalf("weixin channel: %+v %v", prof, err)
	}
	if mockP.last == "" || ttP.last != "" {
		t.Fatalf("expected mock provider used, mock=%q tt=%q", mockP.last, ttP.last)
	}
}

func TestMultiResolver_RoutesByChannel(t *testing.T) {
	mr := NewMultiResolver(mapSelector{
		"tiktok_minis": config.AuthModeTikTok,
		"weixin":       config.AuthModeMock,
	}, map[string]OpenIDResolver{
		config.AuthModeMock: MockResolver{},
		config.AuthModeTikTok: OpenIDResolverFunc(func(_, _, code string) (string, error) {
			if code == "oauth-code" {
				return "tt-open", nil
			}
			return "", fmt.Errorf("bad code")
		}),
	}, MockResolver{})

	oid, err := mr.Resolve("app", "weixin", "mock:wx1")
	if err != nil || oid != "wx1" {
		t.Fatalf("weixin mock resolve: %q %v", oid, err)
	}
	oid, err = mr.Resolve("app", "tiktok_minis", "oauth-code")
	if err != nil || oid != "tt-open" {
		t.Fatalf("tiktok resolve: %q %v", oid, err)
	}
}

// OpenIDResolverFunc adapts a function to OpenIDResolver.
type OpenIDResolverFunc func(appID, channel, code string) (string, error)

func (f OpenIDResolverFunc) Resolve(appID, channel, code string) (string, error) {
	return f(appID, channel, code)
}

func TestBuildAuthStack_MockOnly(t *testing.T) {
	cfg := config.Config{AuthModes: []string{config.AuthModeMock}}
	profiles, resolver := BuildAuthStack(cfg)
	_, oid, prof, err := profiles.ExchangeCode(context.Background(), "app", "weixin", "mock:u")
	if err != nil || oid != "u" || prof == nil {
		t.Fatalf("mock exchange: %v %v %v", oid, prof, err)
	}
	oid2, err := resolver.Resolve("app", "weixin", "mock:u")
	if err != nil || oid2 != "u" {
		t.Fatalf("mock resolve: %v %v", oid2, err)
	}
}

func TestTikTokResolver_MockPrefix(t *testing.T) {
	r := TikTokResolver{Provider: nil, Mock: MockResolver{}}
	oid, err := r.Resolve("app", "tiktok_minis", "mock:debug1")
	if err != nil || oid != "debug1" {
		t.Fatalf("got %q %v", oid, err)
	}
}

package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// UserProfile is nickname + avatar for TikTok / channel display.
type UserProfile struct {
	OpenID    string
	Nickname  string
	AvatarURL string
}

// ProfileProvider fetches or synthesizes display profile from the channel platform.
type ProfileProvider interface {
	// FetchProfile loads nickname/avatar.
	// accessToken is optional; mock ignores it; TikTok requires it (or exchanges code).
	FetchProfile(ctx context.Context, appID, channel, openID, accessToken string) (*UserProfile, error)
	// ExchangeCode exchanges OAuth code for access_token + open_id (+ profile when possible).
	ExchangeCode(ctx context.Context, appID, channel, code string) (accessToken string, openID string, profile *UserProfile, err error)
}

// MockProfileProvider returns deterministic mock nickname/avatar for local & TikTok Minis debug.
type MockProfileProvider struct{}

func (MockProfileProvider) FetchProfile(_ context.Context, _, _, openID, _ string) (*UserProfile, error) {
	openID = strings.TrimSpace(openID)
	if openID == "" {
		return nil, fmt.Errorf("open_id required")
	}
	return &UserProfile{
		OpenID:    openID,
		Nickname:  "TT_" + openID,
		AvatarURL: mockAvatarURL(openID),
	}, nil
}

func (MockProfileProvider) ExchangeCode(_ context.Context, _, _, code string) (string, string, *UserProfile, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return "", "", nil, fmt.Errorf("empty code")
	}
	openID := code
	if strings.HasPrefix(code, "mock:") {
		openID = strings.TrimPrefix(code, "mock:")
	}
	if openID == "" {
		return "", "", nil, fmt.Errorf("empty mock open_id")
	}
	p := &UserProfile{
		OpenID:    openID,
		Nickname:  "TT_" + openID,
		AvatarURL: mockAvatarURL(openID),
	}
	return "mock-access-token", openID, p, nil
}

func mockAvatarURL(openID string) string {
	// Stable placeholder avatar for client UI (no external TikTok dependency in mock).
	seed := url.QueryEscape(openID)
	return "https://api.dicebear.com/7.x/thumbs/svg?seed=" + seed
}

// TikTokProfileProvider calls TikTok Open API v2 user/info.
// Docs: https://developers.tiktok.com/doc/tiktok-api-v2-get-user-info
type TikTokProfileProvider struct {
	ClientKey    string
	ClientSecret string
	HTTP         *http.Client
	TokenURL     string
	UserInfoURL  string
}

func NewTikTokProfileProvider(clientKey, clientSecret string) *TikTokProfileProvider {
	return &TikTokProfileProvider{
		ClientKey:    clientKey,
		ClientSecret: clientSecret,
		HTTP:         &http.Client{Timeout: 8 * time.Second},
		TokenURL:     "https://open.tiktokapis.com/v2/oauth/token/",
		UserInfoURL:  "https://open.tiktokapis.com/v2/user/info/",
	}
}

func (p *TikTokProfileProvider) client() *http.Client {
	if p.HTTP != nil {
		return p.HTTP
	}
	return &http.Client{Timeout: 8 * time.Second}
}

func (p *TikTokProfileProvider) FetchProfile(ctx context.Context, _, _, openID, accessToken string) (*UserProfile, error) {
	accessToken = strings.TrimSpace(accessToken)
	if accessToken == "" {
		return nil, fmt.Errorf("access_token required for TikTok profile fetch")
	}
	fields := "open_id,display_name,avatar_url,avatar_url_100"
	u := p.UserInfoURL + "?fields=" + url.QueryEscape(fields)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := p.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("tiktok user/info HTTP %d: %s", resp.StatusCode, truncate(string(body), 256))
	}
	var raw struct {
		Data struct {
			User struct {
				OpenID      string `json:"open_id"`
				DisplayName string `json:"display_name"`
				AvatarURL   string `json:"avatar_url"`
				Avatar100   string `json:"avatar_url_100"`
			} `json:"user"`
		} `json:"data"`
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("tiktok user/info decode: %w", err)
	}
	if raw.Error.Code != "" && raw.Error.Code != "ok" {
		return nil, fmt.Errorf("tiktok user/info: %s %s", raw.Error.Code, raw.Error.Message)
	}
	uinfo := raw.Data.User
	avatar := strings.TrimSpace(uinfo.AvatarURL)
	if avatar == "" {
		avatar = strings.TrimSpace(uinfo.Avatar100)
	}
	oid := strings.TrimSpace(uinfo.OpenID)
	if oid == "" {
		oid = strings.TrimSpace(openID)
	}
	nick := strings.TrimSpace(uinfo.DisplayName)
	if nick == "" {
		return nil, fmt.Errorf("tiktok user/info: empty display_name")
	}
	return &UserProfile{OpenID: oid, Nickname: nick, AvatarURL: avatar}, nil
}

func (p *TikTokProfileProvider) ExchangeCode(ctx context.Context, _, _, code string) (string, string, *UserProfile, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return "", "", nil, fmt.Errorf("empty code")
	}
	if p.ClientKey == "" || p.ClientSecret == "" {
		return "", "", nil, fmt.Errorf("TikTok client_key/secret not configured")
	}
	form := url.Values{}
	form.Set("client_key", p.ClientKey)
	form.Set("client_secret", p.ClientSecret)
	form.Set("code", code)
	form.Set("grant_type", "authorization_code")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := p.client().Do(req)
	if err != nil {
		return "", "", nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", nil, fmt.Errorf("tiktok oauth/token HTTP %d: %s", resp.StatusCode, truncate(string(body), 256))
	}
	var tok struct {
		AccessToken string `json:"access_token"`
		OpenID      string `json:"open_id"`
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &tok); err != nil {
		return "", "", nil, fmt.Errorf("tiktok oauth/token decode: %w", err)
	}
	if tok.AccessToken == "" {
		return "", "", nil, fmt.Errorf("tiktok oauth/token: %s %s", tok.Error, tok.Description)
	}
	prof, err := p.FetchProfile(ctx, "", "", tok.OpenID, tok.AccessToken)
	if err != nil {
		// Token ok but profile failed — still return token/open_id for caller.
		return tok.AccessToken, tok.OpenID, nil, err
	}
	return tok.AccessToken, tok.OpenID, prof, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

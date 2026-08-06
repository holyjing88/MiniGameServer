package auth

import (
	"context"
	"fmt"
	"strings"

	"minigameserver/internal/config"
)

// ModeSelector picks an auth mode name for a channel.
type ModeSelector interface {
	ModeForChannel(channel string) string
}

// MultiProfileProvider routes ProfileProvider calls by channel → auth mode.
type MultiProfileProvider struct {
	Select    ModeSelector
	Providers map[string]ProfileProvider // mode → provider
	Fallback  ProfileProvider            // usually MockProfileProvider
}

func NewMultiProfileProvider(sel ModeSelector, providers map[string]ProfileProvider, fallback ProfileProvider) *MultiProfileProvider {
	if providers == nil {
		providers = map[string]ProfileProvider{}
	}
	if fallback == nil {
		fallback = MockProfileProvider{}
	}
	return &MultiProfileProvider{Select: sel, Providers: providers, Fallback: fallback}
}

func (m *MultiProfileProvider) providerFor(channel string) ProfileProvider {
	mode := config.AuthModeMock
	if m.Select != nil {
		mode = m.Select.ModeForChannel(channel)
	}
	if p, ok := m.Providers[mode]; ok && p != nil {
		return p
	}
	if p, ok := m.Providers[config.AuthModeMock]; ok && p != nil {
		return p
	}
	return m.Fallback
}

func (m *MultiProfileProvider) FetchProfile(ctx context.Context, appID, channel, openID, accessToken string) (*UserProfile, error) {
	return m.providerFor(channel).FetchProfile(ctx, appID, channel, openID, accessToken)
}

func (m *MultiProfileProvider) ExchangeCode(ctx context.Context, appID, channel, code string) (string, string, *UserProfile, error) {
	return m.providerFor(channel).ExchangeCode(ctx, appID, channel, code)
}

// MultiResolver routes OpenIDResolver by channel → auth mode.
type MultiResolver struct {
	Select    ModeSelector
	Resolvers map[string]OpenIDResolver
	Fallback  OpenIDResolver
}

func NewMultiResolver(sel ModeSelector, resolvers map[string]OpenIDResolver, fallback OpenIDResolver) *MultiResolver {
	if resolvers == nil {
		resolvers = map[string]OpenIDResolver{}
	}
	if fallback == nil {
		fallback = MockResolver{}
	}
	return &MultiResolver{Select: sel, Resolvers: resolvers, Fallback: fallback}
}

func (m *MultiResolver) Resolve(appID, channel, code string) (string, error) {
	mode := config.AuthModeMock
	if m.Select != nil {
		mode = m.Select.ModeForChannel(channel)
	}
	if r, ok := m.Resolvers[mode]; ok && r != nil {
		return r.Resolve(appID, channel, code)
	}
	if r, ok := m.Resolvers[config.AuthModeMock]; ok && r != nil {
		return r.Resolve(appID, channel, code)
	}
	return m.Fallback.Resolve(appID, channel, code)
}

// TikTokResolver resolves login/register codes via TikTok OAuth exchange.
// Falls back to MockResolver when code has mock: prefix (local debug on tiktok channel).
type TikTokResolver struct {
	Provider ProfileProvider
	Mock     MockResolver
}

func (r TikTokResolver) Resolve(appID, channel, code string) (string, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return "", fmt.Errorf("empty code")
	}
	if strings.HasPrefix(code, "mock:") {
		return r.Mock.Resolve(appID, channel, code)
	}
	if r.Provider == nil {
		return "", fmt.Errorf("tiktok profile provider not configured")
	}
	_, openID, _, err := r.Provider.ExchangeCode(context.Background(), appID, channel, code)
	if err != nil && openID == "" {
		return "", err
	}
	if openID == "" {
		return "", fmt.Errorf("tiktok exchange returned empty open_id")
	}
	return openID, nil
}

// BuildAuthStack constructs multi-mode profile provider + resolver from config.
func BuildAuthStack(cfg config.Config) (ProfileProvider, OpenIDResolver) {
	providers := map[string]ProfileProvider{
		config.AuthModeMock: MockProfileProvider{},
	}
	resolvers := map[string]OpenIDResolver{
		config.AuthModeMock: MockResolver{},
	}

	if cfg.AuthModeEnabled(config.AuthModeTikTok) && cfg.TikTokReady() {
		tt := NewTikTokProfileProvider(cfg.TikTokClientKey, cfg.TikTokClientSecret)
		providers[config.AuthModeTikTok] = tt
		resolvers[config.AuthModeTikTok] = TikTokResolver{Provider: tt}
	}

	profiles := NewMultiProfileProvider(cfg, providers, MockProfileProvider{})
	resolver := NewMultiResolver(cfg, resolvers, MockResolver{})
	return profiles, resolver
}

package config

import "testing"

func TestParseAuthModes(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", []string{"mock"}},
		{"mock", []string{"mock"}},
		{"tiktok", []string{"tiktok"}},
		{"mock,tiktok", []string{"mock", "tiktok"}},
		{" tiktok , mock , tiktok ", []string{"tiktok", "mock"}},
		{"unknown,mock", []string{"mock"}},
	}
	for _, c := range cases {
		got := ParseAuthModes(c.in)
		if len(got) != len(c.want) {
			t.Fatalf("ParseAuthModes(%q)=%v want %v", c.in, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("ParseAuthModes(%q)=%v want %v", c.in, got, c.want)
			}
		}
	}
}

func TestModeForChannel_Defaults(t *testing.T) {
	cfg := Config{
		AuthModes:      []string{"mock", "tiktok"},
		TikTokClientKey: "k", TikTokClientSecret: "s",
	}
	if got := cfg.ModeForChannel("tiktok_minis"); got != AuthModeTikTok {
		t.Fatalf("tiktok_minis -> %s", got)
	}
	if got := cfg.ModeForChannel("douyin"); got != AuthModeTikTok {
		t.Fatalf("douyin -> %s", got)
	}
	if got := cfg.ModeForChannel("weixin"); got != AuthModeMock {
		t.Fatalf("weixin -> %s", got)
	}
}

func TestModeForChannel_CustomMapAndFallback(t *testing.T) {
	cfg := Config{
		AuthModes: []string{"mock", "tiktok"},
		AuthChannelMap: map[string]string{
			"weixin": "tiktok",
			"web":    "mock",
		},
		TikTokClientKey: "k", TikTokClientSecret: "s",
	}
	if got := cfg.ModeForChannel("weixin"); got != AuthModeTikTok {
		t.Fatalf("mapped weixin -> %s", got)
	}
	// tiktok not enabled → fallback mock
	cfg2 := Config{
		AuthModes:      []string{"mock"},
		AuthChannelMap: map[string]string{"tiktok_minis": "tiktok"},
		TikTokClientKey: "k", TikTokClientSecret: "s",
	}
	if got := cfg2.ModeForChannel("tiktok_minis"); got != AuthModeMock {
		t.Fatalf("disabled tiktok should fallback mock, got %s", got)
	}
	// credentials missing → fallback mock
	cfg3 := Config{
		AuthModes: []string{"mock", "tiktok"},
	}
	if got := cfg3.ModeForChannel("tiktok_minis"); got != AuthModeMock {
		t.Fatalf("no creds should fallback mock, got %s", got)
	}
}

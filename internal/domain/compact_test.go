package domain

import "testing"

func TestToCompact_NicknameAndAvatarFromExtra(t *testing.T) {
	e := Entry{
		PlayerID: "tiktok_minis_oid1",
		Score:    12,
		Extra:    []byte(`{"openid":"oid1","nickname":"mehak","avatar_url":"https://x/a.png"}`),
	}
	c := ToCompact(1, e)
	if c.R != 1 || c.P != e.PlayerID || c.S != 12 {
		t.Fatalf("base fields: %+v", c)
	}
	if c.N != "mehak" {
		t.Fatalf("nickname N=%q", c.N)
	}
	if c.A != "https://x/a.png" {
		t.Fatalf("avatar A=%q", c.A)
	}
}

func TestToCompact_NicknameAliases(t *testing.T) {
	cases := []struct {
		extra string
		want  string
	}{
		{`{"nick":"A"}`, "A"},
		{`{"display_name":"B"}`, "B"},
		{`{"username":"C"}`, "C"},
		{`{}`, ""},
		{"", ""},
		{"not-json", ""},
	}
	for _, c := range cases {
		got := ToCompact(2, Entry{PlayerID: "p", Score: 1, Extra: []byte(c.extra)}).N
		if got != c.want {
			t.Fatalf("extra=%q got=%q want=%q", c.extra, got, c.want)
		}
	}
}

func TestToCompact_AvatarAliases(t *testing.T) {
	cases := []struct {
		extra string
		want  string
	}{
		{`{"avatar":"https://a"}`, "https://a"},
		{`{"avatarUrl":"https://b"}`, "https://b"},
		{`{"head_url":"https://c"}`, "https://c"},
		{`{}`, ""},
	}
	for _, c := range cases {
		got := ToCompact(3, Entry{PlayerID: "p", Score: 1, Extra: []byte(c.extra)}).A
		if got != c.want {
			t.Fatalf("extra=%q got=%q want=%q", c.extra, got, c.want)
		}
	}
}

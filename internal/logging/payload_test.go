package logging

import (
	"strings"
	"testing"
)

func TestFormatPayloadAsIs(t *testing.T) {
	in := []byte(`{"app_id":"a1","password":"secret","access_token":"tok123","code":"abcdefghijklmnopqrstuvwxyz","score":10}`)
	out := FormatPayload(in)
	if out != string(in) {
		t.Fatalf("expected raw payload, got %s", out)
	}
}

func TestFormatPayloadTruncate(t *testing.T) {
	raw := strings.Repeat("a", maxLogPayload+100)
	out := FormatPayload([]byte(raw))
	if !strings.HasSuffix(out, "...(truncated)") {
		t.Fatalf("expected truncation suffix: %q", out[len(out)-20:])
	}
	body := strings.TrimSuffix(out, "...(truncated)")
	if len([]rune(body)) != maxLogPayload {
		t.Fatalf("unexpected truncated length: %d", len([]rune(body)))
	}
}

func TestFormatPayloadPlain(t *testing.T) {
	if got := FormatPayload([]byte("  hello  ")); got != "hello" {
		t.Fatalf("got %q", got)
	}
}

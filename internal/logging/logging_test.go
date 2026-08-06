package logging

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseLevel(t *testing.T) {
	cases := map[string]Level{
		"trace": LevelTrace,
		"DEBUG": LevelDebug,
		"info":  LevelInfo,
		"error": LevelError,
		"fatal": LevelFatal,
		"fatle": LevelFatal,
		"":      LevelInfo,
		"nope":  LevelInfo,
	}
	for in, want := range cases {
		if got := ParseLevel(in); got != want {
			t.Fatalf("ParseLevel(%q)=%v want %v", in, got, want)
		}
	}
}

func TestLevelFilter(t *testing.T) {
	var buf bytes.Buffer
	l := New(LevelInfo, &buf)
	l.Trace("hidden-trace")
	l.Debug("hidden-debug")
	l.Info("shown-info")
	l.Error("shown-error")
	out := buf.String()
	if strings.Contains(out, "hidden-trace") || strings.Contains(out, "hidden-debug") {
		t.Fatalf("trace/debug should be filtered: %s", out)
	}
	if !strings.Contains(out, "[INFO]") || !strings.Contains(out, "shown-info") {
		t.Fatalf("missing info: %s", out)
	}
	if !strings.Contains(out, "[ERROR]") || !strings.Contains(out, "shown-error") {
		t.Fatalf("missing error: %s", out)
	}
}

func TestTraceEnabled(t *testing.T) {
	var buf bytes.Buffer
	l := New(LevelTrace, &buf)
	l.Trace("t1")
	l.Debug("d1")
	out := buf.String()
	if !strings.Contains(out, "[TRACE]") || !strings.Contains(out, "t1") {
		t.Fatalf("want trace: %s", out)
	}
	if !strings.Contains(out, "[DEBUG]") {
		t.Fatalf("want debug: %s", out)
	}
}

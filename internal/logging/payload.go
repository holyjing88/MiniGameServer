package logging

import (
	"strings"
	"unicode/utf8"
)

const (
	// maxLogPayload is the max number of runes written for a single req/resp body.
	maxLogPayload = 8192
	// maxReadBody caps how much of an HTTP body we buffer to restore for the handler.
	maxReadBody = 1 << 20 // 1 MiB
)

// FormatPayload returns a single-line string suitable for logs.
// Content is printed as-is (no redaction); oversized payloads are truncated.
func FormatPayload(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	trimmed := bytesTrimSpace(b)
	if len(trimmed) == 0 {
		return ""
	}
	return truncateRunes(string(trimmed), maxLogPayload)
}

func bytesTrimSpace(b []byte) []byte {
	i, j := 0, len(b)
	for i < j && (b[i] == ' ' || b[i] == '\t' || b[i] == '\n' || b[i] == '\r') {
		i++
	}
	for j > i && (b[j-1] == ' ' || b[j-1] == '\t' || b[j-1] == '\n' || b[j-1] == '\r') {
		j--
	}
	return b[i:j]
}

func truncateRunes(s string, max int) string {
	if max <= 0 || s == "" {
		return s
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	var b strings.Builder
	b.Grow(max + 16)
	n := 0
	for _, r := range s {
		if n >= max {
			break
		}
		b.WriteRune(r)
		n++
	}
	b.WriteString("...(truncated)")
	return b.String()
}

package logging

import (
	"context"
	"crypto/rand"
	"encoding/hex"
)

type ctxKey int

const requestIDKey ctxKey = 1

// WithRequestID stores a request id on ctx.
func WithRequestID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, requestIDKey, id)
}

// RequestID returns the request id from ctx, or "".
func RequestID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(requestIDKey).(string); ok {
		return v
	}
	return ""
}

// NewRequestID returns a short random hex id.
func NewRequestID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "0000000000000000"
	}
	return hex.EncodeToString(b[:])
}

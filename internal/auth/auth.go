package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrInvalidToken  = errors.New("invalid session token")
	ErrExpiredToken  = errors.New("session expired")
	ErrNeedRegister  = errors.New("NEED_REGISTER")
	ErrAlreadyExists = errors.New("ALREADY_REGISTERED")
)

type Claims struct {
	AppID    string `json:"a"`
	Channel  string `json:"c"`
	OpenID   string `json:"o"`
	PlayerID string `json:"p"` // account_id = channel_openid
	Exp      int64  `json:"e"`
}

type SessionManager struct {
	secret []byte
	ttl    time.Duration
}

func NewSessionManager(secret string, ttl time.Duration) *SessionManager {
	return &SessionManager{secret: []byte(secret), ttl: ttl}
}

func (m *SessionManager) Issue(appID, channel, openID, accountID string, now time.Time) (token string, expiresIn int64, err error) {
	if accountID == "" {
		accountID = channel + "_" + openID
	}
	claims := Claims{
		AppID:    appID,
		Channel:  channel,
		OpenID:   openID,
		PlayerID: accountID,
		Exp:      now.Add(m.ttl).Unix(),
	}
	raw, err := json.Marshal(claims)
	if err != nil {
		return "", 0, err
	}
	payload := base64.RawURLEncoding.EncodeToString(raw)
	sig := m.sign(payload)
	return payload + "." + sig, int64(m.ttl.Seconds()), nil
}

func (m *SessionManager) Parse(token string, now time.Time) (*Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return nil, ErrInvalidToken
	}
	if !hmac.Equal([]byte(m.sign(parts[0])), []byte(parts[1])) {
		return nil, ErrInvalidToken
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, ErrInvalidToken
	}
	var c Claims
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, ErrInvalidToken
	}
	if c.PlayerID == "" || c.Exp <= now.Unix() {
		return nil, ErrExpiredToken
	}
	return &c, nil
}

func (m *SessionManager) sign(payload string) string {
	h := hmac.New(sha256.New, m.secret)
	_, _ = h.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

// ResolveOpenID maps login code to open_id.
// mock mode: code "mock:<open_id>" or plain open_id.
type OpenIDResolver interface {
	Resolve(appID, channel, code string) (openID string, err error)
}

type MockResolver struct{}

func (MockResolver) Resolve(_, _, code string) (string, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return "", fmt.Errorf("empty code")
	}
	if strings.HasPrefix(code, "mock:") {
		id := strings.TrimPrefix(code, "mock:")
		if id == "" {
			return "", fmt.Errorf("empty mock open_id")
		}
		return id, nil
	}
	return code, nil
}

type StaticServiceAuth struct {
	Token string
}

func (s StaticServiceAuth) Valid(token string) bool {
	return s.Token != "" && hmac.Equal([]byte(s.Token), []byte(token))
}

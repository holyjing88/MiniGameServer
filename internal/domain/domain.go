package domain

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"time"
)

type PeriodType string

const (
	PeriodDay   PeriodType = "day"
	PeriodWeek  PeriodType = "week"
	PeriodMonth PeriodType = "month"
	PeriodAll   PeriodType = "all"
	PeriodBoth  PeriodType = "both" // week+month write shortcut
	PeriodFull  PeriodType = "full" // day+week+month+all write shortcut
)

// RankType is the public ranking window: day | week | month | all.
type RankType = PeriodType

// RelationType mirrors Douyin ImRank: friend | all | default.
type RelationType string

const (
	RelationFriend  RelationType = "friend"
	RelationAll     RelationType = "all"
	RelationDefault RelationType = "default"
)

// Common channel IDs (extensible string — any non-empty id is accepted).
const (
	ChannelTikTokMinis   = "tiktok_minis"
	ChannelDouyin        = "douyin"
	ChannelWeixin        = "weixin"
	ChannelKuaishou      = "ks_minigame"
	ChannelAndroidGoogle = "android_google"
	ChannelWeb           = "web"
)

// AccountID uniquely identifies a player account: "{channel}_{open_id}".
func AccountID(channel, openID string) string {
	return channel + "_" + openID
}

// ParseAccountID splits "{channel}_{open_id}". open_id may contain underscores.
func ParseAccountID(accountID string) (channel, openID string, ok bool) {
	i := strings.Index(accountID, "_")
	if i <= 0 || i >= len(accountID)-1 {
		return "", "", false
	}
	return accountID[:i], accountID[i+1:], true
}

type BoardRef struct {
	AppID   string
	BoardID string
	ZoneID  string
	Channel string
}

type Entry struct {
	BoardKey  []byte
	PlayerID  string
	Score     int64
	UpdatedAt int64
	Extra     []byte
	// Denormalized metadata for MySQL rows (ignored by memory store).
	AppID     string
	BoardID   string
	ZoneID    string
	PeriodKey string
	Channel   string
}

type PeriodResult struct {
	Updated   bool  `json:"updated"`
	BestScore int64 `json:"best_score"`
	SelfRank  int32 `json:"self_rank"`
}

// PlayerRegister is a registered account keyed by (app_id, channel, open_id).
// AccountID / PlayerID = "{channel}_{open_id}".
type PlayerRegister struct {
	AppID          string
	Channel        string
	OpenID         string
	PlayerID       string // account_id = channel_openid
	Username       string
	AvatarURL      string // TikTok avatar / display avatar URL
	ClickID        string // promo attribution (ttclid / clickid), first-touch
	PlatformKind   int32
	RegisteredAtMs int64
	ExtraJSON      string
}

type ChannelCount struct {
	Channel string `json:"channel"`
	Count   int64  `json:"count"`
}

func NormalizeZone(z string) string {
	if strings.TrimSpace(z) == "" {
		return "default"
	}
	return strings.TrimSpace(z)
}

// NormalizeChannel trims channel_id. Empty is invalid for board isolation.
func NormalizeChannel(ch string) (string, error) {
	ch = strings.TrimSpace(ch)
	if ch == "" {
		return "", fmt.Errorf("channel / channel_id required")
	}
	return ch, nil
}

// FirstNonEmpty returns the first non-empty trimmed string (channel aliases).
func FirstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

func BoardKey(appID, boardID, zoneID, periodKey, channel string) []byte {
	zoneID = NormalizeZone(zoneID)
	raw := strings.Join([]string{appID, boardID, zoneID, periodKey, channel}, "|")
	sum := sha256.Sum256([]byte(raw))
	out := make([]byte, 16)
	copy(out, sum[:16])
	return out
}

func BoardKeyHex(key []byte) string {
	return fmt.Sprintf("%x", key)
}

// PeriodKey returns the bucket key for a rank window in loc.
// day=YYYY-MM-DD, week=YYYY-Www (ISO), month=YYYY-MM, all=constant "all".
func PeriodKey(pt PeriodType, now time.Time, loc *time.Location) (string, error) {
	t := now.In(loc)
	switch pt {
	case PeriodDay:
		return t.Format("2006-01-02"), nil
	case PeriodWeek:
		y, w := t.ISOWeek()
		return fmt.Sprintf("%04d-W%02d", y, w), nil
	case PeriodMonth:
		return t.Format("2006-01"), nil
	case PeriodAll:
		return "all", nil
	default:
		return "", fmt.Errorf("unsupported period_type %q", pt)
	}
}

func ParsePeriodType(s string) (PeriodType, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "both":
		return PeriodBoth, nil
	case "full", "*":
		return PeriodFull, nil
	case "day":
		return PeriodDay, nil
	case "week":
		return PeriodWeek, nil
	case "month":
		return PeriodMonth, nil
	case "all":
		return PeriodAll, nil
	default:
		return "", fmt.Errorf("invalid period_type %q", s)
	}
}

// ParseRankType parses query rankType (day|week|month|all). Empty defaults to week.
func ParseRankType(s string) (RankType, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "week":
		return PeriodWeek, nil
	case "day":
		return PeriodDay, nil
	case "month":
		return PeriodMonth, nil
	case "all":
		return PeriodAll, nil
	default:
		return "", fmt.Errorf("invalid rankType %q (want day|week|month|all)", s)
	}
}

// ParseWriteRankTypes resolves which windows to write.
// Accepts: empty/full/* → all four; both → week+month; day|week|month|all;
// comma-separated list e.g. "week,month,all".
func ParseWriteRankTypes(s string) ([]PeriodType, error) {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "full") || s == "*" {
		return []PeriodType{PeriodDay, PeriodWeek, PeriodMonth, PeriodAll}, nil
	}
	if strings.EqualFold(s, "both") {
		return []PeriodType{PeriodWeek, PeriodMonth}, nil
	}
	parts := strings.Split(s, ",")
	seen := map[PeriodType]bool{}
	var out []PeriodType
	for _, p := range parts {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "" {
			continue
		}
		var pt PeriodType
		switch p {
		case "day":
			pt = PeriodDay
		case "week":
			pt = PeriodWeek
		case "month":
			pt = PeriodMonth
		case "all":
			pt = PeriodAll
		case "both":
			for _, x := range []PeriodType{PeriodWeek, PeriodMonth} {
				if !seen[x] {
					seen[x] = true
					out = append(out, x)
				}
			}
			continue
		case "full", "*":
			return []PeriodType{PeriodDay, PeriodWeek, PeriodMonth, PeriodAll}, nil
		default:
			return nil, fmt.Errorf("invalid rankType %q in write list", p)
		}
		if !seen[pt] {
			seen[pt] = true
			out = append(out, pt)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("rankType write list empty")
	}
	return out, nil
}

// ParseRelationType parses ImRank relationType. Empty / default → all.
func ParseRelationType(s string) (RelationType, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "all", "default":
		return RelationAll, nil
	case "friend":
		return RelationFriend, nil
	default:
		return "", fmt.Errorf("invalid relationType %q", s)
	}
}

func LoadLocation(name string) *time.Location {
	if name == "" || strings.EqualFold(name, "UTC") {
		return time.UTC
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return loc
}

// CompactEntry is the short JSON shape for leaderboard payload.
type CompactEntry struct {
	R int32  `json:"r"`
	P string `json:"p"`
	S int64  `json:"s"`
}

func ToCompact(rank int32, e Entry) CompactEntry {
	return CompactEntry{R: rank, P: e.PlayerID, S: e.Score}
}

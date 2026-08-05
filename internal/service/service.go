package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"minigameserver/internal/auth"
	"minigameserver/internal/config"
	"minigameserver/internal/domain"
	"minigameserver/internal/hotcache"
	"minigameserver/internal/store"
)

type Service struct {
	cfg      config.Config
	store    store.Store
	cache    *hotcache.Cache
	sessions *auth.SessionManager
	resolver auth.OpenIDResolver
	svcAuth  auth.StaticServiceAuth
	loc      *time.Location
	now      func() time.Time
}

func New(cfg config.Config, st store.Store, cache *hotcache.Cache, sessions *auth.SessionManager, resolver auth.OpenIDResolver) *Service {
	return &Service{
		cfg:      cfg,
		store:    st,
		cache:    cache,
		sessions: sessions,
		resolver: resolver,
		svcAuth:  auth.StaticServiceAuth{Token: cfg.ServiceToken},
		loc:      domain.LoadLocation(cfg.Timezone),
		now:      time.Now,
	}
}

func (s *Service) SetNow(fn func() time.Time) { s.now = fn }

type CreateSessionInput struct {
	AppID   string
	Channel string
	Code    string
}

type CreateSessionOutput struct {
	SessionToken string `json:"session_token"`
	ExpiresIn    int64  `json:"expires_in"`
	PlayerID     string `json:"player_id"` // account_id = channel_openid
	AccountID    string `json:"account_id"`
	OpenID       string `json:"open_id"`
	Username     string `json:"username"`
	ClickID      string `json:"click_id"`
	Channel      string `json:"channel"`
	ChannelID    string `json:"channel_id"`
}

// NeedRegisterInfo is returned (via error wrapping / HTTP) when login finds no account.
type NeedRegisterInfo struct {
	OpenID    string `json:"open_id"`
	Channel   string `json:"channel"`
	ChannelID string `json:"channel_id"`
	AccountID string `json:"account_id"`
	AppID     string `json:"app_id"`
}

func (e *NeedRegisterInfo) Error() string { return auth.ErrNeedRegister.Error() }

// Login resolves code → open_id, requires existing registration, then issues session.
// Account uniqueness: account_id = "{channel_id}_{open_id}".
func (s *Service) Login(ctx context.Context, in CreateSessionInput) (*CreateSessionOutput, error) {
	ch, err := domain.NormalizeChannel(in.Channel)
	if err != nil {
		return nil, err
	}
	if in.AppID == "" {
		return nil, fmt.Errorf("app_id and channel required")
	}
	openID, err := s.resolver.Resolve(in.AppID, ch, in.Code)
	if err != nil {
		return nil, err
	}
	accountID := domain.AccountID(ch, openID)
	reg, ok, err := s.store.GetAccount(ctx, in.AppID, ch, openID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, &NeedRegisterInfo{
			OpenID: openID, Channel: ch, ChannelID: ch, AccountID: accountID, AppID: in.AppID,
		}
	}
	tok, exp, err := s.sessions.Issue(in.AppID, ch, openID, accountID, s.now())
	if err != nil {
		return nil, err
	}
	return &CreateSessionOutput{
		SessionToken: tok, ExpiresIn: exp,
		PlayerID: accountID, AccountID: accountID, OpenID: openID,
		Username: usernameOrExtra(reg), ClickID: clickIDOrExtra(reg),
		Channel: ch, ChannelID: ch,
	}, nil
}

func usernameOrExtra(reg domain.PlayerRegister) string {
	if strings.TrimSpace(reg.Username) != "" {
		return reg.Username
	}
	return stringFromExtra(reg.ExtraJSON, "username")
}

func clickIDOrExtra(reg domain.PlayerRegister) string {
	if strings.TrimSpace(reg.ClickID) != "" {
		return reg.ClickID
	}
	return stringFromExtra(reg.ExtraJSON, "click_id")
}

// CreateSession is an alias of Login (legacy path /v1/session).
func (s *Service) CreateSession(ctx context.Context, in CreateSessionInput) (*CreateSessionOutput, error) {
	return s.Login(ctx, in)
}

func (s *Service) ParseSession(token string) (*auth.Claims, error) {
	return s.sessions.Parse(token, s.now())
}

func (s *Service) CheckServiceToken(token string) bool {
	return s.svcAuth.Valid(token)
}

type UpsertInput struct {
	AppID      string
	BoardID    string
	ZoneID     string
	Channel    string
	PlayerID   string
	Score      int64
	PeriodType domain.PeriodType // both/full/week/month/day/all — write targets
	Extra      []byte
}

type UpsertOutput struct {
	Channel   string               `json:"channel"`
	ChannelID string               `json:"channel_id"`
	Day       *domain.PeriodResult `json:"day,omitempty"`
	Week      *domain.PeriodResult `json:"week,omitempty"`
	Month     *domain.PeriodResult `json:"month,omitempty"`
	All       *domain.PeriodResult `json:"all,omitempty"`
}

func (s *Service) UpsertMaxScore(ctx context.Context, in UpsertInput) (*UpsertOutput, error) {
	ch, err := domain.NormalizeChannel(in.Channel)
	if err != nil {
		return nil, err
	}
	in.Channel = ch
	if in.AppID == "" || in.BoardID == "" || in.PlayerID == "" {
		return nil, fmt.Errorf("missing required fields")
	}
	if in.Score < 0 {
		return nil, fmt.Errorf("score must be >= 0")
	}
	writeSpec := string(in.PeriodType)
	if writeSpec == "" {
		writeSpec = "both" // legacy /v1/score default
	}
	targets, err := domain.ParseWriteRankTypes(writeSpec)
	if err != nil {
		return nil, err
	}
	out := &UpsertOutput{Channel: ch, ChannelID: ch}
	now := s.now()
	for _, pt := range targets {
		pr, err := s.upsertOne(ctx, in, pt, now)
		if err != nil {
			return nil, err
		}
		assignPeriodResult(out, pt, pr)
	}
	return out, nil
}

func assignPeriodResult(out *UpsertOutput, pt domain.PeriodType, pr *domain.PeriodResult) {
	switch pt {
	case domain.PeriodDay:
		out.Day = pr
	case domain.PeriodWeek:
		out.Week = pr
	case domain.PeriodMonth:
		out.Month = pr
	case domain.PeriodAll:
		out.All = pr
	}
}

func (s *Service) upsertOne(ctx context.Context, in UpsertInput, pt domain.PeriodType, now time.Time) (*domain.PeriodResult, error) {
	pk, err := domain.PeriodKey(pt, now, s.loc)
	if err != nil {
		return nil, err
	}
	zone := domain.NormalizeZone(in.ZoneID)
	bk := domain.BoardKey(in.AppID, in.BoardID, zone, pk, in.Channel)
	e := domain.Entry{
		BoardKey:  bk,
		PlayerID:  in.PlayerID,
		Score:     in.Score,
		UpdatedAt: now.UnixMilli(),
		Extra:     in.Extra,
		AppID:     in.AppID,
		BoardID:   in.BoardID,
		ZoneID:    zone,
		PeriodKey: pk,
		Channel:   in.Channel,
	}
	updated, best, err := s.store.UpsertMax(ctx, e)
	if err != nil {
		return nil, err
	}
	// Dirty cache if may enter TopN.
	hex := domain.BoardKeyHex(bk)
	if it, ok := s.cache.Get(hex); ok {
		if best >= it.TopScoreMin || updated {
			s.cache.Invalidate(hex)
		}
	} else if updated {
		s.cache.Invalidate(hex)
	}
	rank, _, ok, err := s.store.RankOf(ctx, bk, in.PlayerID)
	if err != nil {
		return nil, err
	}
	pr := &domain.PeriodResult{Updated: updated, BestScore: best}
	if ok {
		pr.SelfRank = rank
	}
	return pr, nil
}

type LeaderboardInput struct {
	AppID      string
	BoardID    string
	ZoneID     string
	Channel    string
	PeriodType domain.PeriodType // query window = RankType
	TopN       int
	PlayerID   string
}

type LeaderboardOutput struct {
	Channel    string                `json:"channel"`
	ChannelID  string                `json:"channel_id"`
	RankType   string                `json:"rankType"`
	SnapshotTs int64                 `json:"snapshot_ts"`
	CacheHit   bool                  `json:"cache_hit"`
	SelfRank   int32                 `json:"self_rank"`
	SelfScore  int64                 `json:"self_score"`
	Encoding   string                `json:"encoding"`
	Entries    string                `json:"entries"` // base64(gzip(json))
	Items      []domain.CompactEntry `json:"items"`   // plain for Cocos (no pako)
	EntriesRaw []byte                `json:"-"`
}

func (s *Service) GetLeaderboard(ctx context.Context, in LeaderboardInput) (*LeaderboardOutput, error) {
	ch, err := domain.NormalizeChannel(in.Channel)
	if err != nil {
		return nil, err
	}
	in.Channel = ch
	switch in.PeriodType {
	case domain.PeriodDay, domain.PeriodWeek, domain.PeriodMonth, domain.PeriodAll:
	default:
		return nil, fmt.Errorf("rankType must be day, week, month, or all")
	}
	topN := in.TopN
	if topN <= 0 {
		topN = s.cfg.TopNDefault
	}
	if topN > s.cfg.TopNMax {
		topN = s.cfg.TopNMax
	}
	pk, err := domain.PeriodKey(in.PeriodType, s.now(), s.loc)
	if err != nil {
		return nil, err
	}
	zone := domain.NormalizeZone(in.ZoneID)
	bk := domain.BoardKey(in.AppID, in.BoardID, zone, pk, in.Channel)
	hex := domain.BoardKeyHex(bk)

	out := &LeaderboardOutput{
		Channel: ch, ChannelID: ch, RankType: string(in.PeriodType),
		Encoding: "gzip+base64",
	}
	if it, ok := s.cache.Get(hex); ok {
		out.CacheHit = true
		out.SnapshotTs = it.SnapshotTs
		out.EntriesRaw = it.EntriesGzip
		out.Entries = base64.StdEncoding.EncodeToString(it.EntriesGzip)
		out.Items = it.Entries
	} else {
		list, err := s.store.TopN(ctx, bk, topN)
		if err != nil {
			return nil, err
		}
		compact := make([]domain.CompactEntry, 0, len(list))
		for i, e := range list {
			compact = append(compact, domain.ToCompact(int32(i+1), e))
		}
		it, err := s.cache.Put(hex, compact, s.now())
		if err != nil {
			return nil, err
		}
		out.CacheHit = false
		out.SnapshotTs = it.SnapshotTs
		out.EntriesRaw = it.EntriesGzip
		out.Entries = base64.StdEncoding.EncodeToString(it.EntriesGzip)
		out.Items = compact
	}

	if in.PlayerID != "" {
		rank, score, ok, err := s.store.RankOf(ctx, bk, in.PlayerID)
		if err != nil {
			return nil, err
		}
		if ok {
			out.SelfRank = rank
			out.SelfScore = score
		}
	}
	return out, nil
}

// --- ImRank-aligned APIs (Douyin-compatible shapes for NO_NATIVE_RANK) ---

type SetImRankDataInput struct {
	AppID    string
	BoardID  string
	ZoneID   string
	Channel  string
	PlayerID string
	DataType int32
	Value    string
	Priority int32
	Extra    string
	// RankType: empty/full → day+week+month+all; or day|week|month|all|both|comma-list
	RankType string
	// RelationType is illegal on setImRankData — rejected if non-empty.
	RelationType string
}

type SetImRankDataOutput struct {
	Channel   string               `json:"channel"`
	ChannelID string               `json:"channel_id"`
	Day       *domain.PeriodResult `json:"day,omitempty"`
	Week      *domain.PeriodResult `json:"week,omitempty"`
	Month     *domain.PeriodResult `json:"month,omitempty"`
	All       *domain.PeriodResult `json:"all,omitempty"`
}

// SetImRankData writes clear progress into selected RankType windows (per channel).
func (s *Service) SetImRankData(ctx context.Context, in SetImRankDataInput) (*SetImRankDataOutput, error) {
	if strings.TrimSpace(in.RelationType) != "" {
		return nil, fmt.Errorf("relationType is not allowed on setImRankData")
	}
	ch, err := domain.NormalizeChannel(in.Channel)
	if err != nil {
		return nil, err
	}
	if in.AppID == "" || in.BoardID == "" || in.PlayerID == "" {
		return nil, fmt.Errorf("missing required fields")
	}
	score, err := parseImRankValue(in.DataType, in.Value, in.Priority)
	if err != nil {
		return nil, err
	}
	targets, err := domain.ParseWriteRankTypes(in.RankType)
	if err != nil {
		return nil, err
	}
	base := UpsertInput{
		AppID: in.AppID, BoardID: in.BoardID, ZoneID: in.ZoneID,
		Channel: ch, PlayerID: in.PlayerID, Score: score,
		Extra: []byte(in.Extra),
	}
	now := s.now()
	out := &SetImRankDataOutput{Channel: ch, ChannelID: ch}
	uo := &UpsertOutput{}
	for _, pt := range targets {
		pr, err := s.upsertOne(ctx, base, pt, now)
		if err != nil {
			return nil, err
		}
		assignPeriodResult(uo, pt, pr)
	}
	out.Day, out.Week, out.Month, out.All = uo.Day, uo.Week, uo.Month, uo.All
	return out, nil
}

type GetImRankListInput struct {
	AppID        string
	BoardID      string
	ZoneID       string
	Channel      string
	PlayerID     string
	RelationType string
	RankType     string
	DataType     int32
	Suffix       string
	RankTitle    string
	TopN         int
}

type GetImRankListOutput struct {
	Channel           string                `json:"channel"`
	ChannelID         string                `json:"channel_id"`
	RankType          string                `json:"rankType"`
	RelationType      string                `json:"relationType"`
	DataType          int32                 `json:"dataType"`
	ZoneID            string                `json:"zoneId"`
	Suffix            string                `json:"suffix"`
	RankTitle         string                `json:"rankTitle"`
	Display           string                `json:"display"`
	FriendUnsupported bool                  `json:"friend_unsupported,omitempty"`
	SnapshotTs        int64                 `json:"snapshot_ts"`
	CacheHit          bool                  `json:"cache_hit"`
	SelfRank          int32                 `json:"self_rank"`
	SelfScore         int64                 `json:"self_score"`
	Encoding          string                `json:"encoding"`
	Entries           string                `json:"entries"`
	Items             []domain.CompactEntry `json:"items"`
	EntriesRaw        []byte                `json:"-"`
}

func (s *Service) GetImRankList(ctx context.Context, in GetImRankListInput) (*GetImRankListOutput, error) {
	ch, err := domain.NormalizeChannel(in.Channel)
	if err != nil {
		return nil, err
	}
	rt, err := domain.ParseRankType(in.RankType)
	if err != nil {
		return nil, err
	}
	rel, err := domain.ParseRelationType(in.RelationType)
	if err != nil {
		return nil, err
	}
	suffix := in.Suffix
	if suffix == "" {
		suffix = "关"
	}
	title := in.RankTitle
	if title == "" {
		title = "通关排行榜"
	}
	out := &GetImRankListOutput{
		Channel: ch, ChannelID: ch,
		RankType: string(rt), RelationType: string(rel), DataType: in.DataType,
		ZoneID: domain.NormalizeZone(in.ZoneID), Suffix: suffix, RankTitle: title,
		Display: "custom", Encoding: "gzip+base64", Items: []domain.CompactEntry{},
	}
	if rel == domain.RelationFriend {
		out.FriendUnsupported = true
		out.SnapshotTs = s.now().UnixMilli()
		return out, nil
	}
	lb, err := s.GetLeaderboard(ctx, LeaderboardInput{
		AppID: in.AppID, BoardID: in.BoardID, ZoneID: in.ZoneID, Channel: ch,
		PeriodType: rt, TopN: in.TopN, PlayerID: in.PlayerID,
	})
	if err != nil {
		return nil, err
	}
	out.SnapshotTs = lb.SnapshotTs
	out.CacheHit = lb.CacheHit
	out.SelfRank = lb.SelfRank
	out.SelfScore = lb.SelfScore
	out.Entries = lb.Entries
	out.EntriesRaw = lb.EntriesRaw
	out.Items = lb.Items
	if out.Items == nil {
		out.Items = []domain.CompactEntry{}
	}
	return out, nil
}

type GetImRankDataInput struct {
	AppID        string
	BoardID      string
	ZoneID       string
	Channel      string
	PlayerID     string
	RelationType string
	RankType     string
	DataType     int32
	PageNum      int
	PageSize     int
}

type GetImRankDataOutput struct {
	Channel           string                `json:"channel"`
	ChannelID         string                `json:"channel_id"`
	RankType          string                `json:"rankType"`
	RelationType      string                `json:"relationType"`
	DataType          int32                 `json:"dataType"`
	ZoneID            string                `json:"zoneId"`
	PageNum           int                   `json:"pageNum"`
	PageSize          int                   `json:"pageSize"`
	FriendUnsupported bool                  `json:"friend_unsupported,omitempty"`
	SnapshotTs        int64                 `json:"snapshot_ts"`
	CacheHit          bool                  `json:"cache_hit"`
	SelfRank          int32                 `json:"self_rank"`
	SelfScore         int64                 `json:"self_score"`
	Items             []domain.CompactEntry `json:"items"`
	Total             int                   `json:"total"`
}

func (s *Service) GetImRankData(ctx context.Context, in GetImRankDataInput) (*GetImRankDataOutput, error) {
	ch, err := domain.NormalizeChannel(in.Channel)
	if err != nil {
		return nil, err
	}
	rt, err := domain.ParseRankType(in.RankType)
	if err != nil {
		return nil, err
	}
	rel, err := domain.ParseRelationType(in.RelationType)
	if err != nil {
		return nil, err
	}
	pageNum := in.PageNum
	if pageNum <= 0 {
		pageNum = 1
	}
	pageSize := in.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > s.cfg.TopNMax {
		pageSize = s.cfg.TopNMax
	}
	out := &GetImRankDataOutput{
		Channel: ch, ChannelID: ch,
		RankType: string(rt), RelationType: string(rel), DataType: in.DataType,
		ZoneID: domain.NormalizeZone(in.ZoneID), PageNum: pageNum, PageSize: pageSize,
		Items: []domain.CompactEntry{},
	}
	if rel == domain.RelationFriend {
		out.FriendUnsupported = true
		out.SnapshotTs = s.now().UnixMilli()
		return out, nil
	}
	need := pageNum * pageSize
	if need > s.cfg.TopNMax {
		need = s.cfg.TopNMax
	}
	lb, err := s.GetLeaderboard(ctx, LeaderboardInput{
		AppID: in.AppID, BoardID: in.BoardID, ZoneID: in.ZoneID, Channel: ch,
		PeriodType: rt, TopN: need, PlayerID: in.PlayerID,
	})
	if err != nil {
		return nil, err
	}
	out.SnapshotTs = lb.SnapshotTs
	out.CacheHit = lb.CacheHit
	out.SelfRank = lb.SelfRank
	out.SelfScore = lb.SelfScore
	all := lb.Items
	if all == nil {
		all = []domain.CompactEntry{}
	}
	out.Total = len(all)
	start := (pageNum - 1) * pageSize
	if start >= len(all) {
		out.Items = []domain.CompactEntry{}
		return out, nil
	}
	end := start + pageSize
	if end > len(all) {
		end = len(all)
	}
	out.Items = all[start:end]
	return out, nil
}

func parseImRankValue(dataType int32, value string, priority int32) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("value required")
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("value must be numeric string for dataType=%d", dataType)
	}
	if n < 0 {
		return 0, fmt.Errorf("value must be >= 0")
	}
	// dataType 1: platform uses priority for enum ordering; we still persist numeric value.
	_ = priority
	return n, nil
}

type RegisterInput struct {
	AppID        string
	Channel      string
	Code         string
	OpenID       string
	PlayerID     string
	Username     string
	ClickID      string // promo click id (ttclid / clickid), first-touch only
	PlatformKind int32
	ExtraJSON    string
}

type RegisterOutput struct {
	IsNew          bool   `json:"is_new"`
	Channel        string `json:"channel"`
	ChannelID      string `json:"channel_id"`
	OpenID         string `json:"open_id"`
	Username       string `json:"username"`
	ClickID        string `json:"click_id"`
	AccountID      string `json:"account_id"`
	PlayerID       string `json:"player_id"`
	RegisteredAtMs int64  `json:"registered_at_ms"`
	NeedLogin      bool   `json:"need_login"`
}

// Register creates account by channel_id + open_id (no session required).
// Registration fields (username, click_id, open_id, …) are stored on player_register.
// click_id is first-touch only (idempotent re-register does not overwrite).
func (s *Service) Register(ctx context.Context, in RegisterInput) (*RegisterOutput, error) {
	ch, err := domain.NormalizeChannel(in.Channel)
	if err != nil {
		return nil, err
	}
	if in.AppID == "" {
		return nil, fmt.Errorf("app_id, channel required")
	}
	openID := strings.TrimSpace(in.OpenID)
	if in.Code != "" {
		openID, err = s.resolver.Resolve(in.AppID, ch, in.Code)
		if err != nil {
			return nil, err
		}
	}
	if openID == "" {
		return nil, fmt.Errorf("code or open_id required")
	}
	username := strings.TrimSpace(in.Username)
	if utf8RuneCount(username) > 32 {
		return nil, fmt.Errorf("username too long (max 32)")
	}
	clickID := strings.TrimSpace(in.ClickID)
	if utf8RuneCount(clickID) > 256 {
		return nil, fmt.Errorf("click_id too long (max 256)")
	}
	if in.PlatformKind == 0 {
		in.PlatformKind = 1
	}
	accountID := domain.AccountID(ch, openID)
	extra := mergeAttrExtra(in.ExtraJSON, username, clickID)
	reg := domain.PlayerRegister{
		AppID:          in.AppID,
		Channel:        ch,
		OpenID:         openID,
		PlayerID:       accountID,
		Username:       username,
		ClickID:        clickID,
		PlatformKind:   in.PlatformKind,
		RegisteredAtMs: s.now().UnixMilli(),
		ExtraJSON:      extra,
	}
	isNew, attributed, err := s.store.ReportRegister(ctx, reg)
	if err != nil {
		return nil, err
	}
	aid := attributed.PlayerID
	if aid == "" {
		aid = domain.AccountID(attributed.Channel, openID)
	}
	return &RegisterOutput{
		IsNew:          isNew,
		Channel:        attributed.Channel,
		ChannelID:      attributed.Channel,
		OpenID:         openID,
		Username:       usernameOrExtra(attributed),
		ClickID:        clickIDOrExtra(attributed),
		AccountID:      aid,
		PlayerID:       aid,
		RegisteredAtMs: attributed.RegisteredAtMs,
		NeedLogin:      true,
	}, nil
}

func utf8RuneCount(s string) int {
	return len([]rune(s))
}

func mergeAttrExtra(extra, username, clickID string) string {
	m := map[string]interface{}{}
	if strings.TrimSpace(extra) != "" {
		_ = json.Unmarshal([]byte(extra), &m)
	}
	if username != "" {
		m["username"] = username
	}
	if clickID != "" {
		m["click_id"] = clickID
		m["clickid"] = clickID
	}
	b, err := json.Marshal(m)
	if err != nil {
		return extra
	}
	return string(b)
}

func stringFromExtra(extra, key string) string {
	if strings.TrimSpace(extra) == "" {
		return ""
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(extra), &m); err != nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return v
	}
	if key == "click_id" {
		if v, ok := m["clickid"].(string); ok {
			return v
		}
	}
	return ""
}

// ReportPlayerRegister is legacy name; prefers Code when set, else PlayerID as open_id (session path).
func (s *Service) ReportPlayerRegister(ctx context.Context, in RegisterInput) (*RegisterOutput, error) {
	if in.Code == "" && in.OpenID == "" && in.PlayerID != "" {
		// Session path: PlayerID may already be account_id — prefer OpenID from caller.
		if ch, oid, ok := domain.ParseAccountID(in.PlayerID); ok && ch == in.Channel {
			in.OpenID = oid
		} else {
			in.OpenID = in.PlayerID
		}
	}
	return s.Register(ctx, in)
}

type RegisterStatsOutput struct {
	Total     int64                 `json:"total"`
	ByChannel []domain.ChannelCount `json:"by_channel"`
}

func (s *Service) GetRegisterStats(ctx context.Context, appID string) (*RegisterStatsOutput, error) {
	if appID == "" {
		return nil, fmt.Errorf("app_id required")
	}
	total, rows, err := s.store.CountByChannel(ctx, appID)
	if err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []domain.ChannelCount{}
	}
	return &RegisterStatsOutput{Total: total, ByChannel: rows}, nil
}

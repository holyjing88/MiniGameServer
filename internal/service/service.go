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
	profiles auth.ProfileProvider
	svcAuth  auth.StaticServiceAuth
	loc      *time.Location
	now      func() time.Time
}

func New(cfg config.Config, st store.Store, cache *hotcache.Cache, sessions *auth.SessionManager, resolver auth.OpenIDResolver) *Service {
	profiles, builtResolver := auth.BuildAuthStack(cfg)
	if resolver == nil {
		resolver = builtResolver
	}
	return &Service{
		cfg:      cfg,
		store:    st,
		cache:    cache,
		sessions: sessions,
		resolver: resolver,
		profiles: profiles,
		svcAuth:  auth.StaticServiceAuth{Token: cfg.ServiceToken},
		loc:      domain.LoadLocation(cfg.Timezone),
		now:      time.Now,
	}
}

// NewWithAuth is like New but allows injecting a prebuilt auth stack (tests).
func NewWithAuth(cfg config.Config, st store.Store, cache *hotcache.Cache, sessions *auth.SessionManager, resolver auth.OpenIDResolver, profiles auth.ProfileProvider) *Service {
	if resolver == nil || profiles == nil {
		p, r := auth.BuildAuthStack(cfg)
		if profiles == nil {
			profiles = p
		}
		if resolver == nil {
			resolver = r
		}
	}
	return &Service{
		cfg:      cfg,
		store:    st,
		cache:    cache,
		sessions: sessions,
		resolver: resolver,
		profiles: profiles,
		svcAuth:  auth.StaticServiceAuth{Token: cfg.ServiceToken},
		loc:      domain.LoadLocation(cfg.Timezone),
		now:      time.Now,
	}
}

func (s *Service) SetNow(fn func() time.Time) { s.now = fn }

func (s *Service) SetProfileProvider(p auth.ProfileProvider) {
	if p != nil {
		s.profiles = p
	}
}

func (s *Service) DefaultAppID() string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(s.cfg.DefaultAppID)
}

// AuthModeForChannel returns the effective auth mode for ops/health checks.
func (s *Service) AuthModeForChannel(channel string) string {
	if s == nil {
		return ""
	}
	return s.cfg.ModeForChannel(channel)
}

// TikTokReady reports whether TikTok client credentials are configured.
func (s *Service) TikTokReady() bool {
	if s == nil {
		return false
	}
	return s.cfg.TikTokReady()
}

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
	Nickname     string `json:"nickname"`
	AvatarURL    string `json:"avatar_url"`
	Avatar       string `json:"avatar"`
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
	nick := usernameOrExtra(reg)
	avatar := avatarOrExtra(reg)
	return &CreateSessionOutput{
		SessionToken: tok, ExpiresIn: exp,
		PlayerID: accountID, AccountID: accountID, OpenID: openID,
		Username: nick, Nickname: nick, AvatarURL: avatar, Avatar: avatar,
		ClickID: clickIDOrExtra(reg),
		Channel: ch, ChannelID: ch,
	}, nil
}

func usernameOrExtra(reg domain.PlayerRegister) string {
	if strings.TrimSpace(reg.Username) != "" {
		return reg.Username
	}
	if v := stringFromExtra(reg.ExtraJSON, "username"); v != "" {
		return v
	}
	return stringFromExtra(reg.ExtraJSON, "nickname")
}

func avatarOrExtra(reg domain.PlayerRegister) string {
	if strings.TrimSpace(reg.AvatarURL) != "" {
		return reg.AvatarURL
	}
	if v := stringFromExtra(reg.ExtraJSON, "avatar_url"); v != "" {
		return v
	}
	return stringFromExtra(reg.ExtraJSON, "avatar")
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
	AvatarURL    string
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
	Nickname       string `json:"nickname"`
	AvatarURL      string `json:"avatar_url"`
	Avatar         string `json:"avatar"`
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
	avatarURL := strings.TrimSpace(in.AvatarURL)
	if utf8RuneCount(avatarURL) > 512 {
		return nil, fmt.Errorf("avatar_url too long (max 512)")
	}
	clickID := strings.TrimSpace(in.ClickID)
	if utf8RuneCount(clickID) > 256 {
		return nil, fmt.Errorf("click_id too long (max 256)")
	}
	if in.PlatformKind == 0 {
		in.PlatformKind = 1
	}
	accountID := domain.AccountID(ch, openID)
	extra := mergeAttrExtra(in.ExtraJSON, username, clickID, avatarURL)
	reg := domain.PlayerRegister{
		AppID:          in.AppID,
		Channel:        ch,
		OpenID:         openID,
		PlayerID:       accountID,
		Username:       username,
		AvatarURL:      avatarURL,
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
	nick := usernameOrExtra(attributed)
	if nick == "" {
		nick = username
	}
	av := avatarOrExtra(attributed)
	if av == "" {
		av = avatarURL
	}
	return &RegisterOutput{
		IsNew:          isNew,
		Channel:        attributed.Channel,
		ChannelID:      attributed.Channel,
		OpenID:         openID,
		Username:       nick,
		Nickname:       nick,
		AvatarURL:      av,
		Avatar:         av,
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

func mergeAttrExtra(extra, username, clickID, avatarURL string) string {
	m := map[string]interface{}{}
	if strings.TrimSpace(extra) != "" {
		_ = json.Unmarshal([]byte(extra), &m)
	}
	if username != "" {
		m["username"] = username
		m["nickname"] = username
	}
	if clickID != "" {
		m["click_id"] = clickID
		m["clickid"] = clickID
	}
	if avatarURL != "" {
		m["avatar_url"] = avatarURL
		m["avatar"] = avatarURL
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

// PlayerProfile is returned to TikTok / Cocos clients for nickname + avatar display.
type PlayerProfile struct {
	AppID     string `json:"app_id"`
	Channel   string `json:"channel"`
	ChannelID string `json:"channel_id"`
	OpenID    string `json:"open_id"`
	AccountID string `json:"account_id"`
	PlayerID  string `json:"player_id"`
	Username  string `json:"username"`
	Nickname  string `json:"nickname"`
	AvatarURL string `json:"avatar_url"`
	Avatar    string `json:"avatar"`
	Source    string `json:"source,omitempty"` // cache | sync | update
}

type SyncProfileInput struct {
	AppID       string
	Channel     string
	OpenID      string
	AccessToken string // TikTok access_token from client / OAuth
	Code        string // optional: server exchanges code then fetches profile
}

type UpdateProfileInput struct {
	AppID     string
	Channel   string
	OpenID    string
	Username  string
	Nickname  string
	AvatarURL string
	Avatar    string
}

func (s *Service) GetPlayerProfile(ctx context.Context, appID, channel, openID string) (*PlayerProfile, error) {
	ch, err := domain.NormalizeChannel(channel)
	if err != nil {
		return nil, err
	}
	if appID == "" || openID == "" {
		return nil, fmt.Errorf("app_id and open_id required")
	}
	reg, ok, err := s.store.GetAccount(ctx, appID, ch, openID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("account not found")
	}
	return profileFromReg(reg, "cache"), nil
}

// FetchTTProfileInput is the body for POST /api/v1/tiktok/profile.
// ClientKey / AppID come from each game client — server must not hardcode a single RANK_TT_CLIENT_KEY.
type FetchTTProfileInput struct {
	Code      string
	Channel   string
	ClientKey string // TikTok client_key (required for real OAuth)
	AppID     string // TikTok portal / platform app id (optional bookkeeping)
	MiniAppID string // game business app_id (e.g. parking_smart_brain)
}

// FetchProfileByAuthCode exchanges a TikTok authorize/login code for nickname + avatar.
// No session or registered account required — used by games that only need profile display
// (e.g. VampireDormitory main-menu avatar) before full MiniGameServer login.
func (s *Service) FetchProfileByAuthCode(ctx context.Context, in FetchTTProfileInput) (*PlayerProfile, error) {
	code := strings.TrimSpace(in.Code)
	if code == "" {
		return nil, fmt.Errorf("auth_code required")
	}
	ch := strings.TrimSpace(in.Channel)
	if ch == "" {
		ch = domain.ChannelTikTokMinis
	}
	ch, err := domain.NormalizeChannel(ch)
	if err != nil {
		return nil, err
	}
	appID := domain.FirstNonEmpty(in.MiniAppID, in.AppID, s.cfg.DefaultAppID)
	appID = strings.TrimSpace(appID)

	var provider auth.ProfileProvider
	mode := s.cfg.ModeForChannel(ch)
	if mode == config.AuthModeTikTok {
		ck := strings.TrimSpace(in.ClientKey)
		if ck == "" {
			return nil, fmt.Errorf("client_key required (pass from game client; do not rely on server RANK_TT_CLIENT_KEY)")
		}
		secret, ok := s.cfg.SecretForClientKey(ck)
		if !ok {
			return nil, fmt.Errorf("client_key not registered on server (add to RANK_TT_CLIENT_SECRETS)")
		}
		provider = auth.NewTikTokProfileProvider(ck, secret)
	} else {
		if s.profiles == nil {
			return nil, fmt.Errorf("profile provider not configured")
		}
		provider = s.profiles
	}

	_, oid, prof, exErr := provider.ExchangeCode(ctx, appID, ch, code)
	if exErr != nil && prof == nil {
		return nil, fmt.Errorf("profile exchange: %w", exErr)
	}
	if prof == nil {
		return nil, fmt.Errorf("empty profile from provider")
	}
	nick := strings.TrimSpace(prof.Nickname)
	avatar := strings.TrimSpace(prof.AvatarURL)
	if nick == "" && avatar == "" {
		return nil, fmt.Errorf("empty nickname/avatar from provider")
	}
	if oid == "" {
		oid = strings.TrimSpace(prof.OpenID)
	}
	accountID := ""
	if oid != "" {
		accountID = domain.AccountID(ch, oid)
	}
	return &PlayerProfile{
		AppID:     appID,
		Channel:   ch,
		ChannelID: ch,
		OpenID:    oid,
		AccountID: accountID,
		PlayerID:  accountID,
		Username:  nick,
		Nickname:  nick,
		AvatarURL: avatar,
		Avatar:    avatar,
		Source:    "oauth",
	}, nil
}

// SyncPlayerProfile fetches nickname/avatar from channel platform (TikTok or mock) and persists.
func (s *Service) SyncPlayerProfile(ctx context.Context, in SyncProfileInput) (*PlayerProfile, error) {
	ch, err := domain.NormalizeChannel(in.Channel)
	if err != nil {
		return nil, err
	}
	if in.AppID == "" || in.OpenID == "" {
		return nil, fmt.Errorf("app_id and open_id required")
	}
	if _, ok, err := s.store.GetAccount(ctx, in.AppID, ch, in.OpenID); err != nil {
		return nil, err
	} else if !ok {
		return nil, fmt.Errorf("account not found")
	}

	accessToken := strings.TrimSpace(in.AccessToken)
	var fetched *auth.UserProfile
	if code := strings.TrimSpace(in.Code); code != "" && s.profiles != nil {
		tok, oid, prof, exErr := s.profiles.ExchangeCode(ctx, in.AppID, ch, code)
		if exErr != nil && prof == nil {
			return nil, fmt.Errorf("profile exchange: %w", exErr)
		}
		if tok != "" {
			accessToken = tok
		}
		if oid != "" && oid != in.OpenID {
			// Keep session open_id authoritative; still use fetched profile fields.
		}
		fetched = prof
		_ = exErr
	}
	if fetched == nil {
		if s.profiles == nil {
			return nil, fmt.Errorf("profile provider not configured")
		}
		fetched, err = s.profiles.FetchProfile(ctx, in.AppID, ch, in.OpenID, accessToken)
		if err != nil {
			return nil, fmt.Errorf("profile fetch: %w", err)
		}
	}
	nick := strings.TrimSpace(fetched.Nickname)
	avatar := strings.TrimSpace(fetched.AvatarURL)
	if nick == "" && avatar == "" {
		return nil, fmt.Errorf("empty profile from provider")
	}
	updated, err := s.store.UpdateProfile(ctx, in.AppID, ch, in.OpenID, nick, avatar)
	if err != nil {
		return nil, err
	}
	return profileFromReg(updated, "sync"), nil
}

// UpdatePlayerProfile lets the client push nickname/avatar (e.g. from TTMinisSDK.getUserInfo).
func (s *Service) UpdatePlayerProfile(ctx context.Context, in UpdateProfileInput) (*PlayerProfile, error) {
	ch, err := domain.NormalizeChannel(in.Channel)
	if err != nil {
		return nil, err
	}
	if in.AppID == "" || in.OpenID == "" {
		return nil, fmt.Errorf("app_id and open_id required")
	}
	nick := domain.FirstNonEmpty(in.Nickname, in.Username)
	avatar := domain.FirstNonEmpty(in.AvatarURL, in.Avatar)
	if nick == "" && avatar == "" {
		return nil, fmt.Errorf("nickname or avatar required")
	}
	if utf8RuneCount(nick) > 32 {
		return nil, fmt.Errorf("nickname too long (max 32)")
	}
	if utf8RuneCount(avatar) > 512 {
		return nil, fmt.Errorf("avatar_url too long (max 512)")
	}
	updated, err := s.store.UpdateProfile(ctx, in.AppID, ch, in.OpenID, nick, avatar)
	if err != nil {
		return nil, err
	}
	return profileFromReg(updated, "update"), nil
}

func profileFromReg(reg domain.PlayerRegister, source string) *PlayerProfile {
	aid := reg.PlayerID
	if aid == "" {
		aid = domain.AccountID(reg.Channel, reg.OpenID)
	}
	nick := usernameOrExtra(reg)
	avatar := avatarOrExtra(reg)
	return &PlayerProfile{
		AppID:     reg.AppID,
		Channel:   reg.Channel,
		ChannelID: reg.Channel,
		OpenID:    reg.OpenID,
		AccountID: aid,
		PlayerID:  aid,
		Username:  nick,
		Nickname:  nick,
		AvatarURL: avatar,
		Avatar:    avatar,
		Source:    source,
	}
}

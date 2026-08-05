package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"minigameserver/internal/auth"
	"minigameserver/internal/domain"
	"minigameserver/internal/service"
)

type Server struct {
	svc *service.Service
	mux *http.ServeMux
}

func New(svc *service.Service) *Server {
	s := &Server{svc: svc, mux: http.NewServeMux()}
	// Canonical routes are under /api/... ; bare /v1/... kept as legacy aliases.
	reg := func(path string, h http.HandlerFunc) {
		s.mux.HandleFunc("/api"+path, h)
		s.mux.HandleFunc(path, h)
	}
	reg("/healthz", s.handleHealth)
	reg("/v1/auth/login", s.handleLogin)
	reg("/v1/auth/register", s.handleRegister)
	reg("/v1/session", s.handleLogin) // legacy alias → login
	reg("/v1/score", s.handleScore)
	reg("/v1/leaderboard", s.handleLeaderboard)
	reg("/v1/imrank/setImRankData", s.handleSetImRankData)
	reg("/v1/imrank/getImRankList", s.handleGetImRankList)
	reg("/v1/imrank/getImRankData", s.handleGetImRankData)
	reg("/v1/player/register", s.handleRegister) // legacy alias → register
	reg("/v1/player/profile", s.handlePlayerProfile)
	reg("/v1/player/profile/sync", s.handlePlayerProfileSync)
	// Public: authorize/login code → nickname/avatar (no session). For VampireDormitory etc.
	reg("/v1/tiktok/profile", s.handleTikTokProfile)
	reg("/v1/auth/tt_profile", s.handleTikTokProfile)
	reg("/v1/stats/register", s.handleRegisterStats)
	return s
}

func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "METHOD", "POST required")
		return
	}
	var body struct {
		AppID     string `json:"app_id"`
		Channel   string `json:"channel"`
		ChannelID string `json:"channel_id"`
		ChannelId string `json:"channelId"`
		Code      string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "BAD_REQUEST", "invalid json")
		return
	}
	ch := domain.FirstNonEmpty(body.ChannelID, body.ChannelId, body.Channel)
	out, err := s.svc.Login(r.Context(), service.CreateSessionInput{
		AppID: body.AppID, Channel: ch, Code: body.Code,
	})
	if err != nil {
		if nr, ok := err.(*service.NeedRegisterInfo); ok {
			writeJSON(w, http.StatusNotFound, map[string]interface{}{
				"code":       "NEED_REGISTER",
				"msg":        "account not registered; call /api/v1/auth/register then login",
				"open_id":    nr.OpenID,
				"channel":    nr.Channel,
				"channel_id": nr.ChannelID,
				"account_id": nr.AccountID,
				"app_id":     nr.AppID,
			})
			return
		}
		writeErr(w, http.StatusBadRequest, "LOGIN_FAIL", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "METHOD", "POST required")
		return
	}
	var body struct {
		AppID        string `json:"app_id"`
		Channel      string `json:"channel"`
		ChannelID    string `json:"channel_id"`
		ChannelId    string `json:"channelId"`
		Code         string `json:"code"`
		OpenID       string `json:"open_id"`
		Username     string `json:"username"`
		Nickname     string `json:"nickname"`
		AvatarURL    string `json:"avatar_url"`
		Avatar       string `json:"avatar"`
		ClickID      string `json:"click_id"`
		ClickId      string `json:"clickid"`
		Ttclid       string `json:"ttclid"`
		PlatformKind int32  `json:"platform_kind"`
		ExtraJSON    string `json:"extra_json"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "BAD_REQUEST", "invalid json")
		return
	}
	ch := domain.FirstNonEmpty(body.ChannelID, body.ChannelId, body.Channel)
	clickID := domain.FirstNonEmpty(body.ClickID, body.ClickId, body.Ttclid)
	username := domain.FirstNonEmpty(body.Username, body.Nickname)
	avatar := domain.FirstNonEmpty(body.AvatarURL, body.Avatar)
	// Optional session: if present, may supply open_id from claims when code omitted.
	if body.Code == "" && body.OpenID == "" {
		if claims, ok := s.trySession(r); ok {
			body.OpenID = claims.OpenID
			if body.AppID == "" {
				body.AppID = claims.AppID
			}
			if ch == "" {
				ch = claims.Channel
			}
		}
	}
	out, err := s.svc.Register(r.Context(), service.RegisterInput{
		AppID: body.AppID, Channel: ch, Code: body.Code, OpenID: body.OpenID,
		Username: username, AvatarURL: avatar, ClickID: clickID,
		PlatformKind: body.PlatformKind, ExtraJSON: body.ExtraJSON,
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, "REGISTER_FAIL", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) trySession(r *http.Request) (*auth.Claims, bool) {
	h := r.Header.Get("Authorization")
	token := ""
	if strings.HasPrefix(strings.ToLower(h), "bearer ") {
		token = strings.TrimSpace(h[7:])
	}
	if token == "" {
		token = r.Header.Get("X-Rank-Session")
	}
	if token == "" {
		return nil, false
	}
	claims, err := s.svc.ParseSession(token)
	if err != nil {
		return nil, false
	}
	return claims, true
}

func (s *Server) handleScore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "METHOD", "POST required")
		return
	}
	claims, ok := s.requireSession(w, r)
	if !ok {
		return
	}
	var body struct {
		AppID      string `json:"app_id"`
		BoardID    string `json:"board_id"`
		ZoneID     string `json:"zone_id"`
		ZoneId     string `json:"zoneId"`
		Channel    string `json:"channel"`
		ChannelID  string `json:"channel_id"`
		ChannelId  string `json:"channelId"`
		Score      int64  `json:"score"`
		PeriodType string `json:"period_type"`
		RankType   string `json:"rankType"`
		RankType2  string `json:"rank_type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "BAD_REQUEST", "invalid json")
		return
	}
	if body.AppID == "" {
		body.AppID = claims.AppID
	}
	ch := domain.FirstNonEmpty(body.ChannelID, body.ChannelId, body.Channel, claims.Channel)
	if err := s.ensureAppChannel(w, claims, body.AppID, ch); err != nil {
		return
	}
	writeSpec := domain.FirstNonEmpty(body.RankType, body.RankType2, body.PeriodType)
	if writeSpec == "" {
		writeSpec = "both"
	}
	pt := domain.PeriodType(writeSpec)
	zone := domain.FirstNonEmpty(body.ZoneID, body.ZoneId)
	out, err := s.svc.UpsertMaxScore(r.Context(), service.UpsertInput{
		AppID: body.AppID, BoardID: body.BoardID, ZoneID: zone,
		Channel: ch, PlayerID: claims.PlayerID, Score: body.Score, PeriodType: pt,
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, "SCORE_FAIL", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleLeaderboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "METHOD", "GET or POST required")
		return
	}
	claims, ok := s.requireSession(w, r)
	if !ok {
		return
	}
	var appID, boardID, zoneID, channel, rankType string
	var topN int
	if r.Method == http.MethodPost {
		var body struct {
			AppID     string `json:"app_id"`
			BoardID   string `json:"board_id"`
			ZoneID    string `json:"zone_id"`
			ZoneId    string `json:"zoneId"`
			Channel   string `json:"channel"`
			ChannelID string `json:"channel_id"`
			ChannelId string `json:"channelId"`
			RankType  string `json:"rankType"`
			RankType2 string `json:"rank_type"`
			Period    string `json:"period_type"`
			TopN      int    `json:"top_n"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, "BAD_REQUEST", "invalid json")
			return
		}
		appID, boardID = body.AppID, body.BoardID
		zoneID = domain.FirstNonEmpty(body.ZoneID, body.ZoneId)
		channel = domain.FirstNonEmpty(body.ChannelID, body.ChannelId, body.Channel)
		rankType = domain.FirstNonEmpty(body.RankType, body.RankType2, body.Period)
		topN = body.TopN
	} else {
		q := r.URL.Query()
		appID = q.Get("app_id")
		boardID = q.Get("board_id")
		zoneID = domain.FirstNonEmpty(q.Get("zoneId"), q.Get("zone_id"))
		channel = domain.FirstNonEmpty(q.Get("channel_id"), q.Get("channelId"), q.Get("channel"))
		rankType = domain.FirstNonEmpty(q.Get("rankType"), q.Get("rank_type"), q.Get("period_type"))
		topN, _ = strconv.Atoi(q.Get("top_n"))
	}
	if appID == "" {
		appID = claims.AppID
	}
	if channel == "" {
		channel = claims.Channel
	}
	if err := s.ensureAppChannel(w, claims, appID, channel); err != nil {
		return
	}
	rt, err := domain.ParseRankType(rankType)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	out, err := s.svc.GetLeaderboard(r.Context(), service.LeaderboardInput{
		AppID: appID, BoardID: boardID, ZoneID: zoneID,
		Channel: channel, PeriodType: rt, TopN: topN, PlayerID: claims.PlayerID,
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, "LB_FAIL", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleSetImRankData(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "METHOD", "POST required")
		return
	}
	claims, ok := s.requireSession(w, r)
	if !ok {
		return
	}
	var body struct {
		AppID        string `json:"app_id"`
		BoardID      string `json:"board_id"`
		ZoneID       string `json:"zoneId"`
		ZoneIDAlt    string `json:"zone_id"`
		Channel      string `json:"channel"`
		ChannelID    string `json:"channel_id"`
		ChannelId    string `json:"channelId"`
		DataType     int32  `json:"dataType"`
		Value        string `json:"value"`
		Priority     int32  `json:"priority"`
		Extra        string `json:"extra"`
		RankType     string `json:"rankType"`
		RankType2    string `json:"rank_type"`
		RelationType string `json:"relationType"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "BAD_REQUEST", "invalid json")
		return
	}
	if body.AppID == "" {
		body.AppID = claims.AppID
	}
	ch := domain.FirstNonEmpty(body.ChannelID, body.ChannelId, body.Channel, claims.Channel)
	if err := s.ensureAppChannel(w, claims, body.AppID, ch); err != nil {
		return
	}
	zone := domain.FirstNonEmpty(body.ZoneID, body.ZoneIDAlt)
	out, err := s.svc.SetImRankData(r.Context(), service.SetImRankDataInput{
		AppID: body.AppID, BoardID: body.BoardID, ZoneID: zone, Channel: ch,
		PlayerID: claims.PlayerID, DataType: body.DataType, Value: body.Value,
		Priority: body.Priority, Extra: body.Extra,
		RankType: domain.FirstNonEmpty(body.RankType, body.RankType2),
		RelationType: body.RelationType,
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, "SET_IMRANK_FAIL", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetImRankList(w http.ResponseWriter, r *http.Request) {
	claims, ok := s.requireSession(w, r)
	if !ok {
		return
	}
	in, err := s.parseImRankQuery(r, claims)
	if err != nil {
		writeImRankQueryErr(w, err)
		return
	}
	out, err := s.svc.GetImRankList(r.Context(), service.GetImRankListInput{
		AppID: in.appID, BoardID: in.boardID, ZoneID: in.zoneID, Channel: in.channel,
		PlayerID: claims.PlayerID, RelationType: in.relationType, RankType: in.rankType,
		DataType: in.dataType, Suffix: in.suffix, RankTitle: in.rankTitle, TopN: in.topN,
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, "GET_IMRANK_LIST_FAIL", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetImRankData(w http.ResponseWriter, r *http.Request) {
	claims, ok := s.requireSession(w, r)
	if !ok {
		return
	}
	in, err := s.parseImRankQuery(r, claims)
	if err != nil {
		writeImRankQueryErr(w, err)
		return
	}
	out, err := s.svc.GetImRankData(r.Context(), service.GetImRankDataInput{
		AppID: in.appID, BoardID: in.boardID, ZoneID: in.zoneID, Channel: in.channel,
		PlayerID: claims.PlayerID, RelationType: in.relationType, RankType: in.rankType,
		DataType: in.dataType, PageNum: in.pageNum, PageSize: in.pageSize,
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, "GET_IMRANK_DATA_FAIL", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func writeImRankQueryErr(w http.ResponseWriter, err error) {
	if err == errAppChannelMismatch {
		writeErr(w, http.StatusForbidden, "FORBIDDEN", err.Error())
		return
	}
	if err == errMethodNotAllowed {
		writeErr(w, http.StatusMethodNotAllowed, "METHOD", err.Error())
		return
	}
	writeErr(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
}

type imRankQuery struct {
	appID, boardID, zoneID, channel string
	relationType, rankType          string
	dataType                        int32
	suffix, rankTitle               string
	topN, pageNum, pageSize         int
}

func (s *Server) parseImRankQuery(r *http.Request, claims *auth.Claims) (*imRankQuery, error) {
	q := &imRankQuery{}
	if r.Method == http.MethodPost {
		var body struct {
			AppID        string `json:"app_id"`
			BoardID      string `json:"board_id"`
			ZoneID       string `json:"zoneId"`
			ZoneIDAlt    string `json:"zone_id"`
			Channel      string `json:"channel"`
			ChannelID    string `json:"channel_id"`
			ChannelId    string `json:"channelId"`
			RelationType string `json:"relationType"`
			RankType     string `json:"rankType"`
			RankType2    string `json:"rank_type"`
			PeriodType   string `json:"period_type"`
			DataType     int32  `json:"dataType"`
			Suffix       string `json:"suffix"`
			RankTitle    string `json:"rankTitle"`
			TopN         int    `json:"top_n"`
			PageNum      int    `json:"pageNum"`
			PageSize     int    `json:"pageSize"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			return nil, err
		}
		q.appID, q.boardID = body.AppID, body.BoardID
		q.zoneID = domain.FirstNonEmpty(body.ZoneID, body.ZoneIDAlt)
		q.channel = domain.FirstNonEmpty(body.ChannelID, body.ChannelId, body.Channel)
		q.relationType = body.RelationType
		q.rankType = domain.FirstNonEmpty(body.RankType, body.RankType2, body.PeriodType)
		q.dataType = body.DataType
		q.suffix, q.rankTitle = body.Suffix, body.RankTitle
		q.topN, q.pageNum, q.pageSize = body.TopN, body.PageNum, body.PageSize
	} else if r.Method == http.MethodGet {
		qq := r.URL.Query()
		q.appID = qq.Get("app_id")
		q.boardID = qq.Get("board_id")
		q.channel = domain.FirstNonEmpty(qq.Get("channel_id"), qq.Get("channelId"), qq.Get("channel"))
		q.zoneID = domain.FirstNonEmpty(qq.Get("zoneId"), qq.Get("zone_id"))
		q.relationType = qq.Get("relationType")
		q.rankType = domain.FirstNonEmpty(qq.Get("rankType"), qq.Get("rank_type"), qq.Get("period_type"))
		if v := qq.Get("dataType"); v != "" {
			n, _ := strconv.Atoi(v)
			q.dataType = int32(n)
		}
		q.suffix = qq.Get("suffix")
		q.rankTitle = qq.Get("rankTitle")
		q.topN, _ = strconv.Atoi(qq.Get("top_n"))
		q.pageNum, _ = strconv.Atoi(qq.Get("pageNum"))
		q.pageSize, _ = strconv.Atoi(qq.Get("pageSize"))
	} else {
		return nil, errMethodNotAllowed
	}
	if q.appID == "" {
		q.appID = claims.AppID
	}
	if q.channel == "" {
		q.channel = claims.Channel
	}
	if q.appID != claims.AppID || q.channel != claims.Channel {
		return nil, errAppChannelMismatch
	}
	return q, nil
}

var (
	errMethodNotAllowed   = &apiError{msg: "GET or POST required"}
	errAppChannelMismatch = &apiError{msg: "app/channel mismatch"}
)

type apiError struct{ msg string }

func (e *apiError) Error() string { return e.msg }

func (s *Server) ensureAppChannel(w http.ResponseWriter, claims *auth.Claims, appID, channel string) error {
	if appID != claims.AppID || channel != claims.Channel {
		writeErr(w, http.StatusForbidden, "FORBIDDEN", "app/channel mismatch")
		return errAppChannelMismatch
	}
	return nil
}

func (s *Server) handlePlayerProfile(w http.ResponseWriter, r *http.Request) {
	claims, ok := s.requireSession(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		out, err := s.svc.GetPlayerProfile(r.Context(), claims.AppID, claims.Channel, claims.OpenID)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "PROFILE_FAIL", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, out)
	case http.MethodPost, http.MethodPut:
		var body struct {
			Username  string `json:"username"`
			Nickname  string `json:"nickname"`
			AvatarURL string `json:"avatar_url"`
			Avatar    string `json:"avatar"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, "BAD_REQUEST", "invalid json")
			return
		}
		out, err := s.svc.UpdatePlayerProfile(r.Context(), service.UpdateProfileInput{
			AppID: claims.AppID, Channel: claims.Channel, OpenID: claims.OpenID,
			Username: body.Username, Nickname: body.Nickname,
			AvatarURL: body.AvatarURL, Avatar: body.Avatar,
		})
		if err != nil {
			writeErr(w, http.StatusBadRequest, "PROFILE_UPDATE_FAIL", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, out)
	default:
		writeErr(w, http.StatusMethodNotAllowed, "METHOD", "GET or POST required")
	}
}

func (s *Server) handlePlayerProfileSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "METHOD", "POST required")
		return
	}
	claims, ok := s.requireSession(w, r)
	if !ok {
		return
	}
	var body struct {
		AccessToken string `json:"access_token"`
		AccessTok   string `json:"accessToken"`
		Code        string `json:"code"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	out, err := s.svc.SyncPlayerProfile(r.Context(), service.SyncProfileInput{
		AppID: claims.AppID, Channel: claims.Channel, OpenID: claims.OpenID,
		AccessToken: domain.FirstNonEmpty(body.AccessToken, body.AccessTok),
		Code:        body.Code,
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, "PROFILE_SYNC_FAIL", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// handleTikTokProfile exchanges TikTok authorize/login code for nick+avatar.
// No session required. Prefer body.auth_code (Cocos HttpUnit overwrites "code").
func (s *Server) handleTikTokProfile(w http.ResponseWriter, r *http.Request) {
	setCORS(w, r)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "METHOD", "POST required")
		return
	}
	var body struct {
		AuthCode  string `json:"auth_code"`
		TTCode    string `json:"tt_code"`
		Code      string `json:"code"`
		Channel   string `json:"channel"`
		ChannelID string `json:"channel_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "BAD_JSON", "invalid json body")
		return
	}
	code := domain.FirstNonEmpty(body.AuthCode, body.TTCode, body.Code)
	ch := domain.FirstNonEmpty(body.ChannelID, body.Channel)
	out, err := s.svc.FetchProfileByAuthCode(r.Context(), code, ch)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "TT_PROFILE_FAIL", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func setCORS(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if origin == "" {
		origin = "*"
	}
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
}

func (s *Server) handleRegisterStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "METHOD", "GET required")
		return
	}
	h := r.Header.Get("Authorization")
	token := ""
	if strings.HasPrefix(strings.ToLower(h), "bearer ") {
		token = strings.TrimSpace(h[7:])
	}
	if token == "" {
		token = r.Header.Get("X-Rank-Service-Token")
	}
	if !s.svc.CheckServiceToken(token) {
		writeErr(w, http.StatusUnauthorized, "UNAUTHORIZED", "service token required")
		return
	}
	appID := r.URL.Query().Get("app_id")
	out, err := s.svc.GetRegisterStats(r.Context(), appID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "STATS_FAIL", err.Error())
		return
	}
	// Optional filter by channel_id
	filter := domain.FirstNonEmpty(r.URL.Query().Get("channel_id"), r.URL.Query().Get("channelId"), r.URL.Query().Get("channel"))
	if filter != "" {
		var filtered []domain.ChannelCount
		var total int64
		for _, row := range out.ByChannel {
			if row.Channel == filter {
				filtered = append(filtered, row)
				total += row.Count
			}
		}
		out.ByChannel = filtered
		out.Total = total
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) requireSession(w http.ResponseWriter, r *http.Request) (*auth.Claims, bool) {
	h := r.Header.Get("Authorization")
	token := ""
	if strings.HasPrefix(strings.ToLower(h), "bearer ") {
		token = strings.TrimSpace(h[7:])
	}
	if token == "" {
		token = r.Header.Get("X-Rank-Session")
	}
	if token == "" {
		writeErr(w, http.StatusUnauthorized, "SESSION_EXPIRED", "missing session")
		return nil, false
	}
	claims, err := s.svc.ParseSession(token)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "SESSION_EXPIRED", err.Error())
		return nil, false
	}
	return claims, true
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]string{"code": code, "msg": msg})
}

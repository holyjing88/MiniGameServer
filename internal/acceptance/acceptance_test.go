package acceptance_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	rankv1 "minigameserver/api/gen/rank/v1"
	grpcapi "minigameserver/internal/api/grpc"
	httpapi "minigameserver/internal/api/http"
	"minigameserver/internal/auth"
	"minigameserver/internal/config"
	"minigameserver/internal/domain"
	"minigameserver/internal/hotcache"
	"minigameserver/internal/service"
	"minigameserver/internal/store/memory"
)

func newTestService(t *testing.T) *service.Service {
	t.Helper()
	cfg := config.Config{
		SessionSecret: "test-secret",
		SessionTTL:    time.Hour,
		ServiceToken:  "svc-token",
		TopNDefault:   100,
		TopNMax:       100,
		Timezone:      "UTC",
		AuthMode:      "mock",
	}
	st := memory.New()
	sessions := auth.NewSessionManager(cfg.SessionSecret, cfg.SessionTTL)
	svc := service.New(cfg, st, hotcache.New(), sessions, auth.MockResolver{})
	svc.SetNow(func() time.Time { return time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC) })
	return svc
}

func registerAndLogin(t *testing.T, baseURL, channel, code string) (token, accountID string) {
	t.Helper()
	regBody, _ := json.Marshal(map[string]string{
		"app_id": "parking_smart_brain", "channel_id": channel, "code": code,
	})
	res, err := http.Post(baseURL+"/v1/auth/register", "application/json", bytes.NewReader(regBody))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("register %d %s", res.StatusCode, raw)
	}
	loginBody, _ := json.Marshal(map[string]string{
		"app_id": "parking_smart_brain", "channel_id": channel, "code": code,
	})
	res, err = http.Post(baseURL+"/v1/auth/login", "application/json", bytes.NewReader(loginBody))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = io.ReadAll(res.Body)
	res.Body.Close()
	var sess struct {
		SessionToken string `json:"session_token"`
		PlayerID     string `json:"player_id"`
		AccountID    string `json:"account_id"`
		OpenID       string `json:"open_id"`
	}
	_ = json.Unmarshal(raw, &sess)
	if res.StatusCode != 200 || sess.SessionToken == "" {
		t.Fatalf("login %d %s", res.StatusCode, raw)
	}
	return sess.SessionToken, sess.AccountID
}

func TestHTTP_LoginNeedRegisterThenLogin(t *testing.T) {
	svc := newTestService(t)
	ts := httptest.NewServer(httpapi.New(svc).Handler())
	defer ts.Close()

	body, _ := json.Marshal(map[string]string{
		"app_id": "parking_smart_brain", "channel_id": "tiktok_minis", "code": "mock:newUser",
	})
	res, err := http.Post(ts.URL+"/v1/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("expect NEED_REGISTER 404, got %d %s", res.StatusCode, raw)
	}
	var need struct {
		Code      string `json:"code"`
		OpenID    string `json:"open_id"`
		AccountID string `json:"account_id"`
		ChannelID string `json:"channel_id"`
	}
	_ = json.Unmarshal(raw, &need)
	if need.Code != "NEED_REGISTER" || need.OpenID != "newUser" || need.AccountID != "tiktok_minis_newUser" {
		t.Fatalf("%+v body=%s", need, raw)
	}

	tok, aid := registerAndLogin(t, ts.URL, "tiktok_minis", "mock:newUser")
	if aid != "tiktok_minis_newUser" || tok == "" {
		t.Fatalf("aid=%s tok=%q", aid, tok)
	}
}

func TestHTTP_SessionScoreLeaderboard_AgainstDesign(t *testing.T) {
	svc := newTestService(t)
	ts := httptest.NewServer(httpapi.New(svc).Handler())
	defer ts.Close()

	token, accountID := registerAndLogin(t, ts.URL, "tiktok_minis", "mock:playerA")
	if accountID != "tiktok_minis_playerA" {
		t.Fatalf("account_id=%s", accountID)
	}

	scoreBody, _ := json.Marshal(map[string]interface{}{
		"app_id": "parking_smart_brain", "board_id": "clear_level", "zone_id": "default",
		"channel": "tiktok_minis", "score": 50,
	})
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/score", bytes.NewReader(scoreBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("score %d %s", res.StatusCode, raw)
	}
	var scoreOut struct {
		Week  map[string]interface{} `json:"week"`
		Month map[string]interface{} `json:"month"`
	}
	_ = json.Unmarshal(raw, &scoreOut)
	if scoreOut.Week["updated"] != true || scoreOut.Month["updated"] != true {
		t.Fatalf("design: default dual-write week+month, got %s", raw)
	}

	// only increase
	scoreBody, _ = json.Marshal(map[string]interface{}{
		"app_id": "parking_smart_brain", "board_id": "clear_level",
		"channel": "tiktok_minis", "score": 10,
	})
	req, _ = http.NewRequest(http.MethodPost, ts.URL+"/v1/score", bytes.NewReader(scoreBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	res, _ = http.DefaultClient.Do(req)
	raw, _ = io.ReadAll(res.Body)
	res.Body.Close()
	_ = json.Unmarshal(raw, &scoreOut)
	if scoreOut.Week["updated"] != false || scoreOut.Week["best_score"].(float64) != 50 {
		t.Fatalf("design: only-increase, got %s", raw)
	}

	req, _ = http.NewRequest(http.MethodGet,
		ts.URL+"/v1/leaderboard?app_id=parking_smart_brain&board_id=clear_level&channel=tiktok_minis&period_type=week&top_n=10", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = io.ReadAll(res.Body)
	res.Body.Close()
	var lb struct {
		Encoding  string `json:"encoding"`
		Entries   string `json:"entries"`
		SelfRank  int32  `json:"self_rank"`
		SelfScore int64  `json:"self_score"`
		CacheHit  bool   `json:"cache_hit"`
	}
	_ = json.Unmarshal(raw, &lb)
	if lb.Encoding != "gzip+base64" || lb.SelfRank != 1 || lb.SelfScore != 50 {
		t.Fatalf("design envelope mismatch: %+v body=%s", lb, raw)
	}
	gz, err := base64.StdEncoding.DecodeString(lb.Entries)
	if err != nil {
		t.Fatal(err)
	}
	var entries []domain.CompactEntry
	if err := hotcache.GunzipJSON(gz, &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].R != 1 || entries[0].P != "tiktok_minis_playerA" || entries[0].S != 50 {
		t.Fatalf("entries=%+v", entries)
	}

	// second pull should hit cache
	req, _ = http.NewRequest(http.MethodGet,
		ts.URL+"/v1/leaderboard?app_id=parking_smart_brain&board_id=clear_level&channel=tiktok_minis&period_type=week", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res, _ = http.DefaultClient.Do(req)
	raw, _ = io.ReadAll(res.Body)
	res.Body.Close()
	_ = json.Unmarshal(raw, &lb)
	if !lb.CacheHit {
		t.Fatalf("expect cache_hit on second pull: %s", raw)
	}
}

func TestHTTP_UnauthorizedWithoutSession(t *testing.T) {
	svc := newTestService(t)
	ts := httptest.NewServer(httpapi.New(svc).Handler())
	defer ts.Close()
	res, err := http.Get(ts.URL + "/v1/leaderboard?period_type=week&board_id=x&app_id=a&channel=c")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status=%d", res.StatusCode)
	}
}

func TestGRPC_UpsertAndLeaderboard_ServiceAuth(t *testing.T) {
	svc := newTestService(t)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	gs := grpc.NewServer()
	grpcapi.Register(gs, svc)
	go func() { _ = gs.Serve(lis) }()
	defer gs.Stop()

	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	cli := rankv1.NewRankServiceClient(conn)

	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer svc-token"))
	up, err := cli.UpsertMaxScore(ctx, &rankv1.UpsertMaxScoreRequest{
		AppId: "parking_smart_brain", BoardId: "clear_level", ZoneId: "default",
		Channel: "tiktok_minis", PlayerId: "oid-b", Score: 77,
		PeriodType: rankv1.PeriodType_PERIOD_BOTH,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !up.Week.Updated || !up.Month.Updated || up.Week.BestScore != 77 {
		t.Fatalf("%+v", up)
	}

	lb, err := cli.GetLeaderboard(ctx, &rankv1.GetLeaderboardRequest{
		AppId: "parking_smart_brain", BoardId: "clear_level", Channel: "tiktok_minis",
		PeriodType: rankv1.PeriodType_PERIOD_WEEK, TopN: 10, PlayerId: "oid-b",
	})
	if err != nil {
		t.Fatal(err)
	}
	if lb.SelfRank != 1 || lb.SelfScore != 77 || len(lb.EntriesGzip) == 0 {
		t.Fatalf("%+v", lb)
	}
	var entries []domain.CompactEntry
	if err := hotcache.GunzipJSON(lb.EntriesGzip, &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].P != "oid-b" {
		t.Fatalf("%+v", entries)
	}

	// bad token
	badCtx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer wrong"))
	_, err = cli.UpsertMaxScore(badCtx, &rankv1.UpsertMaxScoreRequest{AppId: "a", BoardId: "b", Channel: "c", PlayerId: "p", Score: 1})
	if err == nil {
		t.Fatal("expect auth error")
	}
}

func TestHTTP_PlayerRegister_FirstTouchChannel(t *testing.T) {
	svc := newTestService(t)
	ts := httptest.NewServer(httpapi.New(svc).Handler())
	defer ts.Close()

	// Register creates account (no session). Re-register same channel+openid is idempotent.
	body, _ := json.Marshal(map[string]interface{}{
		"app_id": "parking_smart_brain", "channel_id": "tiktok_minis", "code": "mock:regUser",
		"platform_kind": 1, "extra_json": `{"campaign":"launch"}`,
	})
	res, err := http.Post(ts.URL+"/v1/auth/register", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("%d %s", res.StatusCode, raw)
	}
	var out struct {
		IsNew     bool   `json:"is_new"`
		Channel   string `json:"channel"`
		AccountID string `json:"account_id"`
		NeedLogin bool   `json:"need_login"`
	}
	_ = json.Unmarshal(raw, &out)
	if !out.IsNew || out.Channel != "tiktok_minis" || out.AccountID != "tiktok_minis_regUser" || !out.NeedLogin {
		t.Fatalf("%+v", out)
	}

	// re-register same account: is_new=false
	res, _ = http.Post(ts.URL+"/v1/auth/register", "application/json", bytes.NewReader(body))
	raw, _ = io.ReadAll(res.Body)
	res.Body.Close()
	_ = json.Unmarshal(raw, &out)
	if out.IsNew || out.Channel != "tiktok_minis" {
		t.Fatalf("idempotent register broken: %s", raw)
	}

	// same open_id on different channel = different account
	body2, _ := json.Marshal(map[string]interface{}{
		"app_id": "parking_smart_brain", "channel_id": "weixin", "code": "mock:regUser",
	})
	res, _ = http.Post(ts.URL+"/v1/auth/register", "application/json", bytes.NewReader(body2))
	raw, _ = io.ReadAll(res.Body)
	res.Body.Close()
	_ = json.Unmarshal(raw, &out)
	if !out.IsNew || out.AccountID != "weixin_regUser" {
		t.Fatalf("channel-scoped account expected: %s", raw)
	}

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/v1/stats/register?app_id=parking_smart_brain", nil)
	req.Header.Set("Authorization", "Bearer svc-token")
	res, _ = http.DefaultClient.Do(req)
	raw, _ = io.ReadAll(res.Body)
	res.Body.Close()
	var stats struct {
		Total     int64 `json:"total"`
		ByChannel []struct {
			Channel string `json:"channel"`
			Count   int64  `json:"count"`
		} `json:"by_channel"`
	}
	_ = json.Unmarshal(raw, &stats)
	if stats.Total != 2 {
		t.Fatalf("%s", raw)
	}
}

func TestHTTP_Register_ClickID_FirstTouch(t *testing.T) {
	svc := newTestService(t)
	ts := httptest.NewServer(httpapi.New(svc).Handler())
	defer ts.Close()

	body, _ := json.Marshal(map[string]interface{}{
		"app_id": "parking_smart_brain", "channel_id": "tiktok_minis", "code": "mock:clickUser",
		"username": "promo", "clickid": "ttclid_abc123",
	})
	res, err := http.Post(ts.URL+"/v1/auth/register", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("%d %s", res.StatusCode, raw)
	}
	var out struct {
		IsNew    bool   `json:"is_new"`
		ClickID  string `json:"click_id"`
		Username string `json:"username"`
	}
	_ = json.Unmarshal(raw, &out)
	if !out.IsNew || out.ClickID != "ttclid_abc123" || out.Username != "promo" {
		t.Fatalf("%+v body=%s", out, raw)
	}

	// re-register with different clickid must keep first-touch
	body2, _ := json.Marshal(map[string]interface{}{
		"app_id": "parking_smart_brain", "channel_id": "tiktok_minis", "code": "mock:clickUser",
		"username": "promo2", "click_id": "other_click",
	})
	res, _ = http.Post(ts.URL+"/v1/auth/register", "application/json", bytes.NewReader(body2))
	raw, _ = io.ReadAll(res.Body)
	res.Body.Close()
	_ = json.Unmarshal(raw, &out)
	if out.IsNew || out.ClickID != "ttclid_abc123" {
		t.Fatalf("first-touch click_id broken: %s", raw)
	}

	loginBody, _ := json.Marshal(map[string]string{
		"app_id": "parking_smart_brain", "channel_id": "tiktok_minis", "code": "mock:clickUser",
	})
	res, _ = http.Post(ts.URL+"/v1/auth/login", "application/json", bytes.NewReader(loginBody))
	raw, _ = io.ReadAll(res.Body)
	res.Body.Close()
	var sess struct {
		ClickID  string `json:"click_id"`
		Username string `json:"username"`
	}
	_ = json.Unmarshal(raw, &sess)
	if res.StatusCode != 200 || sess.ClickID != "ttclid_abc123" || sess.Username != "promo" {
		t.Fatalf("login echo click_id: %d %s", res.StatusCode, raw)
	}
}

func TestHTTP_ImRankAlignedAPIs(t *testing.T) {
	svc := newTestService(t)
	ts := httptest.NewServer(httpapi.New(svc).Handler())
	defer ts.Close()

	token, _ := registerAndLogin(t, ts.URL, "tiktok_minis", "mock:imA")

	// illegal relationType on set
	badSet, _ := json.Marshal(map[string]interface{}{
		"app_id": "parking_smart_brain", "board_id": "clear_level", "zoneId": "default",
		"channel": "tiktok_minis", "dataType": 0, "value": "12", "relationType": "all",
	})
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/imrank/setImRankData", bytes.NewReader(badSet))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	res, _ := http.DefaultClient.Do(req)
	if res.StatusCode == 200 {
		t.Fatal("setImRankData must reject relationType")
	}
	res.Body.Close()

	setBody, _ := json.Marshal(map[string]interface{}{
		"app_id": "parking_smart_brain", "board_id": "clear_level", "zoneId": "default",
		"channel": "tiktok_minis", "dataType": 0, "value": "42", "priority": 0, "extra": "",
	})
	req, _ = http.NewRequest(http.MethodPost, ts.URL+"/v1/imrank/setImRankData", bytes.NewReader(setBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("setImRankData %d %s", res.StatusCode, raw)
	}
	var setMap map[string]map[string]interface{}
	_ = json.Unmarshal(raw, &setMap)
	for _, k := range []string{"day", "week", "month", "all"} {
		if setMap[k]["updated"] != true || setMap[k]["best_score"].(float64) != 42 {
			t.Fatalf("expect dual-write %s in %s", k, raw)
		}
	}

	listBody, _ := json.Marshal(map[string]interface{}{
		"app_id": "parking_smart_brain", "board_id": "clear_level", "zoneId": "default",
		"channel": "tiktok_minis", "relationType": "all", "rankType": "week",
		"dataType": 0, "suffix": "guan", "rankTitle": "clear_rank",
	})
	req, _ = http.NewRequest(http.MethodPost, ts.URL+"/v1/imrank/getImRankList", bytes.NewReader(listBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	res, _ = http.DefaultClient.Do(req)
	raw, _ = io.ReadAll(res.Body)
	res.Body.Close()
	var listOut struct {
		Display   string                `json:"display"`
		RankType  string                `json:"rankType"`
		SelfRank  int32                 `json:"self_rank"`
		SelfScore int64                 `json:"self_score"`
		Items     []domain.CompactEntry `json:"items"`
	}
	_ = json.Unmarshal(raw, &listOut)
	if listOut.Display != "custom" || listOut.RankType != "week" || listOut.SelfRank != 1 || listOut.SelfScore != 42 || len(listOut.Items) != 1 {
		t.Fatalf("getImRankList: %s", raw)
	}

	dataBody, _ := json.Marshal(map[string]interface{}{
		"app_id": "parking_smart_brain", "board_id": "clear_level", "zoneId": "default",
		"channel": "tiktok_minis", "relationType": "all", "rankType": "all",
		"dataType": 0, "pageNum": 1, "pageSize": 20,
	})
	req, _ = http.NewRequest(http.MethodPost, ts.URL+"/v1/imrank/getImRankData", bytes.NewReader(dataBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	res, _ = http.DefaultClient.Do(req)
	raw, _ = io.ReadAll(res.Body)
	res.Body.Close()
	var dataOut struct {
		RankType  string                `json:"rankType"`
		SelfScore int64                 `json:"self_score"`
		Items     []domain.CompactEntry `json:"items"`
		Total     int                   `json:"total"`
	}
	_ = json.Unmarshal(raw, &dataOut)
	if dataOut.RankType != "all" || dataOut.SelfScore != 42 || dataOut.Total != 1 || len(dataOut.Items) != 1 {
		t.Fatalf("getImRankData: %s", raw)
	}

	friendBody, _ := json.Marshal(map[string]interface{}{
		"app_id": "parking_smart_brain", "board_id": "clear_level",
		"channel": "tiktok_minis", "relationType": "friend", "rankType": "week",
	})
	req, _ = http.NewRequest(http.MethodPost, ts.URL+"/v1/imrank/getImRankData", bytes.NewReader(friendBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	res, _ = http.DefaultClient.Do(req)
	raw, _ = io.ReadAll(res.Body)
	res.Body.Close()
	var friendOut struct {
		FriendUnsupported bool                  `json:"friend_unsupported"`
		Items             []domain.CompactEntry `json:"items"`
	}
	_ = json.Unmarshal(raw, &friendOut)
	if !friendOut.FriendUnsupported || len(friendOut.Items) != 0 {
		t.Fatalf("friend: %s", raw)
	}

	req, _ = http.NewRequest(http.MethodPost, ts.URL+"/v1/imrank/getImRankData", bytes.NewReader([]byte(
		`{"app_id":"parking_smart_brain","board_id":"clear_level","channel_id":"tiktok_minis","rankType":"day","pageNum":1,"pageSize":10}`,
	)))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	res, _ = http.DefaultClient.Do(req)
	raw, _ = io.ReadAll(res.Body)
	res.Body.Close()
	var dayOut struct {
		Channel   string `json:"channel"`
		RankType  string `json:"rankType"`
		SelfScore int64  `json:"self_score"`
	}
	_ = json.Unmarshal(raw, &dayOut)
	if res.StatusCode != 200 || dayOut.Channel != "tiktok_minis" || dayOut.RankType != "day" || dayOut.SelfScore != 42 {
		t.Fatalf("channel_id+rankType day: %s", raw)
	}
}

func TestHTTP_ChannelIsolationAcrossPlatforms(t *testing.T) {
	svc := newTestService(t)
	ts := httptest.NewServer(httpapi.New(svc).Handler())
	defer ts.Close()

	tokTT, _ := registerAndLogin(t, ts.URL, "tiktok_minis", "mock:p1")
	tokWX, _ := registerAndLogin(t, ts.URL, "weixin", "mock:p1")

	set := func(tok, ch string, val int) {
		body, _ := json.Marshal(map[string]interface{}{
			"app_id": "parking_smart_brain", "board_id": "clear_level",
			"channel_id": ch, "dataType": 0, "value": strconv.Itoa(val),
		})
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/imrank/setImRankData", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != 200 {
			t.Fatalf("set %s: %s", ch, raw)
		}
	}
	set(tokTT, "tiktok_minis", 10)
	set(tokWX, "weixin", 99)

	getScore := func(tok, ch string) int64 {
		body, _ := json.Marshal(map[string]interface{}{
			"app_id": "parking_smart_brain", "board_id": "clear_level",
			"channel_id": ch, "relationType": "all", "rankType": "all",
			"pageNum": 1, "pageSize": 10,
		})
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/imrank/getImRankData", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Content-Type", "application/json")
		res, _ := http.DefaultClient.Do(req)
		raw, _ := io.ReadAll(res.Body)
		res.Body.Close()
		var out struct {
			SelfScore int64  `json:"self_score"`
			Channel   string `json:"channel"`
		}
		_ = json.Unmarshal(raw, &out)
		if out.Channel != ch {
			t.Fatalf("channel echo %s body=%s", ch, raw)
		}
		return out.SelfScore
	}
	if getScore(tokTT, "tiktok_minis") != 10 {
		t.Fatal("tiktok board polluted")
	}
	if getScore(tokWX, "weixin") != 99 {
		t.Fatal("weixin board polluted")
	}
}

func TestGRPC_PlayerRegisterAndStats(t *testing.T) {
	svc := newTestService(t)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	gs := grpc.NewServer()
	grpcapi.Register(gs, svc)
	go func() { _ = gs.Serve(lis) }()
	defer gs.Stop()

	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	cli := rankv1.NewPlayerServiceClient(conn)
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer svc-token"))

	r1, err := cli.ReportPlayerRegister(ctx, &rankv1.ReportPlayerRegisterRequest{
		AppId: "parking_smart_brain", Channel: "tiktok_minis", PlayerId: "g1", PlatformKind: 1,
	})
	if err != nil || !r1.IsNew {
		t.Fatalf("%v %+v", err, r1)
	}
	// same open_id on another channel = new account
	r2, err := cli.ReportPlayerRegister(ctx, &rankv1.ReportPlayerRegisterRequest{
		AppId: "parking_smart_brain", Channel: "other", PlayerId: "g1",
	})
	if err != nil || !r2.IsNew || r2.Channel != "other" {
		t.Fatalf("%v %+v", err, r2)
	}
	// idempotent re-register
	r3, err := cli.ReportPlayerRegister(ctx, &rankv1.ReportPlayerRegisterRequest{
		AppId: "parking_smart_brain", Channel: "tiktok_minis", PlayerId: "g1",
	})
	if err != nil || r3.IsNew || r3.Channel != "tiktok_minis" {
		t.Fatalf("%v %+v", err, r3)
	}
	st, err := cli.GetRegisterStats(ctx, &rankv1.GetRegisterStatsRequest{AppId: "parking_smart_brain"})
	if err != nil || st.Total != 2 {
		t.Fatalf("%v %+v", err, st)
	}
}

func TestHTTP_PlayerProfileNicknameAvatar(t *testing.T) {
	svc := newTestService(t)
	ts := httptest.NewServer(httpapi.New(svc).Handler())
	defer ts.Close()

	token, accountID := registerAndLogin(t, ts.URL, "tiktok_minis", "mock:avatarUser")

	// Initially empty profile fields after bare register
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/v1/player/profile", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("get profile %d %s", res.StatusCode, raw)
	}
	var cached struct {
		AccountID string `json:"account_id"`
		Nickname  string `json:"nickname"`
		AvatarURL string `json:"avatar_url"`
		Source    string `json:"source"`
	}
	_ = json.Unmarshal(raw, &cached)
	if cached.AccountID != accountID || cached.Source != "cache" {
		t.Fatalf("cached=%+v", cached)
	}

	// Client push nickname/avatar (TTMinisSDK path)
	updBody, _ := json.Marshal(map[string]string{
		"nickname": "TikTokNick", "avatar_url": "https://cdn.example/a.png",
	})
	req, _ = http.NewRequest(http.MethodPost, ts.URL+"/v1/player/profile", bytes.NewReader(updBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = io.ReadAll(res.Body)
	res.Body.Close()
	var updated struct {
		Nickname  string `json:"nickname"`
		Avatar    string `json:"avatar"`
		AvatarURL string `json:"avatar_url"`
		Source    string `json:"source"`
	}
	_ = json.Unmarshal(raw, &updated)
	if res.StatusCode != 200 || updated.Nickname != "TikTokNick" || updated.AvatarURL != "https://cdn.example/a.png" || updated.Source != "update" {
		t.Fatalf("update %d %s", res.StatusCode, raw)
	}

	// Server sync overwrites from mock provider
	req, _ = http.NewRequest(http.MethodPost, ts.URL+"/v1/player/profile/sync", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = io.ReadAll(res.Body)
	res.Body.Close()
	var synced struct {
		Nickname  string `json:"nickname"`
		AvatarURL string `json:"avatar_url"`
		Source    string `json:"source"`
	}
	_ = json.Unmarshal(raw, &synced)
	if res.StatusCode != 200 || synced.Source != "sync" || synced.Nickname != "TT_avatarUser" || synced.AvatarURL == "" {
		t.Fatalf("sync %d %s", res.StatusCode, raw)
	}

	// Login returns nickname/avatar
	loginBody, _ := json.Marshal(map[string]string{
		"app_id": "parking_smart_brain", "channel_id": "tiktok_minis", "code": "mock:avatarUser",
	})
	res, err = http.Post(ts.URL+"/v1/auth/login", "application/json", bytes.NewReader(loginBody))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = io.ReadAll(res.Body)
	res.Body.Close()
	var login struct {
		Nickname  string `json:"nickname"`
		AvatarURL string `json:"avatar_url"`
	}
	_ = json.Unmarshal(raw, &login)
	if res.StatusCode != 200 || login.Nickname != "TT_avatarUser" || login.AvatarURL == "" {
		t.Fatalf("login profile %d %s", res.StatusCode, raw)
	}
}

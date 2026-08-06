package httpapi_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	httpapi "minigameserver/internal/api/http"
	"minigameserver/internal/auth"
	"minigameserver/internal/config"
	"minigameserver/internal/hotcache"
	"minigameserver/internal/service"
	"minigameserver/internal/store/memory"
)

func TestImRank_SetAndGet_NoSession(t *testing.T) {
	cfg := config.Config{
		DefaultAppID: "vampire_dormitory",
		AuthMode:     "mock",
		TopNDefault:  100,
		TopNMax:      100,
		Timezone:     "Asia/Shanghai",
	}
	st := memory.New()
	sessions := auth.NewSessionManager("test-secret", 3600)
	svc := service.New(cfg, st, hotcache.New(), sessions, auth.MockResolver{})
	svc.SetProfileProvider(auth.MockProfileProvider{})
	srv := httpapi.New(svc)

	setBody := map[string]interface{}{
		"app_id": "vampire_dormitory", "board_id": "classic_survive_waves",
		"zoneId": "default", "channel_id": "tiktok_minis", "open_id": "u_rank_1",
		"dataType": 0, "value": "15", "priority": 0, "rankType": "both",
	}
	raw, _ := json.Marshal(setBody)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/imrank/setImRankData", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("setImRankData status=%d body=%s", rr.Code, rr.Body.String())
	}

	listBody := map[string]interface{}{
		"app_id": "vampire_dormitory", "board_id": "classic_survive_waves",
		"zoneId": "default", "channel_id": "tiktok_minis", "open_id": "u_rank_1",
		"relationType": "default", "rankType": "week", "dataType": 0, "top_n": 50,
		"suffix": "波", "rankTitle": "存活波次排行榜",
	}
	raw, _ = json.Marshal(listBody)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/imrank/getImRankList", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rr = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("getImRankList status=%d body=%s", rr.Code, rr.Body.String())
	}
	var out map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["self_rank"] == nil || out["self_rank"].(float64) < 1 {
		t.Fatalf("self_rank missing: %+v", out)
	}
	items, _ := out["items"].([]interface{})
	if len(items) < 1 {
		t.Fatalf("items empty: %+v", out)
	}
}

package httpapi_test

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	httpapi "minigameserver/internal/api/http"
	"minigameserver/internal/auth"
	"minigameserver/internal/config"
	"minigameserver/internal/domain"
	"minigameserver/internal/hotcache"
	"minigameserver/internal/service"
	"minigameserver/internal/store/memory"
)

func decodeEntries(t *testing.T, b64 string) []domain.CompactEntry {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatalf("b64: %v", err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	defer zr.Close()
	var items []domain.CompactEntry
	if err := json.NewDecoder(zr).Decode(&items); err != nil {
		t.Fatalf("json: %v", err)
	}
	return items
}

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
	if _, hasItems := out["items"]; hasItems {
		t.Fatalf("HTTP must not return plaintext items: %+v", out)
	}
	ent, _ := out["entries"].(string)
	if ent == "" || out["encoding"] != "gzip+base64" {
		t.Fatalf("entries missing: %+v", out)
	}
	items := decodeEntries(t, ent)
	if len(items) < 1 {
		t.Fatalf("decoded entries empty: %+v", out)
	}
}

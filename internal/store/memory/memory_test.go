package memory_test

import (
	"context"
	"testing"
	"time"

	"minigameserver/internal/domain"
	"minigameserver/internal/store/memory"
)

func TestUpsertMaxOnlyIncrease(t *testing.T) {
	st := memory.New()
	ctx := context.Background()
	bk := domain.BoardKey("app", "b", "default", "2026-W31", "tiktok_minis")
	now := time.Now().UnixMilli()

	up, best, err := st.UpsertMax(ctx, domain.Entry{BoardKey: bk, PlayerID: "p1", Score: 10, UpdatedAt: now})
	if err != nil || !up || best != 10 {
		t.Fatalf("insert: up=%v best=%d err=%v", up, best, err)
	}
	up, best, err = st.UpsertMax(ctx, domain.Entry{BoardKey: bk, PlayerID: "p1", Score: 8, UpdatedAt: now + 1})
	if err != nil || up || best != 10 {
		t.Fatalf("lower must not update: up=%v best=%d err=%v", up, best, err)
	}
	up, best, err = st.UpsertMax(ctx, domain.Entry{BoardKey: bk, PlayerID: "p1", Score: 12, UpdatedAt: now + 2})
	if err != nil || !up || best != 12 {
		t.Fatalf("higher must update: up=%v best=%d err=%v", up, best, err)
	}
}

func TestTopNTieBreakFirstWins(t *testing.T) {
	st := memory.New()
	ctx := context.Background()
	bk := domain.BoardKey("app", "b", "default", "2026-07", "tiktok_minis")
	_, _, _ = st.UpsertMax(ctx, domain.Entry{BoardKey: bk, PlayerID: "late", Score: 100, UpdatedAt: 200})
	_, _, _ = st.UpsertMax(ctx, domain.Entry{BoardKey: bk, PlayerID: "early", Score: 100, UpdatedAt: 100})
	list, err := st.TopN(ctx, bk, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) < 2 || list[0].PlayerID != "early" {
		t.Fatalf("first to score should rank higher: %+v", list)
	}
	rank, _, ok, err := st.RankOf(ctx, bk, "late")
	if err != nil || !ok || rank != 2 {
		t.Fatalf("late rank=%d ok=%v err=%v", rank, ok, err)
	}
}

func TestReportRegister_KeepsFirstTouch(t *testing.T) {
	st := memory.New()
	ctx := context.Background()
	reg := domain.PlayerRegister{
		AppID: "app", Channel: "tiktok_minis", OpenID: "u1",
		PlayerID: "tiktok_minis_u1", Username: "n", ClickID: "c1",
		PlatformKind: 1, RegisteredAtMs: 1000,
	}
	isNew, got, err := st.ReportRegister(ctx, reg)
	if err != nil || !isNew || got.ClickID != "c1" {
		t.Fatalf("first: isNew=%v got=%+v err=%v", isNew, got, err)
	}
	reg.ClickID = "c2"
	reg.Username = "n2"
	isNew, got, err = st.ReportRegister(ctx, reg)
	if err != nil || isNew || got.ClickID != "c1" || got.Username != "n" {
		t.Fatalf("first-touch broken: isNew=%v got=%+v err=%v", isNew, got, err)
	}
}

package domain_test

import (
	"testing"
	"time"

	"minigameserver/internal/domain"
)

func TestBoardKeyStableAndChannelIsolated(t *testing.T) {
	a := domain.BoardKey("app", "clear_level", "default", "2026-W31", "tiktok_minis")
	b := domain.BoardKey("app", "clear_level", "default", "2026-W31", "tiktok_minis")
	c := domain.BoardKey("app", "clear_level", "default", "2026-W31", "other")
	if len(a) != 16 {
		t.Fatalf("key len=%d", len(a))
	}
	if string(a) != string(b) {
		t.Fatal("same input should same key")
	}
	if string(a) == string(c) {
		t.Fatal("channel must isolate boards")
	}
}

func TestPeriodKeyWeekMonthUTC(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, loc)
	w, err := domain.PeriodKey(domain.PeriodWeek, now, loc)
	if err != nil {
		t.Fatal(err)
	}
	m, err := domain.PeriodKey(domain.PeriodMonth, now, loc)
	if err != nil {
		t.Fatal(err)
	}
	if w != "2026-W31" {
		t.Fatalf("week=%s", w)
	}
	if m != "2026-07" {
		t.Fatalf("month=%s", m)
	}
}

func TestPeriodKeyDayAllAndRankType(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, loc)
	d, err := domain.PeriodKey(domain.PeriodDay, now, loc)
	if err != nil || d != "2026-07-31" {
		t.Fatalf("day=%s err=%v", d, err)
	}
	a, err := domain.PeriodKey(domain.PeriodAll, now, loc)
	if err != nil || a != "all" {
		t.Fatalf("all=%s err=%v", a, err)
	}
	rt, err := domain.ParseRankType("all")
	if err != nil || rt != domain.PeriodAll {
		t.Fatalf("rankType=%s err=%v", rt, err)
	}
	rel, err := domain.ParseRelationType("default")
	if err != nil || rel != domain.RelationAll {
		t.Fatalf("relation=%s err=%v", rel, err)
	}
	full, err := domain.ParseWriteRankTypes("")
	if err != nil || len(full) != 4 {
		t.Fatalf("full write=%v err=%v", full, err)
	}
	both, err := domain.ParseWriteRankTypes("both")
	if err != nil || len(both) != 2 {
		t.Fatalf("both=%v err=%v", both, err)
	}
	ch, err := domain.NormalizeChannel("  tiktok_minis ")
	if err != nil || ch != "tiktok_minis" {
		t.Fatalf("channel=%s err=%v", ch, err)
	}
	if _, err := domain.NormalizeChannel(""); err == nil {
		t.Fatal("empty channel must fail")
	}
}

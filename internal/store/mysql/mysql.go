package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"minigameserver/deployments"
	"minigameserver/internal/domain"
	"minigameserver/internal/store"
)

type Store struct {
	db *sql.DB
}

func Open(dsn string) (*Store, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(time.Hour)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) UpsertMax(ctx context.Context, e domain.Entry) (bool, int64, error) {
	var oldScore sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT score FROM rank_score WHERE board_key=? AND player_id=?`,
		e.BoardKey, e.PlayerID,
	).Scan(&oldScore)
	if err != nil && err != sql.ErrNoRows {
		return false, 0, err
	}

	zone := domain.NormalizeZone(e.ZoneID)
	_, err = s.db.ExecContext(ctx, `
INSERT INTO rank_score (
  board_key, app_id, board_id, zone_id, period_key, platform_kind, channel,
  player_id, score, extra, updated_at
) VALUES (?, ?, ?, ?, ?, 1, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
  score = IF(VALUES(score) > score, VALUES(score), score),
  updated_at = IF(VALUES(score) > score, VALUES(updated_at), updated_at),
  extra = IF(VALUES(score) > score, VALUES(extra), extra)
`, e.BoardKey, e.AppID, e.BoardID, zone, e.PeriodKey, e.Channel, e.PlayerID, e.Score, nullBytes(e.Extra), e.UpdatedAt)
	if err != nil {
		return false, 0, err
	}

	if !oldScore.Valid {
		return true, e.Score, nil
	}
	if e.Score > oldScore.Int64 {
		return true, e.Score, nil
	}
	return false, oldScore.Int64, nil
}

func nullBytes(b []byte) interface{} {
	if len(b) == 0 {
		return nil
	}
	return b
}

func (s *Store) TopN(ctx context.Context, boardKey []byte, n int) ([]domain.Entry, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT player_id, score, updated_at, extra
FROM rank_score
WHERE board_key=?
ORDER BY score DESC, updated_at ASC
LIMIT ?`, boardKey, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Entry
	for rows.Next() {
		var e domain.Entry
		var extra []byte
		if err := rows.Scan(&e.PlayerID, &e.Score, &e.UpdatedAt, &extra); err != nil {
			return nil, err
		}
		e.BoardKey = append([]byte(nil), boardKey...)
		e.Extra = extra
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) RankOf(ctx context.Context, boardKey []byte, playerID string) (int32, int64, bool, error) {
	var score, updatedAt int64
	err := s.db.QueryRowContext(ctx,
		`SELECT score, updated_at FROM rank_score WHERE board_key=? AND player_id=?`,
		boardKey, playerID,
	).Scan(&score, &updatedAt)
	if err == sql.ErrNoRows {
		return 0, 0, false, nil
	}
	if err != nil {
		return 0, 0, false, err
	}
	var better int64
	err = s.db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM rank_score
WHERE board_key=? AND (score > ? OR (score = ? AND updated_at < ?))`,
		boardKey, score, score, updatedAt,
	).Scan(&better)
	if err != nil {
		return 0, 0, false, err
	}
	return int32(better + 1), score, true, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) EnsureSchema(ctx context.Context) error {
	for _, stmt := range splitSQLStatements(deployments.SchemaSQL) {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	// Best-effort migrations for existing deployments created before avatar_url.
	_, _ = s.db.ExecContext(ctx, `ALTER TABLE player_register ADD COLUMN avatar_url VARCHAR(512) NOT NULL DEFAULT ''`)
	return nil
}

func splitSQLStatements(sqlText string) []string {
	parts := strings.Split(sqlText, ";")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

func (s *Store) GetAccount(ctx context.Context, appID, channel, openID string) (domain.PlayerRegister, bool, error) {
	accountID := domain.AccountID(channel, openID)
	var old domain.PlayerRegister
	err := s.db.QueryRowContext(ctx, `
SELECT app_id, channel, player_id, IFNULL(open_id,''), IFNULL(username,''), IFNULL(avatar_url,''), IFNULL(click_id,''),
       platform_kind, registered_at_ms, IFNULL(extra_json,'')
FROM player_register WHERE app_id=? AND player_id=?`, appID, accountID,
	).Scan(&old.AppID, &old.Channel, &old.PlayerID, &old.OpenID, &old.Username, &old.AvatarURL, &old.ClickID,
		&old.PlatformKind, &old.RegisteredAtMs, &old.ExtraJSON)
	if err == sql.ErrNoRows {
		return domain.PlayerRegister{}, false, nil
	}
	if err != nil {
		return domain.PlayerRegister{}, false, err
	}
	if old.OpenID == "" {
		old.OpenID = openID
	}
	hydrateAttrFromExtra(&old)
	return old, true, nil
}

func hydrateAttrFromExtra(old *domain.PlayerRegister) {
	if old == nil || old.ExtraJSON == "" {
		return
	}
	var m map[string]interface{}
	if json.Unmarshal([]byte(old.ExtraJSON), &m) != nil {
		return
	}
	if old.Username == "" {
		if v, ok := m["username"].(string); ok {
			old.Username = v
		}
		if v, ok := m["nickname"].(string); ok && old.Username == "" {
			old.Username = v
		}
	}
	if old.AvatarURL == "" {
		if v, ok := m["avatar_url"].(string); ok {
			old.AvatarURL = v
		} else if v, ok := m["avatar"].(string); ok {
			old.AvatarURL = v
		}
	}
	if old.ClickID == "" {
		if v, ok := m["click_id"].(string); ok && v != "" {
			old.ClickID = v
		} else if v, ok := m["clickid"].(string); ok {
			old.ClickID = v
		}
	}
}

func (s *Store) ReportRegister(ctx context.Context, r domain.PlayerRegister) (bool, domain.PlayerRegister, error) {
	if r.PlayerID == "" {
		r.PlayerID = domain.AccountID(r.Channel, r.OpenID)
	}
	var old domain.PlayerRegister
	err := s.db.QueryRowContext(ctx, `
SELECT app_id, channel, player_id, IFNULL(open_id,''), IFNULL(username,''), IFNULL(avatar_url,''), IFNULL(click_id,''),
       platform_kind, registered_at_ms, IFNULL(extra_json,'')
FROM player_register WHERE app_id=? AND player_id=?`, r.AppID, r.PlayerID,
	).Scan(&old.AppID, &old.Channel, &old.PlayerID, &old.OpenID, &old.Username, &old.AvatarURL, &old.ClickID,
		&old.PlatformKind, &old.RegisteredAtMs, &old.ExtraJSON)
	if err == nil {
		if old.OpenID == "" {
			old.OpenID = r.OpenID
		}
		hydrateAttrFromExtra(&old)
		return false, old, nil
	}
	if err != sql.ErrNoRows {
		return false, domain.PlayerRegister{}, err
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO player_register
  (app_id, channel, player_id, open_id, username, avatar_url, click_id, platform_kind, registered_at_ms, extra_json)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.AppID, r.Channel, r.PlayerID, r.OpenID, r.Username, r.AvatarURL, r.ClickID,
		r.PlatformKind, r.RegisteredAtMs, nullStr(r.ExtraJSON))
	if err != nil {
		err2 := s.db.QueryRowContext(ctx, `
SELECT app_id, channel, player_id, IFNULL(open_id,''), IFNULL(username,''), IFNULL(avatar_url,''), IFNULL(click_id,''),
       platform_kind, registered_at_ms, IFNULL(extra_json,'')
FROM player_register WHERE app_id=? AND player_id=?`, r.AppID, r.PlayerID,
		).Scan(&old.AppID, &old.Channel, &old.PlayerID, &old.OpenID, &old.Username, &old.AvatarURL, &old.ClickID,
			&old.PlatformKind, &old.RegisteredAtMs, &old.ExtraJSON)
		if err2 == nil {
			if old.OpenID == "" {
				old.OpenID = r.OpenID
			}
			hydrateAttrFromExtra(&old)
			return false, old, nil
		}
		return false, domain.PlayerRegister{}, err
	}
	return true, r, nil
}

func (s *Store) UpdateProfile(ctx context.Context, appID, channel, openID, username, avatarURL string) (domain.PlayerRegister, error) {
	accountID := domain.AccountID(channel, openID)
	old, ok, err := s.GetAccount(ctx, appID, channel, openID)
	if err != nil {
		return domain.PlayerRegister{}, err
	}
	if !ok {
		return domain.PlayerRegister{}, fmt.Errorf("account not found")
	}
	if username != "" {
		old.Username = username
	}
	if avatarURL != "" {
		old.AvatarURL = avatarURL
	}
	extra := old.ExtraJSON
	if username != "" || avatarURL != "" {
		var m map[string]interface{}
		if strings.TrimSpace(extra) != "" {
			_ = json.Unmarshal([]byte(extra), &m)
		}
		if m == nil {
			m = map[string]interface{}{}
		}
		if username != "" {
			m["username"] = username
			m["nickname"] = username
		}
		if avatarURL != "" {
			m["avatar_url"] = avatarURL
			m["avatar"] = avatarURL
		}
		if b, e := json.Marshal(m); e == nil {
			extra = string(b)
		}
	}
	_, err = s.db.ExecContext(ctx, `
UPDATE player_register
SET username=?, avatar_url=?, extra_json=?
WHERE app_id=? AND player_id=?`,
		old.Username, old.AvatarURL, nullStr(extra), appID, accountID)
	if err != nil {
		return domain.PlayerRegister{}, err
	}
	old.ExtraJSON = extra
	return old, nil
}

func nullStr(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func (s *Store) CountByChannel(ctx context.Context, appID string) (int64, []domain.ChannelCount, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT channel, COUNT(*) FROM player_register WHERE app_id=? GROUP BY channel ORDER BY COUNT(*) DESC, channel ASC`, appID)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()
	var out []domain.ChannelCount
	var total int64
	for rows.Next() {
		var c domain.ChannelCount
		if err := rows.Scan(&c.Channel, &c.Count); err != nil {
			return 0, nil, err
		}
		total += c.Count
		out = append(out, c)
	}
	return total, out, rows.Err()
}

var _ store.Store = (*Store)(nil)

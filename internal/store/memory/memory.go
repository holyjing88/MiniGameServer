package memory

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"minigameserver/internal/domain"
	"minigameserver/internal/store"
)

type Store struct {
	mu        sync.RWMutex
	data      map[string]map[string]domain.Entry // boardHex -> playerID -> entry
	registers map[string]domain.PlayerRegister   // appID|channel|openID -> reg
}

func New() *Store {
	return &Store{
		data:      make(map[string]map[string]domain.Entry),
		registers: make(map[string]domain.PlayerRegister),
	}
}

func regKey(appID, channel, openID string) string {
	return appID + "|" + channel + "|" + openID
}

func (s *Store) UpsertMax(_ context.Context, e domain.Entry) (bool, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	bk := domain.BoardKeyHex(e.BoardKey)
	m, ok := s.data[bk]
	if !ok {
		m = make(map[string]domain.Entry)
		s.data[bk] = m
	}
	old, exists := m[e.PlayerID]
	if !exists {
		m[e.PlayerID] = e
		return true, e.Score, nil
	}
	if e.Score > old.Score {
		m[e.PlayerID] = e
		return true, e.Score, nil
	}
	// Refresh display Extra (nick/avatar) even when score does not rise.
	// Empty Extra must not wipe an existing nickname payload.
	if len(e.Extra) > 0 {
		old.Extra = append([]byte(nil), e.Extra...)
		m[e.PlayerID] = old
		return true, old.Score, nil
	}
	return false, old.Score, nil
}

func (s *Store) TopN(_ context.Context, boardKey []byte, n int) ([]domain.Entry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m := s.data[domain.BoardKeyHex(boardKey)]
	list := make([]domain.Entry, 0, len(m))
	for _, e := range m {
		list = append(list, e)
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].Score != list[j].Score {
			return list[i].Score > list[j].Score
		}
		return list[i].UpdatedAt < list[j].UpdatedAt
	})
	if n > 0 && len(list) > n {
		list = list[:n]
	}
	return list, nil
}

func (s *Store) RankOf(_ context.Context, boardKey []byte, playerID string) (int32, int64, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m := s.data[domain.BoardKeyHex(boardKey)]
	me, ok := m[playerID]
	if !ok {
		return 0, 0, false, nil
	}
	var better int32
	for _, e := range m {
		if e.Score > me.Score || (e.Score == me.Score && e.UpdatedAt < me.UpdatedAt) {
			better++
		}
	}
	return better + 1, me.Score, true, nil
}

func (s *Store) GetAccount(_ context.Context, appID, channel, openID string) (domain.PlayerRegister, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.registers[regKey(appID, channel, openID)]
	return r, ok, nil
}

func (s *Store) ReportRegister(_ context.Context, r domain.PlayerRegister) (bool, domain.PlayerRegister, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := regKey(r.AppID, r.Channel, r.OpenID)
	if old, ok := s.registers[k]; ok {
		return false, old, nil
	}
	s.registers[k] = r
	return true, r, nil
}

func (s *Store) UpdateProfile(_ context.Context, appID, channel, openID, username, avatarURL string) (domain.PlayerRegister, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := regKey(appID, channel, openID)
	old, ok := s.registers[k]
	if !ok {
		return domain.PlayerRegister{}, fmt.Errorf("account not found")
	}
	if username != "" {
		old.Username = username
	}
	if avatarURL != "" {
		old.AvatarURL = avatarURL
	}
	s.registers[k] = old
	return old, nil
}

func (s *Store) CountByChannel(_ context.Context, appID string) (int64, []domain.ChannelCount, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	counts := map[string]int64{}
	var total int64
	for _, r := range s.registers {
		if r.AppID != appID {
			continue
		}
		counts[r.Channel]++
		total++
	}
	out := make([]domain.ChannelCount, 0, len(counts))
	for ch, n := range counts {
		out = append(out, domain.ChannelCount{Channel: ch, Count: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Channel < out[j].Channel
	})
	return total, out, nil
}

func (s *Store) Close() error { return nil }

var _ store.Store = (*Store)(nil)

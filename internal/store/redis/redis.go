// Package redis is a reserved RankStore stub for multi-instance later.
package redis

import (
	"context"
	"errors"

	"minigameserver/internal/domain"
	"minigameserver/internal/store"
)

var ErrNotImplemented = errors.New("redis RankStore not implemented yet")

type Store struct{}

func New() *Store { return &Store{} }

func (s *Store) UpsertMax(context.Context, domain.Entry) (bool, int64, error) {
	return false, 0, ErrNotImplemented
}
func (s *Store) TopN(context.Context, []byte, int) ([]domain.Entry, error) {
	return nil, ErrNotImplemented
}
func (s *Store) RankOf(context.Context, []byte, string) (int32, int64, bool, error) {
	return 0, 0, false, ErrNotImplemented
}
func (s *Store) ReportRegister(context.Context, domain.PlayerRegister) (bool, domain.PlayerRegister, error) {
	return false, domain.PlayerRegister{}, ErrNotImplemented
}
func (s *Store) GetAccount(context.Context, string, string, string) (domain.PlayerRegister, bool, error) {
	return domain.PlayerRegister{}, false, ErrNotImplemented
}
func (s *Store) CountByChannel(context.Context, string) (int64, []domain.ChannelCount, error) {
	return 0, nil, ErrNotImplemented
}
func (s *Store) Close() error { return nil }

var _ store.Store = (*Store)(nil)

package store

import (
	"context"

	"minigameserver/internal/domain"
)

// RankStore persists scores. Redis implementation is reserved (stub).
type RankStore interface {
	UpsertMax(ctx context.Context, e domain.Entry) (updated bool, best int64, err error)
	TopN(ctx context.Context, boardKey []byte, n int) ([]domain.Entry, error)
	RankOf(ctx context.Context, boardKey []byte, playerID string) (rank int32, score int64, ok bool, err error)
	Close() error
}

// PlayerStore persists registered accounts (unique by app + channel + open_id).
type PlayerStore interface {
	GetAccount(ctx context.Context, appID, channel, openID string) (reg domain.PlayerRegister, ok bool, err error)
	ReportRegister(ctx context.Context, r domain.PlayerRegister) (isNew bool, attributed domain.PlayerRegister, err error)
	// UpdateProfile updates nickname/avatar for an existing account. Empty fields are left unchanged.
	UpdateProfile(ctx context.Context, appID, channel, openID, username, avatarURL string) (reg domain.PlayerRegister, err error)
	CountByChannel(ctx context.Context, appID string) (total int64, rows []domain.ChannelCount, err error)
}

// Store is the combined persistence surface for MiniGameServer.
type Store interface {
	RankStore
	PlayerStore
}

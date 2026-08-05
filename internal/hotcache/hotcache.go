package hotcache

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"sync"
	"time"

	"minigameserver/internal/domain"
)

type Item struct {
	EntriesGzip []byte
	Entries     []domain.CompactEntry
	SnapshotTs  int64
	TopScoreMin int64
}

type Cache struct {
	mu    sync.RWMutex
	items map[string]*Item
}

func New() *Cache {
	return &Cache{items: make(map[string]*Item)}
}

func (c *Cache) Get(boardKeyHex string) (*Item, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	it, ok := c.items[boardKeyHex]
	if !ok || it == nil {
		return nil, false
	}
	cp := *it
	return &cp, true
}

func (c *Cache) Put(boardKeyHex string, entries []domain.CompactEntry, now time.Time) (*Item, error) {
	raw, err := json.Marshal(entries)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(raw); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	minScore := int64(0)
	if n := len(entries); n > 0 {
		minScore = entries[n-1].S
	}
	it := &Item{
		EntriesGzip: buf.Bytes(),
		Entries:     entries,
		SnapshotTs:  now.UnixMilli(),
		TopScoreMin: minScore,
	}
	c.mu.Lock()
	c.items[boardKeyHex] = it
	c.mu.Unlock()
	return it, nil
}

func (c *Cache) Invalidate(boardKeyHex string) {
	c.mu.Lock()
	delete(c.items, boardKeyHex)
	c.mu.Unlock()
}

func GunzipJSON(b []byte, v interface{}) error {
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return err
	}
	defer zr.Close()
	return json.NewDecoder(zr).Decode(v)
}

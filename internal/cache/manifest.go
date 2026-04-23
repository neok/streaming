package cache

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/singleflight"
)

type Loader func(ctx context.Context) ([]byte, error)

type Manifest struct {
	rdb   *redis.Client
	ttl   time.Duration
	group singleflight.Group
}

func NewManifest(rdb *redis.Client, ttl time.Duration) *Manifest {
	return &Manifest{rdb: rdb, ttl: ttl}
}

func (m *Manifest) Get(ctx context.Context, tenantID, videoID int64, name string, load Loader) ([]byte, bool, error) {
	key := fmt.Sprintf("manifest:%d:%d:%s", tenantID, videoID, name)

	if m.rdb != nil {
		if b, err := m.rdb.Get(ctx, key).Bytes(); err == nil {
			return b, true, nil
		} else if !errors.Is(err, redis.Nil) {
			return nil, false, err
		}
	}

	v, err, _ := m.group.Do(key, func() (any, error) {
		b, err := load(ctx)
		if err != nil {
			return nil, err
		}
		if m.rdb != nil {
			_ = m.rdb.Set(ctx, key, b, m.ttl).Err()
		}
		return b, nil
	})
	if err != nil {
		return nil, false, err
	}
	return v.([]byte), false, nil
}

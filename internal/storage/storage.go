package storage

import (
	"context"
	"io"
)

type Storage interface {
	StageDir(ctx context.Context, key string) (localPath string, finalize func(context.Context) error, err error)

	Open(ctx context.Context, key string) (io.ReadCloser, error)

	ReadFile(ctx context.Context, key string) ([]byte, error)

	Exists(ctx context.Context, key string) (bool, error)
}

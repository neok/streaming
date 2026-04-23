package storage

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

type Disk struct {
	Root string
}

func NewDisk(root string) *Disk {
	return &Disk{Root: root}
}

func (d *Disk) path(key string) string {
	return filepath.Join(d.Root, filepath.FromSlash(key))
}

func (d *Disk) StageDir(_ context.Context, key string) (string, func(context.Context) error, error) {
	p := d.path(key)
	if err := os.MkdirAll(p, 0o755); err != nil {
		return "", nil, err
	}
	return p, func(context.Context) error { return nil }, nil
}

func (d *Disk) Open(_ context.Context, key string) (io.ReadCloser, error) {
	f, err := os.Open(d.path(key))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	}
	return f, err
}

func (d *Disk) ReadFile(_ context.Context, key string) ([]byte, error) {
	b, err := os.ReadFile(d.path(key))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	}
	return b, err
}

func (d *Disk) Exists(_ context.Context, key string) (bool, error) {
	_, err := os.Stat(d.path(key))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return false, err
}

var ErrNotFound = errors.New("storage: not found")

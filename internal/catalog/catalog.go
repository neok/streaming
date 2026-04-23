package catalog

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("not found")

type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

type Video struct {
	ID       int64
	TenantID int64
	Title    string
	Source   string
	Status   string
	Error    string
}

func (r *Repo) TenantIDBySlug(ctx context.Context, slug string) (int64, error) {
	var id int64
	err := r.pool.QueryRow(ctx, `SELECT id FROM tenants WHERE slug = $1`, slug).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	return id, err
}

func (r *Repo) CreateVideo(ctx context.Context, tenantID int64, title, source string) (int64, error) {
	var id int64
	err := r.pool.QueryRow(ctx, `
		INSERT INTO videos (tenant_id, title, source_path)
		VALUES ($1, $2, $3)
		RETURNING id`, tenantID, title, source).Scan(&id)
	return id, err
}

func (r *Repo) SetStatus(ctx context.Context, id int64, status, errMsg string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE videos SET status = $2, error = $3, updated_at = NOW()
		WHERE id = $1`, id, status, nullable(errMsg))
	return err
}

func (r *Repo) GetVideo(ctx context.Context, id int64) (Video, error) {
	var v Video
	var errMsg *string
	err := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, title, source_path, status, error
		FROM videos WHERE id = $1`, id).
		Scan(&v.ID, &v.TenantID, &v.Title, &v.Source, &v.Status, &errMsg)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, ErrNotFound
	}
	if errMsg != nil {
		v.Error = *errMsg
	}
	return v, err
}

func (r *Repo) AddRendition(ctx context.Context, videoID int64, height, bitrateKbps int, manifestPath string) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO renditions (video_id, height, bitrate_kbps, manifest_path)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (video_id, height) DO UPDATE
		SET bitrate_kbps = EXCLUDED.bitrate_kbps, manifest_path = EXCLUDED.manifest_path`,
		videoID, height, bitrateKbps, manifestPath)
	return err
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

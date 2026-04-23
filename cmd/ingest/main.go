package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"

	"github.com/neok/streaming/internal/catalog"
	"github.com/neok/streaming/internal/config"
	"github.com/neok/streaming/internal/storage"
	"github.com/neok/streaming/internal/transcode"
	ingestv1 "github.com/neok/streaming/proto"
)

type server struct {
	ingestv1.UnimplementedIngestServer
	repo  *catalog.Repo
	store storage.Storage
}

func (s *server) Upload(ctx context.Context, req *ingestv1.UploadRequest) (*ingestv1.UploadResponse, error) {
	tenantID, err := s.repo.TenantIDBySlug(ctx, req.GetTenantSlug())
	if err != nil {
		return nil, err
	}
	id, err := s.repo.CreateVideo(ctx, tenantID, req.GetTitle(), req.GetSourcePath())
	if err != nil {
		return nil, err
	}
	go s.transcode(id, req.GetTenantSlug(), req.GetSourcePath())
	return &ingestv1.UploadResponse{VideoId: id}, nil
}

func (s *server) GetStatus(ctx context.Context, req *ingestv1.GetStatusRequest) (*ingestv1.GetStatusResponse, error) {
	v, err := s.repo.GetVideo(ctx, req.GetVideoId())
	if err != nil {
		return nil, err
	}
	return &ingestv1.GetStatusResponse{Status: v.Status, Error: v.Error}, nil
}

func (s *server) transcode(id int64, slug, source string) {
	ctx := context.Background()
	_ = s.repo.SetStatus(ctx, id, "transcoding", "")

	key := fmt.Sprintf("tenants/%s/videos/%d", slug, id)
	res, err := transcode.HLS(ctx, s.store, source, key, transcode.DefaultLadder)
	if err != nil {
		_ = s.repo.SetStatus(ctx, id, "failed", err.Error())
		return
	}
	for _, r := range res.Renditions {
		_ = s.repo.AddRendition(ctx, id, r.Height, r.BitrateKbps, r.ManifestKey)
	}
	_ = s.repo.SetStatus(ctx, id, "ready", "")
}

func main() {
	cfg, err := config.LoadIngest()
	if err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	lis, err := net.Listen("tcp", cfg.GRPCAddr)
	if err != nil {
		log.Fatal(err)
	}

	srv := grpc.NewServer()
	ingestv1.RegisterIngestServer(srv, &server{
		repo:  catalog.New(pool),
		store: storage.NewDisk(cfg.MediaDir),
	})

	go func() { _ = srv.Serve(lis) }()
	log.Printf("ingest listening on %s", cfg.GRPCAddr)

	<-ctx.Done()
	srv.GracefulStop()
}

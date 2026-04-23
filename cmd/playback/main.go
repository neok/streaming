package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/neok/streaming/internal/cache"
	"github.com/neok/streaming/internal/catalog"
	"github.com/neok/streaming/internal/config"
	"github.com/neok/streaming/internal/signing"
	"github.com/neok/streaming/internal/storage"
	"github.com/neok/streaming/internal/tenant"
)

type app struct {
	resolver tenant.Resolver
	store    storage.Storage
	cache    *cache.Manifest
	signer   *signing.Signer
	now      func() time.Time
}

func (a *app) clock() time.Time {
	if a.now != nil {
		return a.now()
	}
	return time.Now()
}

func main() {
	cfg, err := config.LoadPlayback()
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

	var rdb *redis.Client
	if cfg.RedisAddr != "" {
		rdb = redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
		defer rdb.Close()
	}

	a := &app{
		resolver: catalog.New(pool),
		store:    storage.NewDisk(cfg.MediaDir),
		cache:    cache.NewManifest(rdb, 5*time.Minute),
		signer:   signing.New(cfg.SigningSecret, cfg.SignatureTTL),
	}

	srv := &http.Server{Addr: cfg.HTTPAddr, Handler: a.router(), ReadHeaderTimeout: 5 * time.Second}

	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Println(err)
			stop()
		}
	}()
	log.Printf("playback listening on %s", cfg.HTTPAddr)

	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdown)
}

func (a *app) router() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {})

	withTenant := tenant.Middleware(a.resolver)
	mux.Handle("GET /tenants/{slug}/videos/{id}/master.m3u8", withTenant(http.HandlerFunc(a.master)))
	mux.Handle("GET /tenants/{slug}/videos/{id}/{rendition}/playlist.m3u8", withTenant(http.HandlerFunc(a.playlist)))
	mux.Handle("GET /tenants/{slug}/videos/{id}/{rendition}/{segment}", withTenant(http.HandlerFunc(a.segment)))
	return cors(mux)
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		next.ServeHTTP(w, r)
	})
}

func (a *app) videoKey(t tenant.Tenant, id string) string {
	return fmt.Sprintf("tenants/%s/videos/%s", t.Slug, id)
}

func (a *app) master(w http.ResponseWriter, r *http.Request) {
	t, _ := tenant.From(r.Context())
	id := r.PathValue("id")
	key := a.videoKey(t, id) + "/master.m3u8"

	b, _, err := a.cache.Get(r.Context(), t.ID, parseID(id), "master", func(ctx context.Context) ([]byte, error) {
		return a.store.ReadFile(ctx, key)
	})
	if err != nil {
		notFoundOr500(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	_, _ = w.Write(b)
}

func (a *app) playlist(w http.ResponseWriter, r *http.Request) {
	t, _ := tenant.From(r.Context())
	id := r.PathValue("id")
	rendition := r.PathValue("rendition")
	key := fmt.Sprintf("%s/%s/playlist.m3u8", a.videoKey(t, id), rendition)
	name := "playlist:" + rendition

	b, _, err := a.cache.Get(r.Context(), t.ID, parseID(id), name, func(ctx context.Context) ([]byte, error) {
		raw, err := a.store.ReadFile(ctx, key)
		if err != nil {
			return nil, err
		}
		base := fmt.Sprintf("/tenants/%s/videos/%s/%s/", t.Slug, id, rendition)
		return a.signer.RewriteSegments(raw, func(seg string) string { return base + seg }, a.clock()), nil
	})
	if err != nil {
		notFoundOr500(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	_, _ = w.Write(b)
}

func (a *app) segment(w http.ResponseWriter, r *http.Request) {
	t, _ := tenant.From(r.Context())
	id := r.PathValue("id")
	rendition := r.PathValue("rendition")
	seg := r.PathValue("segment")

	if !strings.HasSuffix(seg, ".ts") {
		http.NotFound(w, r)
		return
	}

	path := r.URL.Path
	if err := a.signer.Verify(path, r.URL.Query().Get("exp"), r.URL.Query().Get("sig"), a.clock()); err != nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	key := fmt.Sprintf("%s/%s/%s", a.videoKey(t, id), rendition, seg)
	rc, err := a.store.Open(r.Context(), key)
	if err != nil {
		notFoundOr500(w, r, err)
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", "video/mp2t")
	_, _ = io.Copy(w, rc)
}

func notFoundOr500(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, storage.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	http.Error(w, "internal", http.StatusInternalServerError)
}

func parseID(s string) int64 {
	var n int64
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int64(c-'0')
	}
	return n
}

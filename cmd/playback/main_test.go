package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/neok/streaming/internal/cache"
	"github.com/neok/streaming/internal/catalog"
	"github.com/neok/streaming/internal/signing"
	"github.com/neok/streaming/internal/storage"
)

type fakeResolver map[string]int64

func (f fakeResolver) TenantIDBySlug(_ context.Context, slug string) (int64, error) {
	id, ok := f[slug]
	if !ok {
		return 0, catalog.ErrNotFound
	}
	return id, nil
}

type fakeStore struct {
	files map[string][]byte
}

func newFakeStore() *fakeStore { return &fakeStore{files: map[string][]byte{}} }

func (f *fakeStore) put(key string, data []byte) { f.files[key] = data }

func (f *fakeStore) StageDir(context.Context, string) (string, func(context.Context) error, error) {
	return "", nil, errors.New("unused")
}

func (f *fakeStore) Open(_ context.Context, key string) (io.ReadCloser, error) {
	b, err := f.ReadFile(context.Background(), key)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (f *fakeStore) ReadFile(_ context.Context, key string) ([]byte, error) {
	b, ok := f.files[key]
	if !ok {
		return nil, storage.ErrNotFound
	}
	return b, nil
}

func (f *fakeStore) Exists(_ context.Context, key string) (bool, error) {
	_, ok := f.files[key]
	return ok, nil
}

const testSecret = "test-secret"

func newTestApp(store storage.Storage, now time.Time) *app {
	return &app{
		resolver: fakeResolver{"demo": 1, "acme": 2},
		store:    store,
		cache:    cache.NewManifest(nil, time.Minute),
		signer:   signing.New(testSecret, time.Hour),
		now:      func() time.Time { return now },
	}
}

func doGET(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestHealthz(t *testing.T) {
	a := newTestApp(newFakeStore(), time.Now())
	w := doGET(t, a.router(), "/healthz")
	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", w.Code)
	}
}

func TestMaster_UnknownTenant404(t *testing.T) {
	a := newTestApp(newFakeStore(), time.Now())
	w := doGET(t, a.router(), "/tenants/nope/videos/1/master.m3u8")
	if w.Code != http.StatusNotFound {
		t.Fatalf("got %d, want 404", w.Code)
	}
}

func TestMaster_MissingFile404(t *testing.T) {
	a := newTestApp(newFakeStore(), time.Now())
	w := doGET(t, a.router(), "/tenants/demo/videos/1/master.m3u8")
	if w.Code != http.StatusNotFound {
		t.Fatalf("got %d, want 404", w.Code)
	}
}

func TestMaster_Serves(t *testing.T) {
	store := newFakeStore()
	body := []byte("#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=5000000\n720p/playlist.m3u8\n")
	store.put("tenants/demo/videos/42/master.m3u8", body)

	a := newTestApp(store, time.Now())
	w := doGET(t, a.router(), "/tenants/demo/videos/42/master.m3u8")

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/vnd.apple.mpegurl" {
		t.Fatalf("content-type=%q", ct)
	}
	if !bytes.Equal(w.Body.Bytes(), body) {
		t.Fatalf("body mismatch")
	}
}

func TestPlaylist_SignsSegments(t *testing.T) {
	store := newFakeStore()
	playlist := []byte(strings.Join([]string{
		"#EXTM3U",
		"#EXT-X-VERSION:3",
		"#EXT-X-TARGETDURATION:6",
		"#EXTINF:6.0,",
		"seg_00000.ts",
		"#EXTINF:6.0,",
		"seg_00001.ts",
		"#EXT-X-ENDLIST",
		"",
	}, "\n"))
	store.put("tenants/demo/videos/42/720p/playlist.m3u8", playlist)

	a := newTestApp(store, time.Unix(1_700_000_000, 0))
	w := doGET(t, a.router(), "/tenants/demo/videos/42/720p/playlist.m3u8")
	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", w.Code)
	}

	out := w.Body.String()
	for _, seg := range []string{"seg_00000.ts", "seg_00001.ts"} {
		line := findSegmentLine(out, seg)
		if line == "" {
			t.Fatalf("segment %q not found in %q", seg, out)
		}
		if !strings.Contains(line, "?exp=") || !strings.Contains(line, "&sig=") {
			t.Fatalf("segment %q not signed: %q", seg, line)
		}
	}
}

func TestSegment_UnsignedForbidden(t *testing.T) {
	store := newFakeStore()
	store.put("tenants/demo/videos/42/720p/seg_00000.ts", []byte("bytes"))
	a := newTestApp(store, time.Now())
	w := doGET(t, a.router(), "/tenants/demo/videos/42/720p/seg_00000.ts")
	if w.Code != http.StatusForbidden {
		t.Fatalf("got %d, want 403", w.Code)
	}
}

func TestSegment_SignedServes(t *testing.T) {
	store := newFakeStore()
	data := []byte("bytes")
	store.put("tenants/demo/videos/42/720p/seg_00000.ts", data)

	now := time.Unix(1_700_000_000, 0)
	a := newTestApp(store, now)

	urlPath := "/tenants/demo/videos/42/720p/seg_00000.ts"
	exp, sig := a.signer.Sign(urlPath, now)
	w := doGET(t, a.router(), fmt.Sprintf("%s?exp=%s&sig=%s", urlPath, exp, sig))

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200, body=%q", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "video/mp2t" {
		t.Fatalf("content-type=%q", ct)
	}
	if !bytes.Equal(w.Body.Bytes(), data) {
		t.Fatalf("body mismatch")
	}
}

func TestSegment_ExpiredSignatureForbidden(t *testing.T) {
	store := newFakeStore()
	store.put("tenants/demo/videos/42/720p/seg_00000.ts", []byte("bytes"))

	signedAt := time.Unix(1_700_000_000, 0)
	a := newTestApp(store, signedAt.Add(2*time.Hour))

	urlPath := "/tenants/demo/videos/42/720p/seg_00000.ts"
	exp, sig := a.signer.Sign(urlPath, signedAt)
	w := doGET(t, a.router(), fmt.Sprintf("%s?exp=%s&sig=%s", urlPath, exp, sig))

	if w.Code != http.StatusForbidden {
		t.Fatalf("got %d, want 403", w.Code)
	}
}

func TestSegment_TamperedPathForbidden(t *testing.T) {
	store := newFakeStore()
	store.put("tenants/demo/videos/42/720p/seg_00001.ts", []byte("bytes"))

	now := time.Unix(1_700_000_000, 0)
	a := newTestApp(store, now)

	exp, sig := a.signer.Sign("/tenants/demo/videos/42/720p/seg_00000.ts", now)
	w := doGET(t, a.router(), fmt.Sprintf("/tenants/demo/videos/42/720p/seg_00001.ts?exp=%s&sig=%s", exp, sig))

	if w.Code != http.StatusForbidden {
		t.Fatalf("got %d, want 403", w.Code)
	}
}

func TestSegment_CrossTenantIsolation(t *testing.T) {
	store := newFakeStore()
	store.put("tenants/acme/videos/42/720p/seg_00000.ts", []byte("acme-secret"))

	now := time.Unix(1_700_000_000, 0)
	a := newTestApp(store, now)

	urlPath := "/tenants/demo/videos/42/720p/seg_00000.ts"
	exp, sig := a.signer.Sign(urlPath, now)
	w := doGET(t, a.router(), fmt.Sprintf("%s?exp=%s&sig=%s", urlPath, exp, sig))

	if w.Code != http.StatusNotFound {
		t.Fatalf("got %d, want 404", w.Code)
	}
}

func TestCORSHeader(t *testing.T) {
	a := newTestApp(newFakeStore(), time.Now())
	w := doGET(t, a.router(), "/healthz")
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("CORS header=%q", got)
	}
}

func TestParseID(t *testing.T) {
	cases := map[string]int64{
		"0":     0,
		"1":     1,
		"42":    42,
		"":      0,
		"abc":   0,
		"12a":   0,
		"99999": 99999,
	}
	for in, want := range cases {
		if got := parseID(in); got != want {
			t.Errorf("parseID(%q)=%d, want %d", in, got, want)
		}
	}
}

func findSegmentLine(playlist, segment string) string {
	for _, line := range strings.Split(playlist, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), segment) {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

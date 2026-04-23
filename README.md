# Go OTT Mini-Platform

A small multi-tenant VOD service in Go. You upload an MP4 over gRPC, it gets transcoded into HLS with three bitrate ladders (1080p / 720p / 480p), and a playback HTTP service hands out signed manifests and segments. Postgres holds the catalog, Redis caches manifests, and a tiny `hls.js` page plays it back.

Two services, one repo:

- **ingest** — gRPC. Takes an upload, runs FFmpeg, writes HLS output, updates status.
- **playback** — HTTP. Serves master/variant playlists and `.ts` segments. Rewrites segment URLs with an HMAC signature and expiry, so only signed requests can fetch bytes. Tenant isolation enforced in middleware.

Storage is behind an interface — today it's disk, swap in S3 later without touching callers.

<img height="400" alt="Screenshot 2026-04-22 184645" src="https://github.com/user-attachments/assets/8d984f11-639d-44ae-9f08-919eafce1bbc" />

---

## Prerequisites

- Docker + Docker Compose
- `grpcurl` (for manually kicking off an upload): `go install github.com/fullstorydev/grpcurl/cmd/grpcurl@latest`
- An MP4 file lying around somewhere

---

## Running the project

Bring everything up (postgres, redis, ingest, playback):

```bash
make up-all
make migrate
```

That seeds two tenants: `demo` and `acme`.

Quick health check:

```bash
curl -sf http://localhost:9005/healthz && echo OK
```

---

## Uploading a video

The ingest service reads the source file from its own filesystem, so copy your MP4 into the ingest container first:

```bash
docker compose cp ./myvideo.mp4 ingest:/tmp/myvideo.mp4
```

Then kick off the upload:

```bash
grpcurl -plaintext \
  -import-path proto -proto ingest.proto \
  -d '{"tenant_slug":"demo","title":"my video","source_path":"/tmp/myvideo.mp4"}' \
  localhost:9090 ingest.v1.Ingest/Upload
```

You'll get back a `videoId`. Poll status until it's `ready`:

```bash
grpcurl -plaintext \
  -import-path proto -proto ingest.proto \
  -d '{"video_id":1}' \
  localhost:9090 ingest.v1.Ingest/GetStatus
```

If something goes wrong, `docker compose logs -f ingest` shows the FFmpeg output.

---

## Watching the video

Open the demo player:

```bash
make web
```

Go to `http://localhost:9000`, leave tenant as `demo`, punch in your video ID, hit **Load**. You should see the player pick up 480p/720p/1080p automatically.

Or hit the manifest directly:

```bash
curl -s http://localhost:9005/tenants/demo/videos/1/master.m3u8
```

---

## Verifying the moving parts

**Signed segments** — fetching a `.ts` without a signature should be forbidden:

```bash
curl -i http://localhost:9005/tenants/demo/videos/1/720p/seg_00000.ts
# 403
```

Pull a signed URL out of the variant playlist and it works:

```bash
curl -s http://localhost:9005/tenants/demo/videos/1/720p/playlist.m3u8
# copy a seg_*.ts?exp=...&sig=... line and curl it — 200
```

**Tenant isolation** — unknown tenants are 404:

```bash
curl -i http://localhost:9005/tenants/nope/videos/1/master.m3u8
```

**Redis cache** — after hitting the master manifest a couple of times:

```bash
docker compose exec redis redis-cli KEYS 'manifest:*'
```

---

## Tests

```bash
go test -race ./...
```

The playback handler tests cover tenant resolution, signing, expiry, path tampering, and cross-tenant isolation — no postgres or redis required (fakes in-memory).

---

## Tearing down

```bash
docker compose down -v
```

The `-v` wipes the media volume too, which you'll want if you're changing ingest permissions.

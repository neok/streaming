package transcode

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/neok/streaming/internal/storage"
)

type Rendition struct {
	Height      int
	BitrateKbps int
}

var DefaultLadder = []Rendition{
	{Height: 1080, BitrateKbps: 5000},
	{Height: 720, BitrateKbps: 2800},
	{Height: 480, BitrateKbps: 1400},
}

type Result struct {
	Key        string
	Master     string
	Renditions []RenditionResult
}

type RenditionResult struct {
	Rendition
	ManifestKey string
}

func HLS(ctx context.Context, store storage.Storage, source, key string, ladder []Rendition) (Result, error) {
	outputDir, finalize, err := store.StageDir(ctx, key)
	if err != nil {
		return Result{}, err
	}

	hasAudio, err := probeHasAudio(ctx, source)
	if err != nil {
		return Result{}, err
	}

	args := []string{"-y", "-i", source, "-filter_complex", filterComplex(ladder)}

	for i, r := range ladder {
		args = append(args,
			"-map", fmt.Sprintf("[v%dout]", i),
			fmt.Sprintf("-c:v:%d", i), "libx264",
			fmt.Sprintf("-b:v:%d", i), fmt.Sprintf("%dk", r.BitrateKbps),
		)
		if hasAudio {
			args = append(args, "-map", "a:0")
		}
	}

	streamMap := ""
	for i, r := range ladder {
		if i > 0 {
			streamMap += " "
		}
		if hasAudio {
			streamMap += fmt.Sprintf("v:%d,a:%d,name:%dp", i, i, r.Height)
		} else {
			streamMap += fmt.Sprintf("v:%d,name:%dp", i, r.Height)
		}
	}

	if hasAudio {
		args = append(args, "-c:a", "aac", "-b:a", "128k")
	}
	args = append(args,
		"-f", "hls",
		"-hls_time", "6",
		"-hls_playlist_type", "vod",
		"-hls_segment_filename", filepath.Join(outputDir, "%v/seg_%05d.ts"),
		"-master_pl_name", "master.m3u8",
		"-var_stream_map", streamMap,
		filepath.Join(outputDir, "%v/playlist.m3u8"),
	)

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return Result{}, fmt.Errorf("ffmpeg: %w", err)
	}

	if err := finalize(ctx); err != nil {
		return Result{}, fmt.Errorf("finalize: %w", err)
	}

	res := Result{
		Key:    key,
		Master: key + "/master.m3u8",
	}
	for _, r := range ladder {
		res.Renditions = append(res.Renditions, RenditionResult{
			Rendition:   r,
			ManifestKey: fmt.Sprintf("%s/%dp/playlist.m3u8", key, r.Height),
		})
	}
	return res, nil
}
func probeHasAudio(ctx context.Context, source string) (bool, error) {
	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "error",
		"-select_streams", "a:0",
		"-show_entries", "stream=index",
		"-of", "csv=p=0",
		source,
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("ffprobe failed for %q: %w: %s", source, err, strings.TrimSpace(string(out)))
	}

	return strings.TrimSpace(string(out)) != "", nil
}

func filterComplex(ladder []Rendition) string {
	split := fmt.Sprintf("[0:v]split=%d", len(ladder))
	for i := range ladder {
		split += fmt.Sprintf("[v%d]", i)
	}
	split += ";"
	for i, r := range ladder {
		split += fmt.Sprintf("[v%d]scale=-2:%d[v%dout]", i, r.Height, i)
		if i < len(ladder)-1 {
			split += ";"
		}
	}
	return split
}

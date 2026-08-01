package media

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type timeoutProcessingStore struct {
	t       *testing.T
	mu      sync.Mutex
	deleted []string
}

func (s *timeoutProcessingStore) DownloadToFileVersion(context.Context, string, string) (string, func(), error) {
	s.t.Helper()
	path := filepath.Join(s.t.TempDir(), "source.mp4")
	if err := os.WriteFile(path, []byte("source"), 0o600); err != nil {
		s.t.Fatalf("writing processor source fixture: %v", err)
	}
	return path, func() { _ = os.Remove(path) }, nil
}

func (*timeoutProcessingStore) PutObject(context.Context, string, []byte, string) error {
	return errors.New("processor must time out before writing HLS output")
}

func (s *timeoutProcessingStore) DeletePrefix(_ context.Context, prefix string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleted = append(s.deleted, prefix)
	return nil
}

func (s *timeoutProcessingStore) deletedPrefixes() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.deleted...)
}

func TestFFmpegProcessorBoundsCommandContextAndCleansTimedOutOutput(t *testing.T) {
	script := filepath.Join(t.TempDir(), "block-processor.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexec /bin/sleep 10\n"), 0o700); err != nil {
		t.Fatalf("writing blocking processor fixture: %v", err)
	}

	cases := []struct {
		name    string
		timeout time.Duration
		context func() (context.Context, context.CancelFunc)
	}{
		{
			name:    "configured timeout",
			timeout: 20 * time.Millisecond,
			context: func() (context.Context, context.CancelFunc) { return context.Background(), func() {} },
		},
		{
			name:    "caller cancellation",
			timeout: time.Second,
			context: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx, func() {}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &timeoutProcessingStore{t: t}
			processor, err := NewFFmpegProcessor(store, script, script, tc.timeout)
			if err != nil {
				t.Fatalf("NewFFmpegProcessor: %v", err)
			}
			ctx, cancel := tc.context()
			defer cancel()
			started := time.Now()
			if _, err := processor.Transcode(ctx, ObjectVersion{AssetVersionID: "version-1", StorageObjectKey: "quarantine/version-1", StorageObjectVersion: "object-v1"}); err == nil {
				t.Fatal("timed-out processor unexpectedly succeeded")
			}
			if elapsed := time.Since(started); elapsed > time.Second {
				t.Fatalf("CommandContext was not bounded; processing took %s", elapsed)
			}
			if got := store.deletedPrefixes(); len(got) != 1 || got[0] != "media/version-1/hls" {
				t.Fatalf("partial output cleanup = %v, want [media/version-1/hls]", got)
			}
		})
	}
}

func TestFFmpegProcessorRejectsInvalidTimeout(t *testing.T) {
	store := &timeoutProcessingStore{t: t}
	if _, err := NewFFmpegProcessor(store, "ffmpeg", "ffprobe", -time.Second); err == nil {
		t.Fatal("negative processing timeout was accepted")
	}
}

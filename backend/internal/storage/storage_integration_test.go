//go:build integration

package storage

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"
	"time"
)

var httpClient = &http.Client{Timeout: 30 * time.Second}

func testOptions() Options {
	return Options{
		Endpoint:     "http://localhost:9000",
		AccessKey:    "gradexminio",
		SecretKey:    "gradexminio",
		Bucket:       "gradex-video",
		Region:       "us-east-1",
		UsePathStyle: true,
	}
}

func TestPresignPutAndGet_RealMinIO(t *testing.T) {
	ctx := context.Background()
	c, err := New(ctx, testOptions())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	key := "integration-test/hello.txt"
	body := []byte("hello from gradex video pipeline integration test")

	putURL, err := c.PresignPutURL(ctx, key, "text/plain", 5*time.Minute)
	if err != nil {
		t.Fatalf("PresignPutURL: %v", err)
	}

	req, err := http.NewRequest(http.MethodPut, putURL, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("building PUT request: %v", err)
	}
	req.Header.Set("Content-Type", "text/plain")
	resp, err := httpClient.Do(req)
	if err != nil {
		t.Fatalf("PUT to presigned URL failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("PUT returned %d: %s", resp.StatusCode, respBody)
	}

	size, exists, err := c.HeadObject(ctx, key)
	if err != nil {
		t.Fatalf("HeadObject: %v", err)
	}
	if !exists {
		t.Fatalf("object %q does not exist after PUT", key)
	}
	if size != int64(len(body)) {
		t.Fatalf("expected size %d, got %d", len(body), size)
	}

	getURL, err := c.PresignGetURLUntil(ctx, key, time.Now().UTC().Add(5*time.Minute))
	if err != nil {
		t.Fatalf("PresignGetURLUntil: %v", err)
	}
	getReq, err := http.NewRequest(http.MethodGet, getURL, nil)
	if err != nil {
		t.Fatalf("building GET request: %v", err)
	}
	getResp, err := httpClient.Do(getReq)
	if err != nil {
		t.Fatalf("GET presigned URL failed: %v", err)
	}
	defer getResp.Body.Close()
	got, err := io.ReadAll(getResp.Body)
	if err != nil {
		t.Fatalf("reading GET body: %v", err)
	}
	if string(got) != string(body) {
		t.Fatalf("round-tripped content mismatch: got %q, want %q", got, body)
	}
}

func TestProtectedMediaObjectsRejectUnsignedMinIOReads(t *testing.T) {
	ctx := context.Background()
	c, err := New(ctx, testOptions())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	keys := []string{
		"integration-protected/hls/master.m3u8",
		"integration-protected/hls/720p/segment-0001.ts",
		"integration-protected/resources/guide.pdf",
		"integration-protected/labs/starter.zip",
		"integration-protected/previews/preview.m3u8",
	}
	for _, key := range keys {
		if err := c.PutObject(ctx, key, []byte("private "+key), "application/octet-stream"); err != nil {
			t.Fatalf("PutObject(%q): %v", key, err)
		}
		unsigned, err := http.NewRequest(http.MethodGet, "http://localhost:9000/gradex-video/"+key, nil)
		if err != nil {
			t.Fatalf("building unsigned GET for %q: %v", key, err)
		}
		response, err := httpClient.Do(unsigned)
		if err != nil {
			t.Fatalf("unsigned GET for %q: %v", key, err)
		}
		_, _ = io.Copy(io.Discard, response.Body)
		response.Body.Close()
		if response.StatusCode < http.StatusBadRequest || response.StatusCode == http.StatusOK {
			t.Fatalf("unsigned GET for private %q returned %d, want refusal", key, response.StatusCode)
		}
		presigned, err := c.PresignGetURL(ctx, key, time.Minute)
		if err != nil {
			t.Fatalf("PresignGetURL(%q): %v", key, err)
		}
		allowed, err := httpClient.Get(presigned)
		if err != nil {
			t.Fatalf("signed GET for %q: %v", key, err)
		}
		_, _ = io.Copy(io.Discard, allowed.Body)
		allowed.Body.Close()
		if allowed.StatusCode != http.StatusOK {
			t.Fatalf("signed GET for %q returned %d, want 200", key, allowed.StatusCode)
		}
	}
}

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
	resp, err := http.DefaultClient.Do(req)
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

	getURL, err := c.PresignGetURL(ctx, key, 5*time.Minute)
	if err != nil {
		t.Fatalf("PresignGetURL: %v", err)
	}
	getResp, err := http.Get(getURL)
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

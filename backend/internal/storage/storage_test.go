package storage

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestClientSeparatesInternalOperationsFromBrowserPresigning(t *testing.T) {
	var (
		mu       sync.Mutex
		requests []string
	)
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.Method+" "+r.URL.Path)
		mu.Unlock()
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, "stored bytes")
		}
	}))
	t.Cleanup(internal.Close)

	client, err := New(context.Background(), Options{
		Endpoint:        internal.URL,
		PresignEndpoint: "https://storage.gradex.example",
		AccessKey:       "access",
		SecretKey:       "secret",
		Bucket:          "private-media",
		Region:          "us-east-1",
		UsePathStyle:    true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := context.Background()
	if err := client.PutObject(ctx, "lesson/video.mp4", []byte("stored bytes"), "video/mp4"); err != nil {
		t.Fatalf("PutObject: %v", err)
	}
	if _, _, err := client.HeadObject(ctx, "lesson/video.mp4"); err != nil {
		t.Fatalf("HeadObject: %v", err)
	}
	if _, err := client.DownloadObject(ctx, "lesson/video.mp4"); err != nil {
		t.Fatalf("DownloadObject: %v", err)
	}

	wantRequests := []string{
		"PUT /private-media/lesson/video.mp4",
		"HEAD /private-media/lesson/video.mp4",
		"GET /private-media/lesson/video.mp4",
	}
	mu.Lock()
	gotRequests := append([]string(nil), requests...)
	mu.Unlock()
	if !reflect.DeepEqual(gotRequests, wantRequests) {
		t.Fatalf("internal requests = %v, want %v", gotRequests, wantRequests)
	}

	assertPresignedOrigin(t, client, http.MethodPut, "https", "storage.gradex.example")
	assertPresignedOrigin(t, client, http.MethodGet, "https", "storage.gradex.example")
}

func TestPresigningDefaultsToTheInternalEndpoint(t *testing.T) {
	client, err := New(context.Background(), Options{
		Endpoint:     "http://minio.internal:9000",
		AccessKey:    "access",
		SecretKey:    "secret",
		Bucket:       "private-media",
		Region:       "us-east-1",
		UsePathStyle: true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	assertPresignedOrigin(t, client, http.MethodPut, "http", "minio.internal:9000")
}

func TestDownloadPresigningUsesASafeAttachmentFilename(t *testing.T) {
	client, err := New(context.Background(), Options{
		Endpoint: "https://storage.gradex.example", AccessKey: "access", SecretKey: "secret",
		Bucket: "private-media", Region: "us-east-1", UsePathStyle: true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	filename := "  notes\\../\"unsafe\r\nملف.pdf  "
	rawURL, err := client.PresignGetDownloadURL(context.Background(), "private/object", filename, time.Minute)
	if err != nil {
		t.Fatalf("PresignGetDownloadURL: %v", err)
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse presigned URL: %v", err)
	}
	disposition := parsed.Query().Get("response-content-disposition")
	if disposition == "" || !strings.HasPrefix(disposition, "attachment; filename=") {
		t.Fatalf("content disposition=%q", disposition)
	}
	for _, unsafe := range []string{"\r", "\n", "\\"} {
		if strings.Contains(disposition, unsafe) {
			t.Fatalf("content disposition leaked unsafe filename character %q: %q", unsafe, disposition)
		}
	}
	if !strings.Contains(disposition, "filename*=UTF-8''") || !strings.Contains(disposition, "%D9%85") {
		t.Fatalf("content disposition did not retain a UTF-8 filename parameter: %q", disposition)
	}
}

func assertPresignedOrigin(t *testing.T, client *Client, method, scheme, host string) {
	t.Helper()
	var (
		rawURL string
		err    error
	)
	if method == http.MethodPut {
		rawURL, err = client.PresignPutURL(context.Background(), "lesson/video.mp4", "video/mp4", time.Minute)
	} else {
		rawURL, err = client.PresignGetURL(context.Background(), "lesson/video.mp4", time.Minute)
	}
	if err != nil {
		t.Fatalf("presign %s: %v", method, err)
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse presigned %s URL: %v", method, err)
	}
	if parsed.Scheme != scheme || parsed.Host != host {
		t.Fatalf("presigned %s origin = %s://%s, want %s://%s", method, parsed.Scheme, parsed.Host, scheme, host)
	}
	if parsed.Path != "/private-media/lesson/video.mp4" {
		t.Fatalf("presigned %s path = %q", method, parsed.Path)
	}
	if parsed.Query().Get("X-Amz-Signature") == "" {
		t.Fatalf("presigned %s URL has no signature", method)
	}
}

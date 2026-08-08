//go:build provider

package storage

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

var r2ProviderHTTPClient = &http.Client{
	Timeout: 15 * time.Second,
	CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

func TestCloudflareR2PreservesPrivateVersionBoundMediaContract(t *testing.T) {
	t.Parallel()

	endpoint := requiredProviderEnvironment(t, "S3_ENDPOINT")
	bucket := requiredProviderEnvironment(t, "S3_BUCKET")
	accessKey := requiredProviderEnvironment(t, "S3_ACCESS_KEY")
	secretKey := requiredProviderEnvironment(t, "S3_SECRET_KEY")
	publicOrigin := requiredProviderEnvironment(t, "PUBLIC_ORIGIN")
	validateR2ProviderTarget(t, endpoint, publicOrigin)

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	store, err := New(ctx, Options{
		Endpoint: endpoint, AccessKey: accessKey, SecretKey: secretKey,
		Bucket: bucket, Region: "auto", UsePathStyle: false,
	})
	if err != nil {
		t.Fatalf("constructing R2 client: %v", err)
	}
	if err := store.CheckBucket(ctx); err != nil {
		t.Fatalf("checking private R2 bucket: %v", err)
	}

	prefix := "provider-proof/" + randomProviderSuffix(t) + "/"
	key := prefix + "source.mp4"
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cleanupCancel()
		if err := store.DeletePrefix(cleanupCtx, prefix); err != nil {
			t.Errorf("cleaning provider proof prefix: %v", err)
		}
	}()

	first := []byte("gradex-r2-provider-version-one")
	firstVersion, unsignedURL := putPresignedProviderObject(t, ctx, store, key, first)
	assertProviderCORS(t, ctx, unsignedURL, publicOrigin)
	assertUnsignedProviderReadDenied(t, ctx, unsignedURL)
	assertProviderVersion(t, ctx, store, key, firstVersion, first)

	second := []byte("gradex-r2-provider-version-two-is-distinct")
	secondVersion, _ := putPresignedProviderObject(t, ctx, store, key, second)
	if firstVersion == secondVersion {
		t.Fatal("R2 returned the same object version after replacement")
	}
	assertProviderVersion(t, ctx, store, key, secondVersion, second)

	old, err := store.DownloadPrefixVersion(ctx, key, firstVersion, int64(len(second)))
	if err == nil && !bytes.Equal(old, first) {
		t.Fatal("R2 silently substituted current bytes for the requested historical object version")
	}
}

func requiredProviderEnvironment(t *testing.T, name string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		t.Fatalf("%s is required for the explicit provider integration test", name)
	}
	return value
}

func validateR2ProviderTarget(t *testing.T, endpoint, publicOrigin string) {
	t.Helper()
	parsedEndpoint, err := url.Parse(endpoint)
	if err != nil || parsedEndpoint.Scheme != "https" || parsedEndpoint.User != nil ||
		parsedEndpoint.RawQuery != "" || parsedEndpoint.Path != "" ||
		!strings.HasSuffix(parsedEndpoint.Hostname(), ".r2.cloudflarestorage.com") {
		t.Fatal("S3_ENDPOINT must be a credential-free Cloudflare R2 HTTPS API origin")
	}
	parsedOrigin, err := url.Parse(publicOrigin)
	if err != nil || parsedOrigin.Scheme != "https" || parsedOrigin.User != nil ||
		parsedOrigin.RawQuery != "" || parsedOrigin.Path != "" || parsedOrigin.Hostname() == "" {
		t.Fatal("PUBLIC_ORIGIN must be a credential-free HTTPS origin")
	}
}

func randomProviderSuffix(t *testing.T) string {
	t.Helper()
	random := make([]byte, 12)
	if _, err := rand.Read(random); err != nil {
		t.Fatalf("generating provider proof key: %v", err)
	}
	return hex.EncodeToString(random)
}

func putPresignedProviderObject(t *testing.T, ctx context.Context, store *Client, key string, body []byte) (string, string) {
	t.Helper()
	presigned, err := store.PresignPutURL(ctx, key, "video/mp4", 5*time.Minute)
	if err != nil {
		t.Fatalf("presigning provider upload: %v", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, presigned, bytes.NewReader(body))
	if err != nil {
		t.Fatal("constructing provider upload request")
	}
	req.Header.Set("Content-Type", "video/mp4")
	response, err := r2ProviderHTTPClient.Do(req)
	if err != nil {
		t.Fatal("provider upload request failed")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, response.Body)
		t.Fatalf("provider upload returned HTTP %d", response.StatusCode)
	}
	version := strings.TrimSpace(response.Header.Get("x-amz-version-id"))
	if version == "" {
		t.Fatal("R2 upload omitted x-amz-version-id required by the frozen media provenance contract")
	}
	parsed, err := url.Parse(presigned)
	if err != nil {
		t.Fatal("parsing provider upload URL")
	}
	parsed.RawQuery = ""
	return version, parsed.String()
}

func assertProviderVersion(t *testing.T, ctx context.Context, store *Client, key, version string, expected []byte) {
	t.Helper()
	size, exists, err := store.HeadObjectVersion(ctx, key, version)
	if err != nil || !exists || size != int64(len(expected)) {
		t.Fatalf("exact provider object version head failed: exists=%t size=%d err=%v", exists, size, err)
	}
	got, err := store.DownloadPrefixVersion(ctx, key, version, int64(len(expected)+1))
	if err != nil {
		t.Fatalf("reading exact provider object version: %v", err)
	}
	if !bytes.Equal(got, expected) {
		t.Fatal("exact provider object version bytes differ")
	}
}

func assertProviderCORS(t *testing.T, ctx context.Context, objectURL, publicOrigin string) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodOptions, objectURL, nil)
	if err != nil {
		t.Fatal("constructing provider CORS request")
	}
	req.Header.Set("Origin", publicOrigin)
	req.Header.Set("Access-Control-Request-Method", http.MethodPut)
	req.Header.Set("Access-Control-Request-Headers", "content-type")
	response, err := r2ProviderHTTPClient.Do(req)
	if err != nil {
		t.Fatal("provider CORS request failed")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		t.Fatalf("provider CORS preflight returned HTTP %d", response.StatusCode)
	}
	if response.Header.Get("Access-Control-Allow-Origin") != publicOrigin {
		t.Fatal("provider CORS did not return the exact staging origin")
	}
	if !strings.Contains(response.Header.Get("Access-Control-Allow-Methods"), http.MethodPut) {
		t.Fatal("provider CORS does not allow the required direct upload method")
	}
}

func assertUnsignedProviderReadDenied(t *testing.T, ctx context.Context, objectURL string) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, objectURL, nil)
	if err != nil {
		t.Fatal("constructing unsigned provider read request")
	}
	response, err := r2ProviderHTTPClient.Do(req)
	if err != nil {
		t.Fatal("unsigned provider read request failed")
	}
	defer response.Body.Close()
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		t.Fatal("private R2 object was anonymously readable")
	}
}

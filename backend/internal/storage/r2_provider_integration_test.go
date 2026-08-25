//go:build provider

package storage

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

var r2ProviderHTTPClient = &http.Client{
	Timeout: 15 * time.Second,
	CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

func TestCloudflareR2PreservesPrivateImmutableObjectIdentityContract(t *testing.T) {
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
	firstKey := prefix + "asset-version-a/source.mp4"
	secondKey := prefix + "asset-version-b/source.mp4"
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cleanupCancel()
		if err := store.DeletePrefix(cleanupCtx, prefix); err != nil {
			t.Errorf("cleaning provider proof prefix: %v", err)
		}
	}()

	first := []byte("A")
	firstUpload := putPresignedProviderObject(t, ctx, store, firstKey, first, publicOrigin)
	assertProviderCORS(t, ctx, firstUpload.objectURL, publicOrigin)
	assertUnsignedProviderReadDenied(t, ctx, firstUpload.objectURL)
	assertProviderIdentity(t, ctx, store, firstKey, firstUpload, first)
	t.Log("upload A: success; ETag identity recorded; exact HEAD, GET, hash, and file read returned A")

	second := []byte("B")
	secondUpload := putPresignedProviderObject(t, ctx, store, secondKey, second, publicOrigin)
	assertProviderIdentity(t, ctx, store, secondKey, secondUpload, second)
	current, err := store.DownloadPrefix(ctx, secondKey, int64(len(second)+1))
	if err != nil || !bytes.Equal(current, second) {
		t.Fatal("separate asset-version key did not return its current bytes")
	}

	// A replacement at the same key is intentionally a failed proof for the old
	// identity; separate Asset Version keys remain independently addressable.
	replacement := []byte("replacement")
	_ = putPresignedProviderObject(t, ctx, store, firstKey, replacement, publicOrigin)
	assertProviderIdentityRejected(t, ctx, store, firstKey, objectIdentityETagPrefix+firstUpload.etag)
	current, err = store.DownloadPrefix(ctx, firstKey, int64(len(replacement)+1))
	if err != nil || !bytes.Equal(current, replacement) {
		t.Fatal("current provider object did not resolve to replacement bytes")
	}
	assertProviderIdentity(t, ctx, store, secondKey, secondUpload, second)
	t.Log("overwrite at one key failed the old ETag identity; separate asset-version key remained readable")

}

type providerUpload struct {
	etag      string
	objectURL string
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

func putPresignedProviderObject(t *testing.T, ctx context.Context, store *Client, key string, body []byte, publicOrigin string) providerUpload {
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
	req.Header.Set("Origin", publicOrigin)
	response, err := r2ProviderHTTPClient.Do(req)
	if err != nil {
		t.Fatal("provider upload request failed")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, response.Body)
		t.Fatalf("provider upload returned HTTP %d", response.StatusCode)
	}
	etag := strings.TrimSpace(response.Header.Get("ETag"))
	if etag == "" {
		t.Fatal("R2 upload omitted ETag metadata")
	}
	if response.Header.Get("Access-Control-Allow-Origin") != publicOrigin {
		t.Fatal("provider upload response did not allow the exact staging origin")
	}
	if !headerContainsToken(response.Header.Get("Access-Control-Expose-Headers"), "etag") ||
		!headerContainsToken(response.Header.Get("Access-Control-Expose-Headers"), "x-amz-version-id") {
		t.Fatal("provider upload response did not expose ETag and x-amz-version-id to the browser")
	}
	parsed, err := url.Parse(presigned)
	if err != nil {
		t.Fatal("parsing provider upload URL")
	}
	parsed.RawQuery = ""
	return providerUpload{etag: etag, objectURL: parsed.String()}
}

func assertProviderIdentity(t *testing.T, ctx context.Context, store *Client, key string, upload providerUpload, expected []byte) {
	t.Helper()
	identity := objectIdentityETagPrefix + upload.etag
	size, exists, err := store.HeadObjectVersion(ctx, key, identity)
	if err != nil || !exists || size != int64(len(expected)) {
		t.Fatalf("exact ETag identity HEAD failed: exists=%t size=%d error=%v", exists, size, err)
	}
	got, err := store.DownloadPrefixVersion(ctx, key, identity, int64(len(expected)+1))
	if err != nil {
		t.Fatalf("reading exact ETag identity failed: %v", err)
	}
	if !bytes.Equal(got, expected) {
		t.Fatal("exact ETag identity bytes differ")
	}
	actualHash, err := store.HashObjectVersion(ctx, key, identity)
	if err != nil {
		t.Fatalf("hashing exact ETag identity failed: %v", err)
	}
	digest := sha256.Sum256(expected)
	if actualHash != hex.EncodeToString(digest[:]) {
		t.Fatalf("exact ETag identity hash = %q, want %q", actualHash, hex.EncodeToString(digest[:]))
	}
	path, cleanup, err := store.DownloadToFileVersion(ctx, key, identity)
	if err != nil {
		t.Fatalf("downloading exact ETag identity to file failed: %v", err)
	}
	defer cleanup()
	contents, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(contents, expected) {
		t.Fatalf("exact ETag identity file bytes = %q, want %q (error=%v)", contents, expected, err)
	}
}

func assertProviderIdentityRejected(t *testing.T, ctx context.Context, store *Client, key, identity string) {
	t.Helper()
	checks := []struct {
		name string
		read func() error
	}{
		{name: "HEAD", read: func() error { _, _, err := store.HeadObjectVersion(ctx, key, identity); return err }},
		{name: "prefix GET", read: func() error { _, err := store.DownloadPrefixVersion(ctx, key, identity, 1024); return err }},
		{name: "hash GET", read: func() error { _, err := store.HashObjectVersion(ctx, key, identity); return err }},
		{name: "file GET", read: func() error {
			_, cleanup, err := store.DownloadToFileVersion(ctx, key, identity)
			if cleanup != nil {
				cleanup()
			}
			return err
		}},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			assertProviderPreconditionFailed(t, check.read())
		})
	}
}

func assertProviderPreconditionFailed(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("stale ETag identity unexpectedly succeeded")
	}
	var responseError *smithyhttp.ResponseError
	if errors.As(err, &responseError) && responseError.HTTPStatusCode() == http.StatusPreconditionFailed {
		return
	}
	var apiError smithy.APIError
	if errors.As(err, &apiError) && apiError.ErrorCode() == "PreconditionFailed" {
		return
	}
	t.Fatalf("stale ETag identity error = %v, want PreconditionFailed", err)
}

func headerContainsToken(header, want string) bool {
	for _, value := range strings.Split(header, ",") {
		if strings.EqualFold(strings.TrimSpace(value), want) {
			return true
		}
	}
	return false
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

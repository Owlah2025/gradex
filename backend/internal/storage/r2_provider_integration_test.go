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

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
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

	first := []byte("A")
	firstUpload := putPresignedProviderObject(t, ctx, store, key, first, publicOrigin)
	assertProviderCORS(t, ctx, firstUpload.objectURL, publicOrigin)
	assertUnsignedProviderReadDenied(t, ctx, firstUpload.objectURL)
	assertProviderVersion(t, ctx, store, key, firstUpload, first)
	t.Log("upload A: success; ETag present; provider VersionId present; exact HEAD and GET returned A")

	second := []byte("B")
	secondUpload := putPresignedProviderObject(t, ctx, store, key, second, publicOrigin)
	if firstUpload.versionID == secondUpload.versionID {
		t.Fatal("R2 returned the same object version after replacement")
	}
	if firstUpload.etag == secondUpload.etag {
		t.Fatal("R2 returned the same ETag for distinct object bytes")
	}
	current, err := store.DownloadPrefix(ctx, key, int64(len(second)+1))
	if err != nil || !bytes.Equal(current, second) {
		t.Fatal("current provider object did not resolve to replacement bytes B")
	}
	assertProviderVersion(t, ctx, store, key, secondUpload, second)
	t.Log("upload B: success; distinct ETag and VersionId present; current and exact-version GET returned B")

	old, err := store.DownloadPrefixVersion(ctx, key, firstUpload.versionID, int64(len(first)+1))
	if err != nil {
		t.Fatal("R2 could not retrieve historical version A after the same key was replaced")
	}
	if !bytes.Equal(old, first) {
		t.Fatal("R2 silently substituted current bytes for the requested historical object version")
	}
	assertProviderHeadMetadata(t, ctx, store, key, firstUpload)
	t.Log("historical retrieval: exact VersionId A remained addressable and returned A after overwrite")
}

type providerUpload struct {
	versionID string
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
	version := strings.TrimSpace(response.Header.Get("x-amz-version-id"))
	if version == "" {
		t.Fatal("R2 upload omitted x-amz-version-id required by the frozen media provenance contract")
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
	return providerUpload{versionID: version, etag: etag, objectURL: parsed.String()}
}

func assertProviderVersion(t *testing.T, ctx context.Context, store *Client, key string, upload providerUpload, expected []byte) {
	t.Helper()
	size, exists, err := store.HeadObjectVersion(ctx, key, upload.versionID)
	if err != nil || !exists || size != int64(len(expected)) {
		t.Fatalf("exact provider object version HEAD failed: exists=%t size=%d", exists, size)
	}
	assertProviderHeadMetadata(t, ctx, store, key, upload)
	got, err := store.DownloadPrefixVersion(ctx, key, upload.versionID, int64(len(expected)+1))
	if err != nil {
		t.Fatal("reading exact provider object version failed")
	}
	if !bytes.Equal(got, expected) {
		t.Fatal("exact provider object version bytes differ")
	}
}

func assertProviderHeadMetadata(t *testing.T, ctx context.Context, store *Client, key string, upload providerUpload) {
	t.Helper()
	out, err := store.s3.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(store.bucket), Key: aws.String(key), VersionId: aws.String(upload.versionID),
	})
	if err != nil {
		t.Fatal("provider exact-version HEAD metadata request failed")
	}
	if strings.TrimSpace(aws.ToString(out.VersionId)) != upload.versionID {
		t.Fatal("provider exact-version HEAD did not return the requested VersionId")
	}
	if strings.TrimSpace(aws.ToString(out.ETag)) != upload.etag {
		t.Fatal("provider exact-version HEAD did not return the upload ETag")
	}
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

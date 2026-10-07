package storage

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// r2LikeTransport answers like Cloudflare R2 did in production on 2026-10-07:
// multipart completion and HEAD carry an x-amz-version-id, and any read that
// names that version id is refused with 501 NotImplemented.
type r2LikeTransport struct {
	versionID string
	eTag      string
	reads     []recordedIdentityRequest
}

func (transport *r2LikeTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	header := http.Header{}
	if transport.versionID != "" {
		header.Set("x-amz-version-id", transport.versionID)
	}
	if transport.eTag != "" {
		header.Set("ETag", transport.eTag)
	}
	respond := func(status int, body string) (*http.Response, error) {
		header.Set("Content-Length", strconv.Itoa(len(body)))
		return &http.Response{StatusCode: status, Status: http.StatusText(status), Header: header,
			Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body)), Request: request}, nil
	}
	if request.Method == http.MethodPost && request.URL.Query().Has("uploadId") {
		return respond(http.StatusOK, `<?xml version="1.0" encoding="UTF-8"?><CompleteMultipartUploadResult><Bucket>private-media</Bucket><Key>quarantine/source</Key><ETag>`+
			strings.ReplaceAll(transport.eTag, `"`, "&quot;")+`</ETag></CompleteMultipartUploadResult>`)
	}
	transport.reads = append(transport.reads, recordedIdentityRequest{
		method: request.Method, versionID: request.URL.Query().Get("versionId"), ifMatch: request.Header.Get("If-Match"),
	})
	if request.URL.Query().Get("versionId") != "" {
		header.Set("Content-Type", "application/xml")
		return respond(http.StatusNotImplemented, `<?xml version="1.0" encoding="UTF-8"?><Error><Code>NotImplemented</Code><Message>versionId not implemented</Message></Error>`)
	}
	return respond(http.StatusOK, "object bytes")
}

func newMultipartIdentityTestClient(transport http.RoundTripper, etagIdentity bool) *Client {
	cfg := aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("access", "secret", ""),
		HTTPClient:  &http.Client{Transport: transport},
	}
	return &Client{
		s3: s3.NewFromConfig(cfg, func(options *s3.Options) {
			options.BaseEndpoint = aws.String("http://storage.test")
			options.UsePathStyle = true
		}),
		bucket:       "private-media",
		etagIdentity: etagIdentity,
	}
}

// The production failure: R2 multipart completion returned a version id, the
// version id was recorded, and the worker's exact HEAD/GET by that version id
// got 501, which surfaced to Instructors as DEPENDENCY_UNAVAILABLE.
func TestR2MultipartCompletionRecordsAReadableETagIdentity(t *testing.T) {
	ctx := context.Background()
	transport := &r2LikeTransport{versionID: "7e5ee7dd341db3622df3a983fc52e68e", eTag: `"a393096d25bfa4d3157dfb83e3ee90a0-1"`}
	client := newMultipartIdentityTestClient(transport, true)

	identity, err := client.CompleteMultipartUpload(ctx, "quarantine/source", "upload-1", []int32{1}, []string{`"part-1"`})
	if err != nil {
		t.Fatalf("CompleteMultipartUpload: %v", err)
	}
	if identity != `etag:"a393096d25bfa4d3157dfb83e3ee90a0-1"` {
		t.Fatalf("R2 multipart identity = %q, want the strong ETag identity", identity)
	}
	reconciled, err := client.MultipartObjectIdentity(ctx, "quarantine/source")
	if err != nil {
		t.Fatalf("MultipartObjectIdentity: %v", err)
	}
	if reconciled != identity {
		t.Fatalf("lost-response reconciliation identity = %q, want %q", reconciled, identity)
	}

	// The worker's verification reads now succeed and stay bound by If-Match.
	if _, _, err := client.HeadObjectVersion(ctx, "quarantine/source", identity); err != nil {
		t.Fatalf("HeadObjectVersion with recorded identity: %v", err)
	}
	if _, err := client.DownloadPrefixVersion(ctx, "quarantine/source", identity, 100); err != nil {
		t.Fatalf("DownloadPrefixVersion with recorded identity: %v", err)
	}
	if _, err := client.HashObjectVersion(ctx, "quarantine/source", identity); err != nil {
		t.Fatalf("HashObjectVersion with recorded identity: %v", err)
	}
	for _, read := range transport.reads[1:] {
		if read.versionID != "" || read.ifMatch != `"a393096d25bfa4d3157dfb83e3ee90a0-1"` {
			t.Fatalf("exact read %+v, want If-Match on the recorded ETag and no versionId", read)
		}
	}

	// Recording the version id, as the deployed build did, reproduces the outage.
	if _, _, err := client.HeadObjectVersion(ctx, "quarantine/source", "7e5ee7dd341db3622df3a983fc52e68e"); err == nil ||
		!strings.Contains(err.Error(), "NotImplemented") {
		t.Fatalf("versioned HEAD against R2 = %v, want 501 NotImplemented", err)
	}
}

func TestVersionedProvidersKeepRecordingVersionIdentity(t *testing.T) {
	ctx := context.Background()
	transport := &r2LikeTransport{versionID: "minio-version-1", eTag: `"minio-etag-1"`}
	client := newMultipartIdentityTestClient(transport, false)
	identity, err := client.CompleteMultipartUpload(ctx, "quarantine/source", "upload-1", []int32{1}, []string{`"part-1"`})
	if err != nil {
		t.Fatalf("CompleteMultipartUpload: %v", err)
	}
	if identity != "minio-version-1" {
		t.Fatalf("versioned provider identity = %q, want its version id", identity)
	}
}

func TestR2MultipartIdentityFailsClosedWithoutStrongETag(t *testing.T) {
	ctx := context.Background()
	for _, eTag := range []string{"", `W/"weak"`, "unquoted"} {
		transport := &r2LikeTransport{versionID: "7e5ee7dd341db3622df3a983fc52e68e", eTag: eTag}
		client := newMultipartIdentityTestClient(transport, true)
		if identity, err := client.CompleteMultipartUpload(ctx, "quarantine/source", "upload-1", []int32{1}, []string{`"part-1"`}); err == nil {
			t.Fatalf("ETag %q: R2 completion recorded %q, want refusal instead of the unreadable version id", eTag, identity)
		}
	}
}

func TestR2EndpointImpliesETagIdentity(t *testing.T) {
	for endpoint, want := range map[string]bool{
		"https://bcd17b26bbd43130c6c82996c53a482b.r2.cloudflarestorage.com": true,
		"https://ACCOUNT.R2.CloudflareStorage.com":                          true,
		"http://minio:9000":                          false,
		"https://s3.amazonaws.com":                   false,
		"https://r2.cloudflarestorage.com.evil.test": false,
	} {
		client, err := New(context.Background(), Options{Endpoint: endpoint, AccessKey: "a", SecretKey: "s", Bucket: "b", Region: "auto"})
		if err != nil {
			t.Fatalf("New(%q): %v", endpoint, err)
		}
		if client.etagIdentity != want {
			t.Fatalf("New(%q).etagIdentity = %v, want %v", endpoint, client.etagIdentity, want)
		}
	}
	client, err := New(context.Background(), Options{Endpoint: "http://minio:9000", AccessKey: "a", SecretKey: "s", Bucket: "b", Region: "auto", ETagObjectIdentity: true})
	if err != nil || !client.etagIdentity {
		t.Fatalf("explicit ETagObjectIdentity not honoured: %v %v", client, err)
	}
}

package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type recordedIdentityRequest struct {
	method    string
	versionID string
	ifMatch   string
}

type identityRecordingTransport struct {
	requests []recordedIdentityRequest
}

func (transport *identityRecordingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.requests = append(transport.requests, recordedIdentityRequest{
		method:    request.Method,
		versionID: request.URL.Query().Get("versionId"),
		ifMatch:   request.Header.Get("If-Match"),
	})
	const responseBody = "object bytes"
	return &http.Response{
		StatusCode:    http.StatusOK,
		Status:        "200 OK",
		Header:        http.Header{"Content-Length": []string{"12"}},
		Body:          io.NopCloser(strings.NewReader(responseBody)),
		ContentLength: int64(len(responseBody)),
		Request:       request,
	}, nil
}

func newIdentityTestClient(transport *identityRecordingTransport) *Client {
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
		bucket: "private-media",
	}
}

func TestExactObjectReadsUseTheProviderIdentityNamespace(t *testing.T) {
	cases := []struct {
		name      string
		identity  string
		versionID string
		ifMatch   string
	}{
		{name: "legacy VersionId", identity: "minio-version-1", versionID: "minio-version-1"},
		{name: "ETag If-Match", identity: `etag:"r2-etag-1"`, ifMatch: `"r2-etag-1"`},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			transport := &identityRecordingTransport{}
			client := newIdentityTestClient(transport)
			ctx := context.Background()

			if _, _, err := client.HeadObjectVersion(ctx, "quarantine/source", testCase.identity); err != nil {
				t.Fatalf("HeadObjectVersion: %v", err)
			}
			if _, err := client.DownloadPrefixVersion(ctx, "quarantine/source", testCase.identity, 100); err != nil {
				t.Fatalf("DownloadPrefixVersion: %v", err)
			}
			if actualHash, err := client.HashObjectVersion(ctx, "quarantine/source", testCase.identity); err != nil {
				t.Fatalf("HashObjectVersion: %v", err)
			} else {
				digest := sha256.Sum256([]byte("object bytes"))
				if actualHash != hex.EncodeToString(digest[:]) {
					t.Fatalf("HashObjectVersion = %q, want hash of exact response bytes", actualHash)
				}
			}
			path, cleanup, err := client.DownloadToFileVersion(ctx, "quarantine/source", testCase.identity)
			if err != nil {
				t.Fatalf("DownloadToFileVersion: %v", err)
			}
			defer cleanup()
			contents, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("reading exact download: %v", err)
			}
			if string(contents) != "object bytes" {
				t.Fatalf("DownloadToFileVersion = %q, want exact response bytes", contents)
			}

			if len(transport.requests) != 4 {
				t.Fatalf("provider request count = %d, want 4", len(transport.requests))
			}
			for _, request := range transport.requests {
				if request.versionID != testCase.versionID || request.ifMatch != testCase.ifMatch {
					t.Fatalf("provider identity request = %+v, want versionID=%q ifMatch=%q", request, testCase.versionID, testCase.ifMatch)
				}
			}
		})
	}
}

func TestMalformedETagIdentityFailsClosedBeforeAnyProviderRead(t *testing.T) {
	for _, encodedIdentity := range []string{
		"etag:",
		"etag:unquoted-etag",
		`etag:""`,
		`etag:"unterminated`,
		`etag:"contains space"`,
		`etag:W/"weak-etag"`,
	} {
		t.Run(encodedIdentity, func(t *testing.T) {
			transport := &identityRecordingTransport{}
			client := newIdentityTestClient(transport)
			ctx := context.Background()
			readMethods := []struct {
				name string
				call func() error
			}{
				{name: "HEAD", call: func() error {
					_, _, err := client.HeadObjectVersion(ctx, "quarantine/source", encodedIdentity)
					return err
				}},
				{name: "prefix GET", call: func() error {
					_, err := client.DownloadPrefixVersion(ctx, "quarantine/source", encodedIdentity, 100)
					return err
				}},
				{name: "hash GET", call: func() error {
					_, err := client.HashObjectVersion(ctx, "quarantine/source", encodedIdentity)
					return err
				}},
				{name: "file GET", call: func() error {
					_, cleanup, err := client.DownloadToFileVersion(ctx, "quarantine/source", encodedIdentity)
					if cleanup != nil {
						cleanup()
					}
					return err
				}},
			}
			for _, readMethod := range readMethods {
				t.Run(readMethod.name, func(t *testing.T) {
					if err := readMethod.call(); !errors.Is(err, errInvalidObjectIdentity) {
						t.Fatalf("error = %v, want errInvalidObjectIdentity", err)
					}
				})
			}
			if len(transport.requests) != 0 {
				t.Fatalf("provider requests = %d, want none for malformed identity", len(transport.requests))
			}
		})
	}
}

package storage

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestThumbnailUploadSignatureBindsContentLength(t *testing.T) {
	client := newAbsoluteExpiryTestClient(t)
	signed, err := client.PresignPutSizedURL(context.Background(), "quarantine/course/asset/source", "image/png", 1234, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(signed)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(parsed.Query().Get("X-Amz-SignedHeaders"), "content-length") {
		t.Fatal("thumbnail signature does not constrain upload size")
	}
}

func TestThumbnailCleanupHandlesVersionedAndUnversionedProviders(t *testing.T) {
	for _, versioned := range []bool{true, false} {
		for _, denied := range []bool{true, false} {
			t.Run(fmt.Sprintf("versioned=%t/denied=%t", versioned, denied), func(t *testing.T) {
				var deletionBodies []string
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/xml")
					if r.Method == "POST" {
						body, _ := io.ReadAll(r.Body)
						deletionBodies = append(deletionBodies, string(body))
						if denied {
							io.WriteString(w, `<DeleteResult><Error><Key>source</Key><Code>AccessDenied</Code></Error></DeleteResult>`)
						} else {
							io.WriteString(w, `<DeleteResult/>`)
						}
						return
					}
					if r.URL.Query().Has("versions") {
						if !versioned {
							w.WriteHeader(501)
							io.WriteString(w, `<Error><Code>NotImplemented</Code></Error>`)
							return
						}
						io.WriteString(w, `<ListVersionsResult><IsTruncated>false</IsTruncated><Version><Key>source</Key><VersionId>v1</VersionId></Version><DeleteMarker><Key>source</Key><VersionId>marker</VersionId></DeleteMarker></ListVersionsResult>`)
					} else {
						io.WriteString(w, `<ListBucketResult><IsTruncated>false</IsTruncated><Contents><Key>source</Key></Contents></ListBucketResult>`)
					}
				}))
				defer server.Close()
				client, err := New(context.Background(), Options{Endpoint: server.URL, AccessKey: "test", SecretKey: "test", Bucket: "private", Region: "us-east-1", UsePathStyle: true})
				if err != nil {
					t.Fatal(err)
				}
				err = client.DeleteThumbnailObjects(context.Background(), "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
				if (err != nil) != denied {
					t.Fatalf("delete result: %v", err)
				}
				if len(deletionBodies) == 0 {
					t.Fatal("no object deletion occurred")
				}
				if versioned && (!strings.Contains(deletionBodies[0], "v1") || !strings.Contains(deletionBodies[0], "marker")) {
					t.Fatal("historical versions or delete markers retained")
				}
			})
		}
	}
}

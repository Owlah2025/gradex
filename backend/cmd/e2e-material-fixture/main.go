//go:build !production

// Command e2e-material-fixture creates only the two deterministic private
// objects referenced by the canonical E2E Course. It is deliberately a test
// fixture producer, not an upload or delivery API: Students still obtain both
// objects only through the live entitlement-checked signing boundary.
package main

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/Owlah2025/gradex/backend/internal/storage"
)

const materialFixtureOptIn = "GRADEX_E2E_SEED_PRIVATE_MATERIALS"

func main() {
	if os.Getenv(materialFixtureOptIn) != "true" {
		panic("refusing to write private E2E material fixtures without " + materialFixtureOptIn + "=true")
	}
	if os.Getenv("APP_ENV") != "development" {
		panic("private E2E material fixtures require APP_ENV=development")
	}
	endpoint, err := url.Parse(os.Getenv("S3_ENDPOINT"))
	if err != nil || endpoint.Hostname() == "" || !isLoopbackEndpoint(endpoint.Hostname()) {
		panic("private E2E material fixtures require a loopback S3_ENDPOINT")
	}
	client, err := storage.New(context.Background(), storage.Options{
		Endpoint:     os.Getenv("S3_ENDPOINT"),
		AccessKey:    os.Getenv("S3_ACCESS_KEY"),
		SecretKey:    os.Getenv("S3_SECRET_KEY"),
		Bucket:       os.Getenv("S3_BUCKET"),
		Region:       "us-east-1",
		UsePathStyle: true,
	})
	if err != nil {
		panic(fmt.Errorf("constructing private E2E object store: %w", err))
	}
	objects := []struct {
		key         string
		contentType string
		body        []byte
	}{
		{"test/notes.pdf", "application/pdf", []byte("%PDF-1.7\n1 0 obj\n<<>>\nendobj\ntrailer\n%%EOF\n")},
		{"test/lab.zip", "application/zip", labArchive()},
	}
	for _, object := range objects {
		if err := client.PutObject(context.Background(), object.key, object.body, object.contentType); err != nil {
			panic(fmt.Errorf("storing private E2E fixture %s: %w", object.key, err))
		}
	}
}

func isLoopbackEndpoint(host string) bool {
	host = strings.Trim(strings.ToLower(host), "[]")
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func labArchive() []byte {
	var output bytes.Buffer
	archive := zip.NewWriter(&output)
	header := &zip.FileHeader{Name: "README.txt", Method: zip.Store}
	header.SetModTime(time.Date(2026, time.August, 21, 0, 0, 0, 0, time.UTC))
	entry, err := archive.CreateHeader(header)
	if err != nil {
		panic(fmt.Errorf("creating Lab fixture entry: %w", err))
	}
	if _, err := entry.Write([]byte("Gradex ST-15 canonical Lab Material fixture.\n")); err != nil {
		panic(fmt.Errorf("writing Lab fixture entry: %w", err))
	}
	if err := archive.Close(); err != nil {
		panic(fmt.Errorf("closing Lab fixture archive: %w", err))
	}
	if output.Len() == 0 {
		panic(errors.New("Lab fixture archive was empty"))
	}
	return output.Bytes()
}

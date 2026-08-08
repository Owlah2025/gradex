// Command storage-fixture writes the fixed protected-playback fixture used by
// the provider staging smoke. It is built only into the proof image.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/Owlah2025/gradex/backend/internal/storage"
)

const maxFixtureBytes = 16 << 20

var allowedFixtures = map[string]string{
	"test/master.m3u8":   "application/vnd.apple.mpegurl",
	"test/segment000.ts": "video/mp2t",
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "storage-fixture: provider fixture write failed")
		os.Exit(1)
	}
}

func run() error {
	key := flag.String("key", "", "fixed provider smoke object key")
	flag.Parse()
	contentType, allowed := allowedFixtures[*key]
	if !allowed || flag.NArg() != 0 {
		return errors.New("unsupported fixture key")
	}

	endpoint, err := requiredEnvironment("S3_ENDPOINT")
	if err != nil {
		return err
	}
	bucket, err := requiredEnvironment("S3_BUCKET")
	if err != nil {
		return err
	}
	accessKey, err := requiredEnvironment("S3_ACCESS_KEY")
	if err != nil {
		return err
	}
	secretKey, err := requiredEnvironment("S3_SECRET_KEY")
	if err != nil {
		return err
	}

	body, err := io.ReadAll(io.LimitReader(os.Stdin, maxFixtureBytes+1))
	if err != nil || len(body) == 0 || len(body) > maxFixtureBytes {
		return errors.New("fixture input is empty, unreadable, or too large")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := storage.New(ctx, storage.Options{
		Endpoint: endpoint, AccessKey: accessKey, SecretKey: secretKey,
		Bucket: bucket, Region: "auto", UsePathStyle: false,
	})
	if err != nil {
		return err
	}
	if err := client.CheckBucket(ctx); err != nil {
		return err
	}
	return client.PutObject(ctx, *key, body, contentType)
}

func requiredEnvironment(name string) (string, error) {
	value := os.Getenv(name)
	if value == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	return value, nil
}

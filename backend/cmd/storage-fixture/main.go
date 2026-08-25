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
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Owlah2025/gradex/backend/internal/storage"
)

const maxFixtureBytes = 16 << 20

var allowedFixtures = map[string]string{
	"test/master.m3u8":   "application/vnd.apple.mpegurl",
	"test/segment000.ts": "video/mp2t",
}

var capacityPrefixPattern = regexp.MustCompile(`^capacity/[A-Za-z0-9][A-Za-z0-9._-]{2,63}/$`)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "storage-fixture: provider fixture write failed")
		os.Exit(1)
	}
}

func run() error {
	key := flag.String("key", "", "fixed provider smoke object key")
	prefix := flag.String("prefix", "", "exact disposable capacity prefix; empty keeps the fixed key")
	flag.Parse()
	contentType, allowed := allowedFixtures[*key]
	if !allowed || flag.NArg() != 0 {
		return errors.New("unsupported fixture key")
	}
	objectKey, err := prefixedFixtureKey(*key, *prefix)
	if err != nil {
		return err
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
	usePathStyle, err := booleanEnvironment("S3_USE_PATH_STYLE", false)
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
		Bucket: bucket, Region: "auto", UsePathStyle: usePathStyle,
	})
	if err != nil {
		return err
	}
	if err := client.CheckBucket(ctx); err != nil {
		return err
	}
	return client.PutObject(ctx, objectKey, body, contentType)
}

func prefixedFixtureKey(key, prefix string) (string, error) {
	if prefix == "" {
		return key, nil
	}
	if !capacityPrefixPattern.MatchString(prefix) {
		return "", errors.New("fixture prefix must be an exact capacity/<run-id>/ prefix")
	}
	if strings.Contains(key, "..") {
		return "", errors.New("fixture key contains an unsafe path")
	}
	return prefix + key, nil
}

func requiredEnvironment(name string) (string, error) {
	value := os.Getenv(name)
	if value == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	return value, nil
}

func booleanEnvironment(name string, fallback bool) (bool, error) {
	value, ok := os.LookupEnv(name)
	if !ok {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback, fmt.Errorf("%s must be a boolean, got %q", name, value)
	}
	return parsed, nil
}

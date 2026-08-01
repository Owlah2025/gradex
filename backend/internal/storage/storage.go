// Package storage wraps an S3-compatible object store (MinIO locally, S3/R2 in
// production) and exposes presigned PUT/GET URL generation. Nothing outside
// this package should construct S3 keys or talk to the SDK directly.
package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type Client struct {
	s3      *s3.Client
	presign *s3.PresignClient
	bucket  string
}

type Options struct {
	Endpoint     string
	AccessKey    string
	SecretKey    string
	Bucket       string
	Region       string
	UsePathStyle bool
}

func New(ctx context.Context, opts Options) (*Client, error) {
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(opts.Region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(opts.AccessKey, opts.SecretKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("loading aws config: %w", err)
	}

	s3Client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		if opts.Endpoint != "" {
			o.BaseEndpoint = aws.String(opts.Endpoint)
		}
		o.UsePathStyle = opts.UsePathStyle
	})

	return &Client{
		s3:      s3Client,
		presign: s3.NewPresignClient(s3Client),
		bucket:  opts.Bucket,
	}, nil
}

// PresignPutURL returns a time-limited URL the caller can PUT a file's bytes
// directly to (browser or instructor tool), bypassing the API server.
func (c *Client) PresignPutURL(ctx context.Context, key, contentType string, expiry time.Duration) (string, error) {
	req, err := c.presign.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(c.bucket),
		Key:         aws.String(key),
		ContentType: aws.String(contentType),
	}, s3.WithPresignExpires(expiry))
	if err != nil {
		return "", fmt.Errorf("presigning PUT for %q: %w", key, err)
	}
	return req.URL, nil
}

// PresignGetURL returns a time-limited URL for reading an object — used for
// signed HLS playback URLs. This is the only way playback bytes are reachable;
// the bucket itself has no public/anonymous read access (see docker-compose.yml).
func (c *Client) PresignGetURL(ctx context.Context, key string, expiry time.Duration) (string, error) {
	req, err := c.presign.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(expiry))
	if err != nil {
		return "", fmt.Errorf("presigning GET for %q: %w", key, err)
	}
	return req.URL, nil
}

// HeadObject reports whether an object exists and its size, used by
// CompleteUpload to verify the instructor's direct-to-storage PUT actually
// landed before enqueuing processing.
func (c *Client) HeadObject(ctx context.Context, key string) (sizeBytes int64, exists bool, err error) {
	return c.HeadObjectVersion(ctx, key, "")
}

// HeadObjectVersion verifies the exact provider object version. A non-empty
// version is never silently replaced by the current object at the same key.
func (c *Client) HeadObjectVersion(ctx context.Context, key, objectVersion string) (sizeBytes int64, exists bool, err error) {
	input := &s3.HeadObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	}
	if objectVersion != "" {
		input.VersionId = aws.String(objectVersion)
	}
	out, err := c.s3.HeadObject(ctx, input)
	if err != nil {
		return 0, false, fmt.Errorf("heading object %q version %q: %w", key, objectVersion, err)
	}
	return aws.ToInt64(out.ContentLength), true, nil
}

// PutObject uploads bytes directly — used by workers writing HLS renditions
// produced locally by ffmpeg back up to storage.
func (c *Client) PutObject(ctx context.Context, key string, body []byte, contentType string) error {
	_, err := c.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(c.bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(body),
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return fmt.Errorf("putting object %q: %w", key, err)
	}
	return nil
}

// DeletePrefix removes derived worker output after a failed or timed-out media
// operation. Callers provide only a media-owned prefix; source quarantine
// objects are never removed through this helper.
func (c *Client) DeletePrefix(ctx context.Context, prefix string) error {
	paginator := s3.NewListObjectsV2Paginator(c.s3, &s3.ListObjectsV2Input{
		Bucket: aws.String(c.bucket),
		Prefix: aws.String(prefix),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("listing objects below %q: %w", prefix, err)
		}
		if len(page.Contents) == 0 {
			continue
		}
		objects := make([]s3types.ObjectIdentifier, 0, len(page.Contents))
		for _, object := range page.Contents {
			objects = append(objects, s3types.ObjectIdentifier{Key: object.Key})
		}
		if _, err := c.s3.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: aws.String(c.bucket),
			Delete: &s3types.Delete{Objects: objects, Quiet: aws.Bool(true)},
		}); err != nil {
			return fmt.Errorf("deleting objects below %q: %w", prefix, err)
		}
	}
	return nil
}

// DownloadObject fetches an object's full bytes — used by ServeManifest for
// small text manifests (m3u8), where in-memory rewriting is required anyway.
// For large binary objects (raw uploads), use DownloadToFile instead.
func (c *Client) DownloadObject(ctx context.Context, key string) ([]byte, error) {
	out, err := c.s3.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("getting object %q: %w", key, err)
	}
	defer out.Body.Close()
	return io.ReadAll(out.Body)
}

// DownloadPrefix reads only the first maxBytes of an object. Media completion
// uses this to inspect content signatures before accepting a direct upload;
// the declared MIME type and filename are not sufficient evidence.
func (c *Client) DownloadPrefix(ctx context.Context, key string, maxBytes int64) ([]byte, error) {
	return c.DownloadPrefixVersion(ctx, key, "", maxBytes)
}

func (c *Client) DownloadPrefixVersion(ctx context.Context, key, objectVersion string, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		return nil, fmt.Errorf("prefix size must be positive")
	}
	input := &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
		Range:  aws.String(fmt.Sprintf("bytes=0-%d", maxBytes-1)),
	}
	if objectVersion != "" {
		input.VersionId = aws.String(objectVersion)
	}
	out, err := c.s3.GetObject(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("getting object prefix %q: %w", key, err)
	}
	defer out.Body.Close()
	return io.ReadAll(io.LimitReader(out.Body, maxBytes))
}

// HashObjectVersion computes evidence over the exact stored version rather
// than accepting a client-provided digest as proof of the bytes received.
func (c *Client) HashObjectVersion(ctx context.Context, key, objectVersion string) (string, error) {
	input := &s3.GetObjectInput{Bucket: aws.String(c.bucket), Key: aws.String(key)}
	if objectVersion != "" {
		input.VersionId = aws.String(objectVersion)
	}
	out, err := c.s3.GetObject(ctx, input)
	if err != nil {
		return "", fmt.Errorf("getting object %q version %q for hashing: %w", key, objectVersion, err)
	}
	defer out.Body.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, out.Body); err != nil {
		return "", fmt.Errorf("hashing object %q version %q: %w", key, objectVersion, err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// DownloadToFile streams an object directly to a temp file instead of
// buffering it in memory — used by the metadata/transcode workers to pull
// multi-GB raw uploads down to local disk before shelling out to ffmpeg.
// Caller must invoke cleanup() once done with the file.
func (c *Client) DownloadToFile(ctx context.Context, key string) (path string, cleanup func(), err error) {
	return c.DownloadToFileVersion(ctx, key, "")
}

// DownloadToFileVersion streams the exact provider object version to a temp
// file. A non-empty version is never replaced by the current object at the
// same key.
func (c *Client) DownloadToFileVersion(ctx context.Context, key, objectVersion string) (path string, cleanup func(), err error) {
	input := &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	}
	if objectVersion != "" {
		input.VersionId = aws.String(objectVersion)
	}
	out, err := c.s3.GetObject(ctx, input)
	if err != nil {
		return "", nil, fmt.Errorf("getting object %q version %q: %w", key, objectVersion, err)
	}
	defer out.Body.Close()

	f, err := os.CreateTemp("", "gradex-download-*")
	if err != nil {
		return "", nil, fmt.Errorf("creating temp file for %q: %w", key, err)
	}
	cleanup = func() { os.Remove(f.Name()) }

	if _, err := io.Copy(f, out.Body); err != nil {
		f.Close()
		cleanup()
		return "", nil, fmt.Errorf("streaming object %q version %q to disk: %w", key, objectVersion, err)
	}
	if err := f.Close(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("closing temp file for %q version %q: %w", key, objectVersion, err)
	}
	return f.Name(), cleanup, nil
}

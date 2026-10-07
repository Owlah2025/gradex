package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

func (c *Client) CreateMultipartUpload(ctx context.Context, key, contentType string) (string, error) {
	out, err := c.s3.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket:      aws.String(c.bucket),
		Key:         aws.String(key),
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return "", fmt.Errorf("creating multipart upload for %q: %w", key, err)
	}
	if aws.ToString(out.UploadId) == "" {
		return "", errors.New("provider returned no multipart identifier")
	}
	return *out.UploadId, nil
}

type MultipartPartUpload struct {
	Key, UploadID string
	PartNumber    int32
	SizeBytes     int64
	Expiry        time.Duration
}

func (c *Client) PresignUploadPartURL(ctx context.Context, part MultipartPartUpload) (string, error) {
	req, err := c.presign.PresignUploadPart(ctx, &s3.UploadPartInput{
		Bucket:        aws.String(c.bucket),
		Key:           aws.String(part.Key),
		UploadId:      aws.String(part.UploadID),
		PartNumber:    aws.Int32(part.PartNumber),
		ContentLength: aws.Int64(part.SizeBytes),
	}, s3.WithPresignExpires(part.Expiry))
	if err != nil {
		return "", fmt.Errorf("presigning multipart upload part %d for %q: %w", part.PartNumber, part.Key, err)
	}
	return req.URL, nil
}

func (c *Client) CompleteMultipartUpload(ctx context.Context, key, uploadID string, partNumbers []int32, eTags []string) (string, error) {
	if len(partNumbers) != len(eTags) {
		return "", fmt.Errorf("mismatched parts and etags")
	}
	s3Parts := make([]types.CompletedPart, len(partNumbers))
	for i := range partNumbers {
		s3Parts[i] = types.CompletedPart{
			PartNumber: aws.Int32(partNumbers[i]),
			ETag:       aws.String(eTags[i]),
		}
	}
	out, err := c.s3.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket:   aws.String(c.bucket),
		Key:      aws.String(key),
		UploadId: aws.String(uploadID),
		MultipartUpload: &types.CompletedMultipartUpload{
			Parts: s3Parts,
		},
	})
	if err != nil {
		return "", fmt.Errorf("completing multipart upload for %q: %w", key, err)
	}
	return c.immutableObjectIdentity(out.VersionId, out.ETag, "multipart object")
}

// immutableObjectIdentity chooses the identity later exact reads will use. A
// provider version id is preferred where versioned reads work; where they do
// not (R2), only a strong ETag is acceptable and its absence fails closed.
func (c *Client) immutableObjectIdentity(versionID, eTag *string, what string) (string, error) {
	version := aws.ToString(versionID)
	if !c.etagIdentity && version != "" && version != "null" {
		return version, nil
	}
	if validStrongETag(aws.ToString(eTag)) {
		return objectIdentityETagPrefix + aws.ToString(eTag), nil
	}
	return "", fmt.Errorf("provider returned no immutable %s identity", what)
}

func (c *Client) AbortMultipartUpload(ctx context.Context, key, uploadID string) error {
	_, err := c.s3.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
		Bucket:   aws.String(c.bucket),
		Key:      aws.String(key),
		UploadId: aws.String(uploadID),
	})
	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) && apiErr.ErrorCode() == "NoSuchUpload" {
			return nil
		}
		return fmt.Errorf("aborting multipart upload for %q: %w", key, err)
	}
	return nil
}

type MultipartPart struct {
	PartNumber int32  `json:"part_number"`
	ETag       string `json:"etag"`
	SizeBytes  int64  `json:"size_bytes"`
}

func (c *Client) ListMultipartParts(ctx context.Context, key, uploadID string) ([]MultipartPart, error) {
	pages := s3.NewListPartsPaginator(c.s3, &s3.ListPartsInput{Bucket: aws.String(c.bucket), Key: aws.String(key), UploadId: aws.String(uploadID)})
	parts := []MultipartPart{}
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, p := range page.Parts {
			parts = append(parts, MultipartPart{aws.ToInt32(p.PartNumber), aws.ToString(p.ETag), aws.ToInt64(p.Size)})
		}
	}
	return parts, nil
}

// MultipartObjectIdentity reconciles provider success whose response was lost.
// Multipart keys are unique per intent and are never issued a single PUT URL.
func (c *Client) MultipartObjectIdentity(ctx context.Context, key string) (string, error) {
	out, err := c.s3.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(c.bucket), Key: aws.String(key)})
	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) && (apiErr.ErrorCode() == "NotFound" || apiErr.ErrorCode() == "NoSuchKey") {
			return "", nil
		}
		return "", err
	}
	return c.immutableObjectIdentity(out.VersionId, out.ETag, "object")
}

func (c *Client) DeleteMultipartObject(ctx context.Context, key, version string) error {
	input := &s3.DeleteObjectInput{Bucket: aws.String(c.bucket), Key: aws.String(key)}
	identity, err := parseObjectIdentity(version)
	if err != nil {
		return err
	}
	if identity.kind == objectIdentityVersion {
		input.VersionId = aws.String(identity.value)
	}
	_, err = c.s3.DeleteObject(ctx, input)
	return err
}

// AbortMultipartKey also covers a crash after provider creation but before ID binding.
func (c *Client) AbortMultipartKey(ctx context.Context, key string) error {
	pages := s3.NewListMultipartUploadsPaginator(c.s3, &s3.ListMultipartUploadsInput{Bucket: aws.String(c.bucket), Prefix: aws.String(key)})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return err
		}
		for _, upload := range page.Uploads {
			if aws.ToString(upload.Key) == key {
				if err := c.AbortMultipartUpload(ctx, key, aws.ToString(upload.UploadId)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (c *Client) FindMultipartUpload(ctx context.Context, key string) (string, error) {
	pages := s3.NewListMultipartUploadsPaginator(c.s3, &s3.ListMultipartUploadsInput{Bucket: aws.String(c.bucket), Prefix: aws.String(key)})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return "", err
		}
		for _, upload := range page.Uploads {
			if aws.ToString(upload.Key) == key {
				return aws.ToString(upload.UploadId), nil
			}
		}
	}
	return "", nil
}

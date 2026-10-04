package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
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
	return *out.UploadId, nil
}

func (c *Client) PresignUploadPartURL(ctx context.Context, key, uploadID string, partNumber int32, expiry time.Duration) (string, error) {
	req, err := c.presign.PresignUploadPart(ctx, &s3.UploadPartInput{
		Bucket:     aws.String(c.bucket),
		Key:        aws.String(key),
		UploadId:   aws.String(uploadID),
		PartNumber: aws.Int32(partNumber),
	}, s3.WithPresignExpires(expiry))
	if err != nil {
		return "", fmt.Errorf("presigning multipart upload part %d for %q: %w", partNumber, key, err)
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
	if out.VersionId != nil {
		return *out.VersionId, nil
	}
	return "", nil
}

func (c *Client) AbortMultipartUpload(ctx context.Context, key, uploadID string) error {
	_, err := c.s3.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
		Bucket:   aws.String(c.bucket),
		Key:      aws.String(key),
		UploadId: aws.String(uploadID),
	})
	if err != nil {
		return fmt.Errorf("aborting multipart upload for %q: %w", key, err)
	}
	return nil
}

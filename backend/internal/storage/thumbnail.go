package storage

import (
	"context"
	"errors"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"github.com/google/uuid"
	"time"
)

// PresignPutSizedURL binds a thumbnail upload to its declared byte length.
// The browser supplies Content-Length from the File body, never credentials.
func (c *Client) PresignPutSizedURL(ctx context.Context, key, contentType string, size int64, expiry time.Duration) (string, error) {
	signed, err := c.presign.PresignPutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(c.bucket), Key: aws.String(key), ContentType: aws.String(contentType), ContentLength: aws.Int64(size)}, s3.WithPresignExpires(expiry))
	if err != nil {
		return "", fmt.Errorf("presigning bounded thumbnail upload: %w", err)
	}
	return signed.URL, nil
}

// DeleteThumbnailObjects deletes historical object versions as well as current
// objects; plain DeleteObject would only create markers in a versioned bucket.
func (c *Client) DeleteThumbnailObjects(ctx context.Context, courseID, versionID string) error {
	for _, id := range []string{courseID, versionID} {
		if _, err := uuid.Parse(id); err != nil {
			return fmt.Errorf("invalid thumbnail identity")
		}
	}
	for _, prefix := range []string{"quarantine/" + courseID + "/" + versionID + "/", "media/" + versionID + "/thumbnail/"} {
		if err := c.deleteThumbnailPrefix(ctx, prefix); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) deleteThumbnailPrefix(ctx context.Context, prefix string) error {
	pages := s3.NewListObjectVersionsPaginator(c.s3, &s3.ListObjectVersionsInput{Bucket: aws.String(c.bucket), Prefix: aws.String(prefix)})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			var apiErr smithy.APIError
			if errors.As(err, &apiErr) && (apiErr.ErrorCode() == "NotImplemented" || apiErr.ErrorCode() == "NotSupported") {
				return c.deleteCurrentThumbnailObjects(ctx, prefix)
			}
			return err
		}
		objects := make([]s3types.ObjectIdentifier, 0, len(page.Versions)+len(page.DeleteMarkers))
		for _, v := range page.Versions {
			objects = append(objects, s3types.ObjectIdentifier{Key: v.Key, VersionId: v.VersionId})
		}
		for _, v := range page.DeleteMarkers {
			objects = append(objects, s3types.ObjectIdentifier{Key: v.Key, VersionId: v.VersionId})
		}
		if len(objects) == 0 {
			continue
		}
		result, err := c.s3.DeleteObjects(ctx, &s3.DeleteObjectsInput{Bucket: aws.String(c.bucket), Delete: &s3types.Delete{Objects: objects, Quiet: aws.Bool(true)}})
		if err != nil {
			return err
		}
		if len(result.Errors) > 0 {
			return fmt.Errorf("thumbnail object deletion refused: %s", aws.ToString(result.Errors[0].Code))
		}
	}
	return nil
}

func (c *Client) deleteCurrentThumbnailObjects(ctx context.Context, prefix string) error {
	pages := s3.NewListObjectsV2Paginator(c.s3, &s3.ListObjectsV2Input{Bucket: aws.String(c.bucket), Prefix: aws.String(prefix)})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return err
		}
		objects := make([]s3types.ObjectIdentifier, 0, len(page.Contents))
		for _, object := range page.Contents {
			objects = append(objects, s3types.ObjectIdentifier{Key: object.Key})
		}
		if len(objects) == 0 {
			continue
		}
		result, err := c.s3.DeleteObjects(ctx, &s3.DeleteObjectsInput{Bucket: aws.String(c.bucket), Delete: &s3types.Delete{Objects: objects, Quiet: aws.Bool(true)}})
		if err != nil {
			return err
		}
		if len(result.Errors) > 0 {
			return fmt.Errorf("thumbnail object deletion refused: %s", aws.ToString(result.Errors[0].Code))
		}
	}
	return nil
}

package storage

import (
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

const objectIdentityETagPrefix = "etag:"

var errInvalidObjectIdentity = errors.New("invalid storage object identity")

type objectIdentityKind string

const (
	objectIdentityCurrent objectIdentityKind = "current"
	objectIdentityVersion objectIdentityKind = "version-id"
	objectIdentityETag    objectIdentityKind = "etag"
)

// objectIdentity is the provider-neutral identity of one stored object. Empty
// input intentionally selects the current object for APIs that allow it.
type objectIdentity struct {
	kind  objectIdentityKind
	value string
}

func parseObjectIdentity(encoded string) (objectIdentity, error) {
	if encoded == "" {
		return objectIdentity{kind: objectIdentityCurrent}, nil
	}
	if !strings.HasPrefix(encoded, objectIdentityETagPrefix) {
		return objectIdentity{kind: objectIdentityVersion, value: encoded}, nil
	}

	etagValue := strings.TrimPrefix(encoded, objectIdentityETagPrefix)
	if !validStrongETag(etagValue) {
		return objectIdentity{}, fmt.Errorf("%w: ETag must be a non-empty strong quoted entity-tag", errInvalidObjectIdentity)
	}
	return objectIdentity{kind: objectIdentityETag, value: etagValue}, nil
}

func validStrongETag(etagValue string) bool {
	if etagValue == "" || etagValue != strings.TrimSpace(etagValue) ||
		len(etagValue) < 3 || etagValue[0] != '"' || etagValue[len(etagValue)-1] != '"' {
		return false
	}
	for _, character := range []byte(etagValue[1 : len(etagValue)-1]) {
		if character <= 0x20 || character == 0x7f || character == '"' {
			return false
		}
	}
	return true
}

func (identity objectIdentity) applyHead(input *s3.HeadObjectInput) {
	switch identity.kind {
	case objectIdentityVersion:
		input.VersionId = aws.String(identity.value)
	case objectIdentityETag:
		input.IfMatch = aws.String(identity.value)
	}
}

func (identity objectIdentity) applyGet(input *s3.GetObjectInput) {
	switch identity.kind {
	case objectIdentityVersion:
		input.VersionId = aws.String(identity.value)
	case objectIdentityETag:
		input.IfMatch = aws.String(identity.value)
	}
}

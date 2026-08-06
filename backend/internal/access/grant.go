package access

import (
	"errors"
	"fmt"
	"time"
)

var (
	ErrCourseNotFound = errors.New("course not found")
	ErrReasonRequired = errors.New("reason is required")
	ErrExpiryRequired = errors.New("default access expiry is required")
	ErrExpiryInPast   = errors.New("default access expiry must be in the future")
)

var KuwaitLocation = time.FixedZone("Asia/Kuwait", 3*3600)

var (
	ErrInvalidDateFormat = errors.New("date must be in YYYY-MM-DD format")
)

// ConvertKuwaitDateToUTCExpiry converts a Kuwait-local calendar date string (YYYY-MM-DD)
// to an exclusive UTC expiry instant representing the first instant of the following local day in UTC.
func ConvertKuwaitDateToUTCExpiry(dateStr string) (time.Time, error) {
	t, err := time.ParseInLocation("2006-01-02", dateStr, KuwaitLocation)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: %v", ErrInvalidDateFormat, err)
	}
	followingDay := t.AddDate(0, 0, 1)
	return followingDay.UTC(), nil
}

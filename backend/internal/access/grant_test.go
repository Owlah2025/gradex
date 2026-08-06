package access

import (
	"errors"
	"testing"
	"time"
)

func TestConvertKuwaitDateToUTCExpiry(t *testing.T) {
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	// For "2026-08-05", exclusive boundary is 2026-08-06 00:00:00 Kuwait = 2026-08-05 21:00:00 UTC.
	boundaryNow := time.Date(2026, 8, 5, 21, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		input   string
		nowTime time.Time
		wantUTC string
		wantErr error
	}{
		{
			name:    "Future date (DST-free offset boundary)",
			input:   "2026-12-31",
			nowTime: now,
			wantUTC: "2026-12-31T21:00:00Z",
		},
		{
			name:    "Month rollover",
			input:   "2026-08-31",
			nowTime: now,
			wantUTC: "2026-08-31T21:00:00Z",
		},
		{
			name:    "Year rollover",
			input:   "2026-12-31",
			nowTime: now,
			wantUTC: "2026-12-31T21:00:00Z",
		},
		{
			name:    "Leap year date",
			input:   "2028-02-29",
			nowTime: now,
			wantUTC: "2028-02-29T21:00:00Z",
		},
		{
			name:    "Past date",
			input:   "2020-01-01",
			nowTime: now,
			wantErr: ErrExpiryInPast,
		},
		{
			name:    "Boundary equal to current time",
			input:   "2026-08-05",
			nowTime: boundaryNow,
			wantErr: ErrExpiryInPast,
		},
		{
			name:    "Empty input",
			input:   "",
			nowTime: now,
			wantErr: ErrExpiryRequired,
		},
		{
			name:    "Whitespace-padded input",
			input:   " 2026-12-31 ",
			nowTime: now,
			wantErr: ErrInvalidDateFormat,
		},
		{
			name:    "Invalid format",
			input:   "2026/12/31",
			nowTime: now,
			wantErr: ErrInvalidDateFormat,
		},
		{
			name:    "Invalid month",
			input:   "2026-13-01",
			nowTime: now,
			wantErr: ErrInvalidDateFormat,
		},
		{
			name:    "Garbage input",
			input:   "invalid",
			nowTime: now,
			wantErr: ErrInvalidDateFormat,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ConvertKuwaitDateToUTCExpiry(tt.input, tt.nowTime)
			if tt.wantErr != nil {
				if err == nil || !errors.Is(err, tt.wantErr) {
					t.Fatalf("ConvertKuwaitDateToUTCExpiry(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ConvertKuwaitDateToUTCExpiry(%q) unexpected error = %v", tt.input, err)
			}
			gotStr := got.Format(time.RFC3339)
			if gotStr != tt.wantUTC {
				t.Errorf("ConvertKuwaitDateToUTCExpiry(%q) = %q, want %q", tt.input, gotStr, tt.wantUTC)
			}
		})
	}
}

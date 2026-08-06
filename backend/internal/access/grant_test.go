package access

import (
	"testing"
	"time"
)

func TestConvertKuwaitDateToUTCExpiry(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantUTC string
		wantErr bool
	}{
		{
			name:    "DST-free offset boundary (mid-year)",
			input:   "2026-06-15",
			wantUTC: "2026-06-15T21:00:00Z",
			wantErr: false,
		},
		{
			name:    "Month rollover",
			input:   "2026-01-31",
			wantUTC: "2026-01-31T21:00:00Z",
			wantErr: false,
		},
		{
			name:    "Year rollover",
			input:   "2026-12-31",
			wantUTC: "2026-12-31T21:00:00Z",
			wantErr: false,
		},
		{
			name:    "Leap year date",
			input:   "2028-02-29",
			wantUTC: "2028-02-29T21:00:00Z",
			wantErr: false,
		},
		{
			name:    "Invalid format",
			input:   "2026/12/31",
			wantErr: true,
		},
		{
			name:    "Invalid month",
			input:   "2026-13-01",
			wantErr: true,
		},
		{
			name:    "Garbage input",
			input:   "invalid",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ConvertKuwaitDateToUTCExpiry(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ConvertKuwaitDateToUTCExpiry(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if !tt.wantErr {
				gotStr := got.Format(time.RFC3339)
				if gotStr != tt.wantUTC {
					t.Errorf("ConvertKuwaitDateToUTCExpiry(%q) = %q, want %q", tt.input, gotStr, tt.wantUTC)
				}
			}
		})
	}
}

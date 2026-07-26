package identity

import (
	"errors"
	"strings"
	"testing"
)

// BR-105 boundary cases: empty / one / two / fifty / fifty-one code points,
// Arabic and Latin scripts, combining marks, URLs, markup, controls, and
// unsupported scripts.
func TestValidateDisplayNameEnforcesStudentProfileContract(t *testing.T) {
	tests := map[string]struct {
		name    string
		want    string
		wantErr bool
	}{
		"Arabic":             {name: "نورة أحمد", want: "نورة أحمد"},
		"Latin punctuation":  {name: "  Anne-Marie O’Neil  ", want: "Anne-Marie O’Neil"},
		"combining mark":     {name: "Jose\u0301 Silva", want: "Jose\u0301 Silva"},
		"two code points":    {name: "Li", want: "Li"},
		"fifty code points":  {name: strings.Repeat("A", 50), want: strings.Repeat("A", 50)},
		"empty":              {name: "", wantErr: true},
		"one code point":     {name: "A", wantErr: true},
		"fifty-one":          {name: strings.Repeat("A", 51), wantErr: true},
		"URL":                {name: "https://example.com", wantErr: true},
		"markup":             {name: "<b>Ahmed</b>", wantErr: true},
		"control":            {name: "Ahmed\u0000Ali", wantErr: true},
		"digits":             {name: "Ahmed 2", wantErr: true},
		"unsupported script": {name: "李明", wantErr: true},
	}
	for scenario, test := range tests {
		t.Run(scenario, func(t *testing.T) {
			got, err := ValidateDisplayName(test.name)
			if test.wantErr {
				if !errors.Is(err, ErrInvalidDisplayName) {
					t.Fatalf("error = %v, want ErrInvalidDisplayName", err)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("ValidateDisplayName() = %q, %v; want %q", got, err, test.want)
			}
		})
	}
}

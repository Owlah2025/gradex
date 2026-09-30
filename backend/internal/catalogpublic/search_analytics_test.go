package catalogpublic

import "testing"

func TestSensitiveSearchShapesAreExcludedFromAnalytics(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  bool
	}{
		{name: "email address", query: "alice@example.com", want: true},
		{name: "international phone", query: "+965 5555 1234", want: true},
		{name: "long digit string", query: "1234567890", want: true},
		{name: "catalogue phrase", query: "biology 101", want: false},
		{name: "short number", query: "year 2", want: false},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if got := looksLikeSensitiveSearchQuery(testCase.query); got != testCase.want {
				t.Fatalf("looksLikeSensitiveSearchQuery(%q) = %t, want %t", testCase.query, got, testCase.want)
			}
		})
	}
}

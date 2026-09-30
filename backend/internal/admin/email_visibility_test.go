package admin

import "testing"

func TestMaskEmailPreservesDomainAndOnlyRevealsFirstLocalRune(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "ordinary mailbox", value: "alice@example.com", want: "a***@example.com"},
		{name: "single rune local", value: "a@example.com", want: "a***@example.com"},
		{name: "invalid mailbox", value: "not-an-email", want: "***"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if got := maskEmail(testCase.value); got != testCase.want {
				t.Fatalf("maskEmail(%q) = %q, want %q", testCase.value, got, testCase.want)
			}
		})
	}
}

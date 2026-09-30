package catalogpublic

import "testing"

func TestNormalizeSearchQueryBoundsAndCanonicalizesWhitespace(t *testing.T) {
	if got := NormalizeSearchQuery("  Biology\t  101 "); got != "biology 101" {
		t.Fatalf("normalized query = %q, want %q", got, "biology 101")
	}
	long := make([]rune, maxSearchEventQueryRunes+10)
	for index := range long {
		long[index] = 'x'
	}
	if got := NormalizeSearchQuery(string(long)); len([]rune(got)) != maxSearchEventQueryRunes {
		t.Fatalf("normalized query rune length = %d, want %d", len([]rune(got)), maxSearchEventQueryRunes)
	}
}

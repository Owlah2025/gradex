package media

import "testing"

func TestMissingRenditionNamesPreservesCompiledLadderOrder(t *testing.T) {
	cases := []struct {
		name     string
		expected []string
		existing map[string]struct{}
		want     []string
	}{
		{name: "first rung only", expected: []string{"1080p", "720p", "480p", "240p"}, existing: map[string]struct{}{"1080p": {}}, want: []string{"720p", "480p", "240p"}},
		{name: "middle rungs", expected: []string{"720p", "480p", "240p"}, existing: map[string]struct{}{"720p": {}, "240p": {}}, want: []string{"480p"}},
		{name: "complete finalization", expected: []string{"1080p", "720p", "480p", "240p"}, existing: map[string]struct{}{"1080p": {}, "720p": {}, "480p": {}, "240p": {}}, want: []string{}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := missingRenditionNames(testCase.expected, testCase.existing)
			if len(got) != len(testCase.want) {
				t.Fatalf("missing = %v, want %v", got, testCase.want)
			}
			for index := range got {
				if got[index] != testCase.want[index] {
					t.Fatalf("missing = %v, want %v", got, testCase.want)
				}
			}
		})
	}
}

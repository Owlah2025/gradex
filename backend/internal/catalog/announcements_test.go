package catalog

import "testing"

func TestNormalizeAnnouncementPageClampsExternalPageValues(t *testing.T) {
	for _, tc := range []struct {
		name string
		page int
		want int
	}{
		{name: "zero uses first page", page: 0, want: 1},
		{name: "negative uses first page", page: -4, want: 1},
		{name: "valid page is preserved", page: 12, want: 12},
		{name: "large page is bounded", page: maxAnnouncementPage + 1, want: maxAnnouncementPage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeAnnouncementPage(tc.page); got != tc.want {
				t.Fatalf("normalizeAnnouncementPage(%d) = %d, want %d", tc.page, got, tc.want)
			}
		})
	}
}

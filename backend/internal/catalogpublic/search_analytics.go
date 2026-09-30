package catalogpublic

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

const maxSearchEventQueryRunes = 120

// NormalizeSearchQuery keeps analytics useful without retaining the visitor's
// original whitespace or unbounded input.
func NormalizeSearchQuery(query string) string {
	normalized := strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(query)), " "))
	if utf8.RuneCountInString(normalized) <= maxSearchEventQueryRunes {
		return normalized
	}
	runes := []rune(normalized)
	return string(runes[:maxSearchEventQueryRunes])
}

func (r *Repository) RecordSearchEvent(ctx context.Context, query string, resultCount int, locale string) error {
	if r == nil || r.pool == nil {
		return errors.New("public catalogue database is required")
	}
	normalized := NormalizeSearchQuery(query)
	if normalized == "" || resultCount < 0 || (locale != "ar" && locale != "en") {
		return errors.New("catalogue search event is invalid")
	}
	if _, err := r.pool.Exec(ctx, `
		INSERT INTO catalog_search_events (normalized_query, result_count, locale)
		VALUES ($1, $2, $3)`, normalized, resultCount, locale); err != nil {
		return fmt.Errorf("recording catalogue search event: %w", err)
	}
	return nil
}

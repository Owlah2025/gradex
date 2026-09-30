package catalogpublic

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"
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
	if looksLikeSensitiveSearchQuery(normalized) {
		return nil
	}
	if _, err := r.pool.Exec(ctx, `
		INSERT INTO catalog_search_events (normalized_query, result_count, locale)
		VALUES ($1, $2, $3)`, normalized, resultCount, locale); err != nil {
		return fmt.Errorf("recording catalogue search event: %w", err)
	}
	return nil
}

func looksLikeSensitiveSearchQuery(query string) bool {
	if looksLikeEmailAddress(query) {
		return true
	}
	digits, other := 0, 0
	for _, char := range query {
		switch {
		case unicode.IsDigit(char):
			digits++
		case unicode.IsSpace(char) || strings.ContainsRune("+-.()", char):
		default:
			other++
		}
	}
	return digits >= 7 && other == 0
}

func looksLikeEmailAddress(query string) bool {
	if strings.ContainsAny(query, " \t\n\r") {
		return false
	}
	at := strings.LastIndexByte(query, '@')
	if at <= 0 || at == len(query)-1 {
		return false
	}
	domain := query[at+1:]
	dot := strings.LastIndexByte(domain, '.')
	return dot > 0 && dot < len(domain)-1
}

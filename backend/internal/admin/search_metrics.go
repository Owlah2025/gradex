package admin

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Owlah2025/gradex/backend/internal/identity"
)

const searchAnalyticsRetentionDays = 180

func (r *Repository) GetSearchMetrics(
	ctx context.Context,
	request SearchMetricsRequest,
) (SearchMetricsResult, error) {
	if err := validateSearchMetricsRequest(request); err != nil {
		return SearchMetricsResult{}, err
	}
	if r == nil || r.pool == nil {
		return SearchMetricsResult{}, ErrRepositoryNil
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return SearchMetricsResult{}, fmt.Errorf("beginning search metrics read: %w", err)
	}
	defer tx.Rollback(ctx)
	var asOf, since time.Time
	if err := tx.QueryRow(ctx, "SELECT now(), now() - interval '30 days'").Scan(&asOf, &since); err != nil {
		return SearchMetricsResult{}, fmt.Errorf("reading search metrics window: %w", err)
	}
	top, err := querySearchMetrics(ctx, tx, since, false)
	if err != nil {
		return SearchMetricsResult{}, err
	}
	zero, err := querySearchMetrics(ctx, tx, since, true)
	if err != nil {
		return SearchMetricsResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return SearchMetricsResult{}, fmt.Errorf("committing search metrics read: %w", err)
	}
	return SearchMetricsResult{
		TopQueries: top, ZeroResultQueries: zero, Since: since,
		RetentionDays: searchAnalyticsRetentionDays,
	}, nil
}

func validateSearchMetricsRequest(request SearchMetricsRequest) error {
	return validateIdentityRead(request.Principal)
}

func querySearchMetrics(
	ctx context.Context,
	tx pgx.Tx,
	since time.Time,
	zeroOnly bool,
) ([]SearchQueryMetric, error) {
	filter := ""
	order := "COUNT(*) DESC, MAX(occurred_at) DESC, normalized_query ASC"
	if zeroOnly {
		filter = " AND result_count = 0"
		order = "COUNT(*) DESC, MAX(occurred_at) DESC, normalized_query ASC"
	}
	rows, err := tx.Query(ctx, `
		SELECT normalized_query, locale, COUNT(*)::int,
		       COUNT(*) FILTER (WHERE result_count = 0)::int,
		       MAX(occurred_at)
		  FROM catalog_search_events
		 WHERE occurred_at >= $1`+filter+`
		 GROUP BY normalized_query, locale
		 HAVING COUNT(*) >= 3
		 ORDER BY `+order+`
		 LIMIT 10`, since)
	if err != nil {
		return nil, fmt.Errorf("querying catalogue search metrics: %w", err)
	}
	defer rows.Close()
	items := make([]SearchQueryMetric, 0, 10)
	for rows.Next() {
		var item SearchQueryMetric
		var locale string
		if err := rows.Scan(&item.Query, &locale, &item.SearchCount, &item.ZeroResultCount, &item.LastSearchedAt); err != nil {
			return nil, fmt.Errorf("scanning catalogue search metric: %w", err)
		}
		item.Locale = identity.Locale(locale)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating catalogue search metrics: %w", err)
	}
	return items, nil
}

var _ interface {
	GetSearchMetrics(context.Context, SearchMetricsRequest) (SearchMetricsResult, error)
} = (*Repository)(nil)

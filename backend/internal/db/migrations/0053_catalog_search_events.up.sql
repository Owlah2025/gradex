-- T7 catalogue search analytics. This is intentionally anonymous: no account,
-- IP, user-agent, or request identity is stored with a search.
CREATE TABLE catalog_search_events (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    normalized_query TEXT NOT NULL,
    result_count     INTEGER NOT NULL,
    locale           TEXT NOT NULL,
    occurred_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT catalog_search_events_query_present CHECK (
        length(trim(normalized_query)) BETWEEN 1 AND 120
    ),
    CONSTRAINT catalog_search_events_result_count_non_negative CHECK (result_count >= 0),
    CONSTRAINT catalog_search_events_locale CHECK (locale IN ('ar', 'en'))
);

CREATE INDEX catalog_search_events_occurred_idx
    ON catalog_search_events (occurred_at DESC);

CREATE INDEX catalog_search_events_query_locale_idx
    ON catalog_search_events (normalized_query, locale, occurred_at DESC);

COMMENT ON TABLE catalog_search_events IS
    'Anonymous catalogue search analytics retained for 180 days by operational cleanup; contains normalized query, result count, locale, and occurrence time only.';

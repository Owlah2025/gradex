package ratelimit

import "time"

// AdminMutationPolicy is the bounded local/Redis budget for high-impact
// operator mutations. The Account identifier keeps one operator from buying a
// fresh budget by varying the target Account or browser headers.
func AdminMutationPolicy(endpoint string) Policy {
	return Policy{
		ID:       endpoint + "-v1",
		Category: "ADMIN_OPERATIONS",
		Endpoint: endpoint,
		Window:   time.Minute,
		Rules: []Rule{
			{Dimension: DimensionEndpoint, Limit: 60, LocalLimit: 6},
			{Dimension: DimensionIdentifier, Limit: 20, LocalLimit: 3},
			{Dimension: DimensionSourceAddr, Limit: 30, LocalLimit: 4},
			{Dimension: DimensionGlobal, Limit: 300, LocalLimit: 20},
		},
		LocalMaxKeys: 4096,
		FailClosed:   true,
	}
}

// AdminExportPolicy gives the PII-bearing directory export its own budget. The
// identifier rule is intentionally hourly so changing a browser or source
// address cannot create another export budget for the same operator.
func AdminExportPolicy(endpoint string) Policy {
	return Policy{
		ID:       endpoint + "-v1",
		Category: "ADMIN_OPERATIONS",
		Endpoint: endpoint,
		Window:   time.Hour,
		Rules: []Rule{
			{Dimension: DimensionEndpoint, Limit: 20, LocalLimit: 10},
			{Dimension: DimensionIdentifier, Limit: 5, LocalLimit: 5},
			{Dimension: DimensionSourceAddr, Limit: 10, LocalLimit: 10},
			{Dimension: DimensionGlobal, Limit: 100, LocalLimit: 100},
		},
		LocalMaxKeys: 4096,
		FailClosed:   true,
	}
}

-- Reverses 0038. The table holds Student-supplied demand, so dropping it
-- discards real product-prioritisation input; that is acceptable only because
-- nothing else references it and no access decision depends on it.
DROP TABLE IF EXISTS subject_demand_signals;

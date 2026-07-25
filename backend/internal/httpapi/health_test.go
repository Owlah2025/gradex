package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/health"
	"github.com/Owlah2025/gradex/backend/internal/logging"
)

// connectionDetails stands for everything a dependency error carries that a
// probe response must not: DSNs, hosts, ports, database names, SQL text,
// migration paths, and credentials.
const connectionDetails = `failed to connect to host=db.internal port=5432 database=gradex ` +
	`user=gradex password=hunter2: pq: relation "schema_migrations" does not exist ` +
	`(file:///app/internal/db/migrations/0001_init.up.sql)`

func failing(err error) health.ProbeFunc {
	return func(context.Context) error { return err }
}

func ok() health.ProbeFunc {
	return func(context.Context) error { return nil }
}

// probeRouter mounts only the probe endpoints, with the real middleware chain.
func probeRouter(t *testing.T, reporter *health.Reporter) (*gin.Engine, *syncBuffer) {
	t.Helper()

	cfg, err := config.LoadFrom(config.MapLookup(map[string]string{
		"APP_ENV": "development", "REDIS_ADDR": "localhost:6379",
		"S3_ENDPOINT": "http://localhost:9000", "S3_BUCKET": "gradex-test",
	}), config.MapSecretResolver{
		"DATABASE_URL": "postgres://x", "S3_ACCESS_KEY": "a",
		"S3_SECRET_KEY": "b", "PLAYBACK_TOKEN_SECRET": "c",
	})
	if err != nil {
		t.Fatalf("config: %v", err)
	}

	buf := &syncBuffer{}
	logger := logging.New(buf, "gradex-api-test", "development", logging.LevelFromString("debug"))

	r, err := newEngine(cfg, logger)
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	r.GET(livenessPath, livenessHandler(reporter))
	r.GET(readinessPath, readinessHandler(reporter, logger))
	return r, buf
}

func probe(t *testing.T, r *gin.Engine, path string) (*httptest.ResponseRecorder, healthResponse) {
	t.Helper()
	rec := do(r, httptest.NewRequest(http.MethodGet, path, nil))
	var body healthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("probe body is not JSON: %q", rec.Body.String())
	}
	return rec, body
}

func started(timeout time.Duration, checks ...health.Check) *health.Reporter {
	r := health.New(timeout, checks...)
	r.MarkStarted()
	return r
}

// Liveness must never consult a dependency: a database outage should not make
// the orchestrator kill a process that is otherwise fine.
func TestLivenessIgnoresDependencyFailure(t *testing.T) {
	reporter := started(time.Second,
		health.Check{Name: "postgres", Required: true, Probe: failing(errors.New(connectionDetails))},
		health.Check{Name: "redis", Required: true, Probe: failing(errors.New(connectionDetails))},
	)
	r, _ := probeRouter(t, reporter)

	rec, body := probe(t, r, livenessPath)
	if rec.Code != http.StatusOK {
		t.Fatalf("liveness status = %d, want 200 while dependencies are down", rec.Code)
	}
	if body.Status != "ok" {
		t.Errorf("liveness status = %q, want ok", body.Status)
	}
	if len(body.Checks) != 0 {
		t.Errorf("liveness must not report dependency checks, got %v", body.Checks)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
}

func TestReadinessFailsWhenRequiredPostgresIsDown(t *testing.T) {
	reporter := started(time.Second,
		health.Check{Name: "postgres", Required: true, Probe: failing(errors.New(connectionDetails))},
		health.Check{Name: "schema", Required: true, Probe: ok()},
		health.Check{Name: "redis", Required: true, Probe: ok()},
	)
	r, _ := probeRouter(t, reporter)

	rec, body := probe(t, r, readinessPath)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("readiness status = %d, want 503", rec.Code)
	}
	if body.Status != "not_ready" {
		t.Errorf("status = %q, want not_ready", body.Status)
	}
	if body.Checks["postgres"] != health.StatusFailed {
		t.Errorf("postgres = %q, want failed", body.Checks["postgres"])
	}
	for _, name := range []string{"schema", "redis"} {
		if body.Checks[name] != health.StatusOK {
			t.Errorf("%s = %q, want ok", name, body.Checks[name])
		}
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
}

func TestSchemaFailureFailsReadiness(t *testing.T) {
	// Dirty schema and unsupported version are separate causes with the same
	// safe outcome.
	for _, cause := range []error{
		errors.New("schema is dirty from a failed migration: version 1"),
		errors.New("schema version is not supported by this build: found 7, this build supports 1..1"),
	} {
		reporter := started(time.Second,
			health.Check{Name: "postgres", Required: true, Probe: ok()},
			health.Check{Name: "schema", Required: true, Probe: failing(cause)},
		)
		r, _ := probeRouter(t, reporter)

		rec, body := probe(t, r, readinessPath)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("readiness status = %d, want 503 for %v", rec.Code, cause)
		}
		if body.Checks["schema"] != health.StatusFailed {
			t.Errorf("schema = %q, want failed", body.Checks["schema"])
		}
	}
}

// Redis is required by role. A role that does not need it must not be held
// unready by its absence.
func TestRedisAffectsReadinessOnlyWhenRequired(t *testing.T) {
	t.Run("required", func(t *testing.T) {
		reporter := started(time.Second,
			health.Check{Name: "postgres", Required: true, Probe: ok()},
			health.Check{Name: "redis", Required: true, Probe: failing(errors.New(connectionDetails))},
		)
		r, _ := probeRouter(t, reporter)

		rec, _ := probe(t, r, readinessPath)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("status = %d, want 503 when Redis is required", rec.Code)
		}
	})

	t.Run("not required", func(t *testing.T) {
		reporter := started(time.Second,
			health.Check{Name: "postgres", Required: true, Probe: ok()},
			health.Check{Name: "redis", Required: false, Probe: failing(errors.New(connectionDetails))},
		)
		r, _ := probeRouter(t, reporter)

		rec, body := probe(t, r, readinessPath)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 when Redis is not required by this role", rec.Code)
		}
		// The failure is still reported, just not fatal — an operator can see
		// it without the API being pulled from the pool.
		if body.Checks["redis"] != health.StatusFailed {
			t.Errorf("redis = %q, want the failure still visible", body.Checks["redis"])
		}
	})
}

// A deliberately disabled optional provider is not a readiness input at all.
func TestDisabledOptionalProviderDoesNotFailReadiness(t *testing.T) {
	reporter := started(time.Second,
		health.Check{Name: "postgres", Required: true, Probe: ok()},
		// No probe: the deployment does not use this provider, so there is
		// nothing to check and nothing to fail.
		health.Check{Name: "tap", Required: false},
	)
	r, _ := probeRouter(t, reporter)

	rec, body := probe(t, r, readinessPath)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: a disabled provider must not fail global readiness", rec.Code)
	}
	if body.Checks["tap"] != health.StatusSkipped {
		t.Errorf("tap = %q, want skipped", body.Checks["tap"])
	}
}

// The probe response is unauthenticated. Nothing about the connection may
// appear in it — only in the protected log.
func TestProbeResponsesLeakNoConnectionDetails(t *testing.T) {
	reporter := started(time.Second,
		health.Check{Name: "postgres", Required: true, Probe: failing(errors.New(connectionDetails))},
	)
	r, buf := probeRouter(t, reporter)

	rec, _ := probe(t, r, readinessPath)

	body := rec.Body.String()
	for _, leaked := range []string{
		"db.internal", "5432", "gradex", "hunter2", "password",
		"schema_migrations", "pq:", "0001_init.up.sql", "file://",
	} {
		if strings.Contains(body, leaked) {
			t.Errorf("probe response leaked %q: %s", leaked, body)
		}
	}

	// The detail must still reach the protected log, correlated by request ID.
	var found bool
	for _, rec := range buf.records(t) {
		if rec["msg"] == "dependency_unready" {
			found = true
			if rec["check"] != "postgres" {
				t.Errorf("check = %v, want postgres", rec["check"])
			}
			if rec["request_id"] == "" {
				t.Error("dependency failure was not correlated")
			}
		}
	}
	if !found {
		t.Error("the failure detail did not reach the protected log")
	}
}

// A hung dependency must not extend the probe past what the orchestrator will
// wait for.
func TestProbeTimesOutPromptly(t *testing.T) {
	const budget = 100 * time.Millisecond

	reporter := started(budget,
		health.Check{Name: "postgres", Required: true, Probe: func(ctx context.Context) error {
			// Models a dependency that accepts the connection and never
			// answers.
			<-ctx.Done()
			return ctx.Err()
		}},
	)
	r, _ := probeRouter(t, reporter)

	start := time.Now()
	rec, body := probe(t, r, readinessPath)
	elapsed := time.Since(start)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
	if body.Checks["postgres"] != health.StatusFailed {
		t.Errorf("postgres = %q, want failed", body.Checks["postgres"])
	}
	if elapsed > budget*5 {
		t.Errorf("probe took %s, well past its %s budget", elapsed, budget)
	}
}

// Readiness is false before startup completes and false again once draining
// begins; liveness stays true across both.
func TestLifecycleGate(t *testing.T) {
	reporter := health.New(time.Second,
		health.Check{Name: "postgres", Required: true, Probe: ok()},
	)
	r, _ := probeRouter(t, reporter)

	t.Run("before startup completes", func(t *testing.T) {
		rec, body := probe(t, r, readinessPath)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("status = %d, want 503 before MarkStarted", rec.Code)
		}
		if body.Checks["postgres"] != health.StatusSkipped {
			t.Errorf("postgres = %q: dependencies should not be probed before startup completes",
				body.Checks["postgres"])
		}
		if rec, _ := probe(t, r, livenessPath); rec.Code != http.StatusOK {
			t.Error("liveness should be healthy during startup")
		}
	})

	t.Run("once started", func(t *testing.T) {
		reporter.MarkStarted()
		if rec, _ := probe(t, r, readinessPath); rec.Code != http.StatusOK {
			t.Errorf("status = %d, want 200 once started with healthy dependencies", rec.Code)
		}
	})

	t.Run("draining flips readiness before termination", func(t *testing.T) {
		reporter.MarkDraining()

		rec, _ := probe(t, r, readinessPath)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("readiness = %d, want 503 while draining", rec.Code)
		}
		// A draining process should be removed from the pool, not killed.
		if rec, _ := probe(t, r, livenessPath); rec.Code != http.StatusOK {
			t.Error("liveness must stay healthy while draining")
		}
	})

	t.Run("draining is one-way", func(t *testing.T) {
		reporter.MarkStarted()
		if rec, _ := probe(t, r, readinessPath); rec.Code != http.StatusServiceUnavailable {
			t.Error("a draining process must not become ready again")
		}
	})
}

// Probes are unauthenticated and carry no session or CSRF requirement.
func TestProbesRequireNoAuthentication(t *testing.T) {
	reporter := started(time.Second, health.Check{Name: "postgres", Required: true, Probe: ok()})
	r, _ := probeRouter(t, reporter)

	for _, path := range []string{livenessPath, readinessPath} {
		rec := do(r, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden {
			t.Errorf("%s required authentication (status %d)", path, rec.Code)
		}
		if rec.Header().Get("Set-Cookie") != "" {
			t.Errorf("%s created a session", path)
		}
	}
}

// Successful probes drop to debug so they cannot bury real traffic; failures
// stay visible.
func TestSuccessfulProbesAreLoggedAtDebug(t *testing.T) {
	reporter := started(time.Second, health.Check{Name: "postgres", Required: true, Probe: ok()})
	r, buf := probeRouter(t, reporter)

	probe(t, r, readinessPath)

	for _, rec := range buf.records(t) {
		if rec["msg"] == "http_request" && rec["route_template"] == readinessPath {
			if rec["level"] != "DEBUG" {
				t.Errorf("successful probe logged at %v, want DEBUG", rec["level"])
			}
		}
	}
}

package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/logging"
	"github.com/Owlah2025/gradex/backend/internal/ratelimit"
)

type sourceKeyStore struct{ entries []ratelimit.Entry }

func (s *sourceKeyStore) Decide(_ context.Context, entries []ratelimit.Entry) (bool, error) {
	s.entries = append(s.entries, entries...)
	return true, nil
}

func progressSourceKey(t *testing.T, trustedProxies, remoteAddress, forwarded string) string {
	t.Helper()
	values := map[string]string{
		"APP_ENV": "development", "REDIS_ADDR": "localhost:6379",
		"S3_ENDPOINT": "http://localhost:9000", "S3_BUCKET": "gradex-test",
	}
	if trustedProxies != "" {
		values["TRUSTED_PROXIES"] = trustedProxies
	}
	cfg, err := config.LoadFrom(config.MapLookup(values), config.MapSecretResolver{
		"DATABASE_URL": "postgres://x", "S3_ACCESS_KEY": "a", "S3_SECRET_KEY": "b", "PLAYBACK_TOKEN_SECRET": "c",
	})
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}
	store := &sourceKeyStore{}
	limiter, err := ratelimit.New(store, bytes.Repeat([]byte{0x54}, 32), time.Second)
	if err != nil {
		t.Fatalf("constructing source limiter: %v", err)
	}
	engine, err := newEngine(cfg, logging.New(&syncBuffer{}, "gradex-api-test", "development", logging.LevelFromString("info")))
	if err != nil {
		t.Fatalf("constructing engine: %v", err)
	}
	handler := &learningHandlers{foundation: &LearningFoundation{
		limiter: limiter, policies: testLearningPolicies(),
	}}
	engine.GET("/source", func(c *gin.Context) {
		if handler.requireRateDecision(c, "learning-progress-source", ratelimit.Input{ClientIP: c.ClientIP()}) {
			c.Status(http.StatusNoContent)
		}
	})
	request := httptest.NewRequest(http.MethodGet, "/source", nil)
	request.RemoteAddr = remoteAddress
	if forwarded != "" {
		request.Header.Set("X-Forwarded-For", forwarded)
	}
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || len(store.entries) != 1 {
		t.Fatalf("source request = %d entries=%v", response.Code, store.entries)
	}
	return store.entries[0].Key
}

func TestProgressSourceAddressRespectsTrustedProxyPolicy(t *testing.T) {
	untrustedForwarded := progressSourceKey(t, "", "198.51.100.7:443", "203.0.113.9")
	untrustedDirect := progressSourceKey(t, "", "198.51.100.7:443", "")
	if untrustedForwarded != untrustedDirect {
		t.Fatal("untrusted forwarding header changed the source-address key")
	}

	trustedForwarded := progressSourceKey(t, "198.51.100.0/24", "198.51.100.7:443", "203.0.113.9")
	trustedDirect := progressSourceKey(t, "", "203.0.113.9:443", "")
	if trustedForwarded != trustedDirect {
		t.Fatal("trusted proxy forwarding header did not select the forwarded source address")
	}
}

func TestProgressSourceAddressKeysIPv6NetworksIndependently(t *testing.T) {
	ipv4A := progressSourceKey(t, "", "192.0.2.1:443", "")
	ipv4B := progressSourceKey(t, "", "192.0.2.2:443", "")
	if ipv4A == ipv4B {
		t.Fatal("individual IPv4 addresses shared source capacity")
	}
	sameNetworkA := progressSourceKey(t, "", "[2001:db8:1:2::1]:443", "")
	sameNetworkB := progressSourceKey(t, "", "[2001:db8:1:2:ffff::1]:443", "")
	differentNetwork := progressSourceKey(t, "", "[2001:db8:1:3::1]:443", "")
	if sameNetworkA != sameNetworkB {
		t.Fatal("IPv6 addresses in one /64 received different source capacity")
	}
	if sameNetworkA == differentNetwork {
		t.Fatal("IPv6 addresses in different /64 prefixes shared source capacity")
	}
}

func TestProgressSourceCapacityIsSharedAcrossStudents(t *testing.T) {
	store := &sourceKeyStore{}
	limiter, err := ratelimit.New(store, bytes.Repeat([]byte{0x55}, 32), time.Second)
	if err != nil {
		t.Fatalf("constructing source limiter: %v", err)
	}
	policy := ratelimit.ProtectedLearningProgressSourcePolicy()
	for _, student := range []string{"student-one", "student-two"} {
		if decision := limiter.Decide(context.Background(), policy, ratelimit.Input{ClientIP: "192.0.2.10", Identifier: student}); !decision.Allowed {
			t.Fatalf("source decision for %s = %+v", student, decision)
		}
	}
	if len(store.entries) != 2 || store.entries[0].Key != store.entries[1].Key {
		t.Fatal("students behind one source did not consume the same source-address capacity")
	}
}

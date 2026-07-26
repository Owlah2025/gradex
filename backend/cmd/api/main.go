package main

import (
	"context"
	"crypto/rand"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Owlah2025/gradex/backend/internal/auth"
	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/db"
	"github.com/Owlah2025/gradex/backend/internal/health"
	"github.com/Owlah2025/gradex/backend/internal/httpapi"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/logging"
	"github.com/Owlah2025/gradex/backend/internal/outbox"
	"github.com/Owlah2025/gradex/backend/internal/queue"
	"github.com/Owlah2025/gradex/backend/internal/ratelimit"
	"github.com/Owlah2025/gradex/backend/internal/storage"
	"github.com/Owlah2025/gradex/backend/internal/video"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func main() {
	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}

	logger := logging.New(os.Stdout, "gradex-api", string(cfg.Environment()), logging.LevelFromString(cfg.LogLevel()))

	pool, err := db.Connect(ctx, cfg.DatabaseURL().Expose())
	if err != nil {
		log.Fatalf("connecting to database: %v", err)
	}
	defer pool.Close()

	storageClient, err := storage.New(ctx, storage.Options{
		Endpoint:     cfg.S3Endpoint(),
		AccessKey:    cfg.S3AccessKey().Expose(),
		SecretKey:    cfg.S3SecretKey().Expose(),
		Bucket:       cfg.S3Bucket(),
		Region:       cfg.S3Region(),
		UsePathStyle: cfg.S3UsePathStyle(),
	})
	if err != nil {
		log.Fatalf("connecting to storage: %v", err)
	}

	queueClient := queue.NewClient(cfg.RedisAddr())
	defer queueClient.Close()

	redisHealth := queue.NewHealthClient(cfg.RedisAddr())
	defer redisHealth.Close()

	var routerOptions []httpapi.RouterOption
	var admissionRedis *redis.Client
	if cfg.Admission().Enabled() {
		foundation, limiterClient, err := buildAdmissionFoundation(cfg, pool)
		if err != nil {
			log.Fatalf("building Student admission foundation: %v", err)
		}
		admissionRedis = limiterClient
		routerOptions = append(routerOptions, httpapi.WithAdmissionFoundation(foundation))
	}
	if admissionRedis != nil {
		defer admissionRedis.Close()
	}

	svc := video.NewService(pool, storageClient, queueClient, cfg)

	// Belt and braces: config validation already refuses AUTH_FAKE_MODE when
	// APP_ENV=production, and the real authenticator does not exist yet, so the
	// API still refuses to start without the development seam.
	if !cfg.AuthFakeMode() {
		log.Fatal("real auth is not implemented yet — set AUTH_FAKE_MODE=true (dev/test only)")
	}
	authenticator := auth.NewFakeAuthenticator()
	entitlements := auth.NewFakeEntitlementChecker(pool)

	reporter := health.New(cfg.ReadinessTimeout(),
		health.Check{
			Name:     "postgres",
			Required: true,
			Probe:    func(ctx context.Context) error { return db.Ping(ctx, pool) },
		},
		health.Check{
			Name:     "schema",
			Required: true,
			Probe:    func(ctx context.Context) error { return db.CheckSchema(ctx, pool) },
		},
		health.Check{
			Name: "redis",
			// Required by role, not by a generic flag: this becomes false for
			// the API once command admission is durable in PostgreSQL alone.
			Required: cfg.ServiceRole().RequiresRedis(),
			Probe:    redisHealth.Ping,
		},
	)

	// Authorization reads Account and credential state from PostgreSQL on every
	// protected request rather than trusting anything carried in the session.
	principals := identity.NewDBPrincipalResolver(pool)

	router, err := httpapi.NewRouter(
		cfg,
		logger,
		reporter,
		svc,
		authenticator,
		entitlements,
		principals,
		routerOptions...,
	)
	if err != nil {
		log.Fatalf("building router: %v", err)
	}

	server := &http.Server{
		Addr:         ":" + cfg.Port(),
		Handler:      router,
		ReadTimeout:  cfg.HTTPReadTimeout(),
		WriteTimeout: cfg.HTTPWriteTimeout(),
		IdleTimeout:  cfg.HTTPIdleTimeout(),
	}

	serverErr := make(chan error, 1)
	go func() {
		log.Printf("gradex API listening on :%s (env=%s, role=%s, payments=%v, email=%v, fake_auth=%v)",
			cfg.Port(), cfg.Environment(), cfg.ServiceRole(),
			cfg.Payments().Enabled(), cfg.Email().Enabled(), cfg.AuthFakeMode())
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	// Readiness opens only once configuration, dependencies, and routing are
	// all constructed. Before this the process answers liveness but takes no
	// traffic.
	reporter.MarkStarted()

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		log.Fatalf("server error: %v", err)
	case sig := <-shutdown:
		log.Printf("received %s, draining", sig)
	}

	// Fail readiness first so the load balancer stops sending new requests,
	// then finish what is already in flight. Liveness stays healthy throughout
	// — a draining process should be removed from the pool, not killed.
	reporter.MarkDraining()

	drainCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout())
	defer cancel()

	if err := server.Shutdown(drainCtx); err != nil {
		log.Printf("graceful shutdown did not complete: %v", err)
	}
	log.Println("gradex API stopped")
}

func buildAdmissionFoundation(
	cfg *config.Config,
	pool *pgxpool.Pool,
) (*httpapi.AdmissionFoundation, *redis.Client, error) {
	admission := cfg.Admission()
	if cfg.Environment() != config.EnvDevelopment {
		return nil, nil, errors.New(
			"approved production policy and compromised-password adapters are not integrated",
		)
	}

	compromised, err := identity.NewDeterministicCompromisedSource()
	if err != nil {
		return nil, nil, err
	}
	compromisedSource, err := identity.NewTimeoutCompromisedSource(
		compromised, admission.CompromisedPasswordTimeout(),
	)
	if err != nil {
		return nil, nil, err
	}
	policies, err := developmentPolicySets(admission.PolicySetID())
	if err != nil {
		return nil, nil, err
	}

	// These are deliberate signer/encryption boundaries. The values go
	// directly from redacting config wrappers into their owning primitives.
	writer, err := outbox.NewWriter(
		admission.ProtectedPayloadKeyVersion(),
		[]byte(admission.ProtectedPayloadKey().Expose()),
	)
	if err != nil {
		return nil, nil, err
	}
	redisClient := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr()})
	limiter, err := ratelimit.New(
		ratelimit.NewRedisStore(redisClient),
		[]byte(admission.LimiterHMACKey().Expose()),
		admission.RateLimitTimeout(),
	)
	if err != nil {
		_ = redisClient.Close()
		return nil, nil, err
	}

	endpoints := []string{
		"student-registrations",
		"email-verification-requests",
		"email-verifications",
	}
	endpointPolicies := make(map[string]ratelimit.Policy, len(endpoints)+2)
	for _, endpoint := range endpoints {
		endpointPolicies[endpoint] = ratelimit.DevelopmentAdmissionPolicy(endpoint)
	}
	endpointPolicies["session-bootstrap"] = ratelimit.DevelopmentAnonymousBootstrapPolicy()
	endpointPolicies["registration-policy-set"] = ratelimit.DevelopmentPolicySetReadPolicy()

	service, err := identity.NewAdmissionService(identity.AdmissionServiceOptions{
		Pool:            pool,
		Policies:        policies,
		Compromised:     compromisedSource,
		Outbox:          writer,
		VerificationTTL: admission.VerificationTokenTTL(),
		Now:             time.Now,
		Random:          rand.Reader,
	})
	if err != nil {
		_ = redisClient.Close()
		return nil, nil, err
	}

	foundation, err := httpapi.NewAdmissionFoundation(httpapi.AdmissionFoundationOptions{
		PublicOrigin:        cfg.PublicOrigin(),
		CookieSigningKey:    []byte(admission.AnonymousCookieSigningKey().Expose()),
		CSRFKey:             []byte(admission.AnonymousCSRFKey().Expose()),
		AnonymousSessionTTL: admission.AnonymousSessionTTL(),
		Policies:            policies,
		Service:             service,
		Limiter:             limiter,
		EndpointPolicies:    endpointPolicies,
	})
	if err != nil {
		_ = redisClient.Close()
		return nil, nil, err
	}
	return foundation, redisClient, nil
}

func developmentPolicySets(id string) (*identity.StaticPolicySetResolver, error) {
	return identity.NewStaticPolicySetResolver(
		identity.RegistrationPolicySet{
			ID: id, Locale: identity.LocaleEnglish,
			Policies: []identity.RegistrationPolicy{
				{
					Kind: identity.PolicyPrivacyNotice, Version: "dev-privacy-v1",
					Label: "Privacy notice", URL: "/legal/privacy",
				},
				{
					Kind: identity.PolicyTermsOfService, Version: "dev-terms-v1",
					Label: "Terms of service", URL: "/legal/terms",
				},
			},
		},
		identity.RegistrationPolicySet{
			ID: id, Locale: identity.LocaleArabic,
			Policies: []identity.RegistrationPolicy{
				{
					Kind: identity.PolicyPrivacyNotice, Version: "dev-privacy-v1",
					Label: "إشعار الخصوصية", URL: "/legal/privacy",
				},
				{
					Kind: identity.PolicyTermsOfService, Version: "dev-terms-v1",
					Label: "شروط الخدمة", URL: "/legal/terms",
				},
			},
		},
	)
}

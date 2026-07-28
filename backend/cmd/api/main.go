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
	"github.com/Owlah2025/gradex/backend/internal/catalog"
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

	var sessionRepository *identity.SessionRepository
	pf, err := buildProductionFoundations(cfg, pool)
	if err != nil {
		log.Fatalf("building production router foundations: %v", err)
	}
	defer pf.Close()

	routerOptions := pf.Options
	sessionRepository = pf.SessionRepository

	svc := video.NewService(pool, storageClient, queueClient, cfg)

	var authenticator auth.Authenticator
	if cfg.AuthFakeMode() {
		authenticator = auth.NewFakeAuthenticator()
	} else {
		if sessionRepository == nil {
			log.Fatal("SESSION_CSRF_KEY is required when AUTH_FAKE_MODE=false")
		}
		authenticator, err = auth.NewSessionAuthenticator(sessionRepository)
		if err != nil {
			log.Fatalf("building session authenticator: %v", err)
		}
	}
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
			Probe: func(ctx context.Context) error {
				return db.CheckSchemaAtLeast(ctx, pool, requiredSchemaVersion(cfg))
			},
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

func buildSessionFoundation(
	cfg *config.Config,
	pool *pgxpool.Pool,
) (*httpapi.SessionFoundation, *identity.SessionRepository, *redis.Client, error) {
	repository, err := identity.NewSessionRepository(identity.SessionRepositoryOptions{
		Pool: pool, Settings: cfg.Sessions(),
		CSRFKey: []byte(cfg.Sessions().CSRFKey().Expose()), Now: time.Now,
	})
	if err != nil {
		return nil, nil, nil, err
	}

	admission := cfg.Admission()
	redisClient := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr()})
	limiter, err := ratelimit.New(
		ratelimit.NewRedisStore(redisClient),
		[]byte(admission.LimiterHMACKey().Expose()),
		admission.RateLimitTimeout(),
	)
	if err != nil {
		_ = redisClient.Close()
		return nil, nil, nil, err
	}

	endpointPolicies := map[string]ratelimit.Policy{
		"session-bootstrap":  ratelimit.DevelopmentAnonymousBootstrapPolicy(),
		"sessions":           ratelimit.DevelopmentLoginPolicy(),
		"session-resolution": ratelimit.DevelopmentSessionPolicy("session-resolution"),
		"session-renewals":   ratelimit.DevelopmentSessionPolicy("session-renewals"),
		"session-logout":     ratelimit.DevelopmentSessionPolicy("session-logout"),
	}
	foundation, err := httpapi.NewSessionFoundation(httpapi.SessionFoundationOptions{
		PublicOrigin:        cfg.PublicOrigin(),
		CookieSigningKey:    []byte(admission.AnonymousCookieSigningKey().Expose()),
		AnonymousCSRFKey:    []byte(admission.AnonymousCSRFKey().Expose()),
		AnonymousSessionTTL: admission.AnonymousSessionTTL(),
		Repository:          repository,
		Limiter:             limiter,
		EndpointPolicies:    endpointPolicies,
	})
	if err != nil {
		_ = redisClient.Close()
		return nil, nil, nil, err
	}
	return foundation, repository, redisClient, nil
}

func requiredSchemaVersion(cfg *config.Config) int64 {
	return db.RevisionIntegritySchemaVersion
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
		"password-reset-requests",
	}
	endpointPolicies := make(map[string]ratelimit.Policy, len(endpoints)+2)
	for _, endpoint := range endpoints {
		endpointPolicies[endpoint] = ratelimit.DevelopmentAdmissionPolicy(endpoint)
	}
	// Completion gets its own stricter policy rather than the generic
	// admission one: it is the only anonymous route that reaches Argon2id.
	endpointPolicies["password-resets"] = ratelimit.DevelopmentPasswordResetCompletionPolicy()
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

	recovery, err := identity.NewRecoveryService(identity.RecoveryServiceOptions{
		Pool:        pool,
		Outbox:      writer,
		Compromised: compromisedSource,
		ResetTTL:    admission.PasswordResetTokenTTL(),
		Now:         time.Now,
		Random:      rand.Reader,
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
		Recovery:            recovery,
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

func buildStaffFoundation(
	cfg *config.Config,
	pool *pgxpool.Pool,
) (*httpapi.StaffFoundation, *redis.Client, error) {
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

	writer, err := outbox.NewWriter(
		admission.ProtectedPayloadKeyVersion(),
		[]byte(admission.ProtectedPayloadKey().Expose()),
	)
	if err != nil {
		return nil, nil, err
	}

	service, err := identity.NewStaffService(pool, writer, compromisedSource)
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

	endpointPolicies := map[string]ratelimit.Policy{
		"staff-invitations-create":   ratelimit.DevelopmentStaffInvitationPolicy("staff-invitations-create"),
		"staff-invitations-preview":  ratelimit.DevelopmentStaffInvitationPolicy("staff-invitations-preview"),
		"staff-invitations-complete": ratelimit.DevelopmentStaffInvitationPolicy("staff-invitations-complete"),
	}

	foundation, err := httpapi.NewStaffFoundation(httpapi.StaffFoundationOptions{
		Service:          service,
		Compromised:      compromisedSource,
		Limiter:          limiter,
		EndpointPolicies: endpointPolicies,
		RecentAuthWindow: cfg.Sessions().HighestRiskRecentAuthWindow(),
	})
	if err != nil {
		_ = redisClient.Close()
		return nil, nil, err
	}
	return foundation, redisClient, nil
}

func buildCatalogFoundation(
	cfg *config.Config,
	pool *pgxpool.Pool,
) (*httpapi.CatalogFoundation, error) {
	admission := cfg.Admission()
	writer, err := outbox.NewWriter(
		admission.ProtectedPayloadKeyVersion(),
		[]byte(admission.ProtectedPayloadKey().Expose()),
	)
	if err != nil {
		return nil, err
	}

	repository, err := catalog.NewRepository(pool, writer)
	if err != nil {
		return nil, err
	}

	assetValidator := catalog.NewDBAssetVersionValidator(pool)

	return httpapi.NewCatalogFoundation(httpapi.CatalogFoundationOptions{
		Repository:     repository,
		AssetValidator: assetValidator,
	})
}

type ProductionFoundations struct {
	Options           []httpapi.RouterOption
	SessionRepository *identity.SessionRepository
	SessionRedis      *redis.Client
	AdmissionRedis    *redis.Client
	StaffRedis        *redis.Client
}

func (f *ProductionFoundations) Close() {
	if f.SessionRedis != nil {
		_ = f.SessionRedis.Close()
	}
	if f.AdmissionRedis != nil {
		_ = f.AdmissionRedis.Close()
	}
	if f.StaffRedis != nil {
		_ = f.StaffRedis.Close()
	}
}

func buildProductionFoundations(
	cfg *config.Config,
	pool *pgxpool.Pool,
) (*ProductionFoundations, error) {
	pf := &ProductionFoundations{}

	if cfg.Sessions().Enabled() {
		sessionFoundation, sessionRepo, sessionRedis, err := buildSessionFoundation(cfg, pool)
		if err != nil {
			pf.Close()
			return nil, err
		}
		pf.SessionRepository = sessionRepo
		pf.SessionRedis = sessionRedis
		pf.Options = append(pf.Options, httpapi.WithSessionFoundation(sessionFoundation))
	}

	if cfg.Admission().Enabled() {
		foundation, limiterClient, err := buildAdmissionFoundation(cfg, pool)
		if err != nil {
			pf.Close()
			return nil, err
		}
		pf.AdmissionRedis = limiterClient
		pf.Options = append(pf.Options, httpapi.WithAdmissionFoundation(foundation))
	}

	if cfg.Admission().Enabled() {
		foundation, limiterClient, err := buildStaffFoundation(cfg, pool)
		if err != nil {
			pf.Close()
			return nil, err
		}
		pf.StaffRedis = limiterClient
		pf.Options = append(pf.Options, httpapi.WithStaffFoundation(foundation))
	}

	catalogFoundation, err := buildCatalogFoundation(cfg, pool)
	if err != nil {
		pf.Close()
		return nil, err
	}
	pf.Options = append(pf.Options, httpapi.WithCatalogFoundation(catalogFoundation))

	return pf, nil
}

package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Owlah2025/gradex/backend/internal/academic"
	"github.com/Owlah2025/gradex/backend/internal/access"
	"github.com/Owlah2025/gradex/backend/internal/auth"
	"github.com/Owlah2025/gradex/backend/internal/catalog"
	"github.com/Owlah2025/gradex/backend/internal/catalogpublic"
	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/db"
	"github.com/Owlah2025/gradex/backend/internal/entitlement"
	"github.com/Owlah2025/gradex/backend/internal/health"
	"github.com/Owlah2025/gradex/backend/internal/httpapi"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/learning"
	"github.com/Owlah2025/gradex/backend/internal/logging"
	"github.com/Owlah2025/gradex/backend/internal/media"
	"github.com/Owlah2025/gradex/backend/internal/outbox"
	"github.com/Owlah2025/gradex/backend/internal/queue"
	"github.com/Owlah2025/gradex/backend/internal/ratelimit"
	"github.com/Owlah2025/gradex/backend/internal/storage"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func main() {
	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}
	if cfg.ServiceRole() != config.RoleAPI {
		log.Fatalf("SERVICE_ROLE=%s cannot run the API process; expected %s", cfg.ServiceRole(), config.RoleAPI)
	}
	if cfg.Environment() != config.EnvDevelopment {
		gin.SetMode(gin.ReleaseMode)
	}

	logger := logging.New(os.Stdout, "gradex-api", string(cfg.Environment()), logging.LevelFromString(cfg.LogLevel()))

	pool, err := db.Connect(ctx, cfg.DatabaseURL().Expose())
	if err != nil {
		log.Fatalf("connecting to database: %v", err)
	}
	defer pool.Close()

	storageClient, err := storage.New(ctx, storage.Options{
		Endpoint:        cfg.S3Endpoint(),
		PresignEndpoint: cfg.S3PresignEndpoint(),
		AccessKey:       cfg.S3AccessKey().Expose(),
		SecretKey:       cfg.S3SecretKey().Expose(),
		Bucket:          cfg.S3Bucket(),
		Region:          cfg.S3Region(),
		UsePathStyle:    cfg.S3UsePathStyle(),
	})
	if err != nil {
		log.Fatalf("connecting to storage: %v", err)
	}

	redisConnection, err := queue.NewConnection(cfg.Redis())
	if err != nil {
		log.Fatalf("configuring Redis: %v", err)
	}
	queueClient := redisConnection.NewClient()
	defer queueClient.Close()

	redisHealth := redisConnection.NewHealthClient()
	defer redisHealth.Close()

	var sessionRepository *identity.SessionRepository
	pf, err := buildProductionFoundations(cfg, pool, redisConnection)
	if err != nil {
		log.Fatalf("building production router foundations: %v", err)
	}
	defer pf.Close()

	routerOptions := pf.Options
	sessionRepository = pf.SessionRepository

	mediaFoundation, err := buildMediaFoundation(cfg, pool, storageClient, pf.PreviewRateLimiter)
	if err != nil {
		log.Fatalf("building media foundation: %v", err)
	}
	routerOptions = append(routerOptions, httpapi.WithMediaFoundation(mediaFoundation))
	learningFoundation, learningRedis, err := buildLearningFoundation(cfg, pool, mediaFoundation, redisConnection)
	if err != nil {
		log.Fatalf("building learning foundation: %v", err)
	}
	defer learningRedis.Close()
	routerOptions = append(routerOptions, httpapi.WithLearningFoundation(learningFoundation))

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
		authenticator,
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
	redisConnection *queue.Connection,
) (*httpapi.SessionFoundation, *identity.SessionRepository, *redis.Client, error) {
	loginAdmission := cfg.LoginAdmission()
	passwordGate, err := identity.NewPasswordVerificationGate(identity.PasswordVerificationGateOptions{
		Concurrency: loginAdmission.VerificationConcurrency(),
		Queue:       loginAdmission.VerificationQueue(),
		QueueWait:   loginAdmission.VerificationQueueWait(),
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("building password verification gate: %w", err)
	}
	repository, err := identity.NewSessionRepository(identity.SessionRepositoryOptions{
		Pool: pool, Settings: cfg.Sessions(),
		CSRFKey: []byte(cfg.Sessions().CSRFKey().Expose()), Now: time.Now,
		PasswordVerificationGate: passwordGate,
	})
	if err != nil {
		return nil, nil, nil, err
	}

	admission := cfg.Admission()
	redisClient := redisConnection.NewRedisClient()
	limiter, err := ratelimit.New(
		ratelimit.NewRedisStore(redisClient),
		[]byte(admission.LimiterHMACKey().Expose()),
		admission.RateLimitTimeout(),
	)
	if err != nil {
		_ = redisClient.Close()
		return nil, nil, nil, err
	}

	compromisedSource, err := buildCompromisedPasswordSource(cfg)
	if err != nil {
		_ = redisClient.Close()
		return nil, nil, nil, err
	}

	endpointPolicies := sessionPolicies(cfg.Environment())
	for endpoint, policy := range map[string]ratelimit.Policy{
		"session-resolution": ratelimit.DevelopmentSessionPolicy("session-resolution"),
		"session-renewals":   ratelimit.DevelopmentSessionPolicy("session-renewals"),
		"session-logout":     ratelimit.DevelopmentSessionPolicy("session-logout"),
		"password-changes":   ratelimit.DevelopmentSessionPolicy("password-changes"),
	} {
		endpointPolicies[endpoint] = policy
	}
	foundation, err := httpapi.NewSessionFoundation(httpapi.SessionFoundationOptions{
		PublicOrigin:        cfg.PublicOrigin(),
		CookieSigningKey:    []byte(admission.AnonymousCookieSigningKey().Expose()),
		AnonymousCSRFKey:    []byte(admission.AnonymousCSRFKey().Expose()),
		AnonymousSessionTTL: admission.AnonymousSessionTTL(),
		Repository:          repository,
		Compromised:         compromisedSource,
		Limiter:             limiter,
		EndpointPolicies:    endpointPolicies,
		LoginRequestTimeout: loginAdmission.RequestTimeout(),
	})
	if err != nil {
		_ = redisClient.Close()
		return nil, nil, nil, err
	}
	return foundation, repository, redisClient, nil
}

func sessionPolicies(environment config.Environment) map[string]ratelimit.Policy {
	if environment == config.EnvDevelopment {
		return map[string]ratelimit.Policy{
			"session-bootstrap": ratelimit.DevelopmentAnonymousBootstrapPolicy(),
			"sessions":          ratelimit.DevelopmentLoginPolicy(),
		}
	}
	return map[string]ratelimit.Policy{
		"session-bootstrap": ratelimit.ProductionAnonymousBootstrapPolicy(),
		"sessions":          ratelimit.ProductionLoginPolicy(),
	}
}

func requiredSchemaVersion(cfg *config.Config) int64 {
	return db.ProtectedLearningSchemaVersion
}

func buildLearningFoundation(
	cfg *config.Config,
	pool *pgxpool.Pool,
	mediaFoundation *httpapi.MediaFoundation,
	redisConnection *queue.Connection,
) (*httpapi.LearningFoundation, *redis.Client, error) {
	if mediaFoundation == nil {
		return nil, nil, errors.New("learning media foundation is required")
	}
	repository, err := learning.NewRepository(pool)
	if err != nil {
		return nil, nil, fmt.Errorf("building learning repository: %w", err)
	}
	entitlementRepository, err := entitlement.NewRepository(pool)
	if err != nil {
		return nil, nil, fmt.Errorf("building learning entitlement reader: %w", err)
	}
	evaluator, err := entitlement.NewEvaluator(entitlementRepository)
	if err != nil {
		return nil, nil, fmt.Errorf("building learning entitlement evaluator: %w", err)
	}
	admission := cfg.Admission()
	redisClient := redisConnection.NewRedisClient()
	limiter, err := ratelimit.New(
		ratelimit.NewRedisStore(redisClient),
		[]byte(admission.LimiterHMACKey().Expose()),
		admission.RateLimitTimeout(),
	)
	if err != nil {
		_ = redisClient.Close()
		return nil, nil, fmt.Errorf("building learning rate limiter: %w", err)
	}
	// Report-context issuer (D-065). The key is derived from an existing application cryptographic
	// secret with explicit domain separation, so it can neither sign nor be signed by any other
	// artefact; the root secret is never used directly. Composition fails closed: a build that
	// cannot mint contexts must not serve protected learning at all, because it would render
	// content the Student cannot report accurately.
	rootSecret := []byte(cfg.Sessions().CSRFKey().Expose())
	if len(rootSecret) < 32 {
		_ = redisClient.Close()
		return nil, nil, fmt.Errorf("report context root secret must contain at least 32 bytes")
	}
	reportContexts, err := learning.NewReportContextSigner(
		learning.DeriveReportContextKey(rootSecret),
		learning.DefaultReportContextLifetime,
		func() time.Time { return time.Now().UTC() },
		func(b []byte) error { _, err := rand.Read(b); return err },
	)
	if err != nil {
		_ = redisClient.Close()
		return nil, nil, fmt.Errorf("building report context issuer: %w", err)
	}

	foundation, err := httpapi.NewLearningFoundation(httpapi.LearningFoundationOptions{
		Repository: repository, Evaluator: evaluator, Media: mediaFoundation.LearningMedia(),
		ReportContexts: reportContexts, Limiter: limiter,
		Policies: map[string]ratelimit.Policy{
			"learning-playback-source": ratelimit.ProtectedLearningPlaybackSourcePolicy(),
			"learning-playback":        ratelimit.ProtectedLearningPlaybackPolicy(),
			"learning-progress-source": ratelimit.ProtectedLearningProgressSourcePolicy(),
			"learning-progress":        ratelimit.ProtectedLearningProgressPolicy(),
			"learning-report":          ratelimit.ProtectedLearningReportPolicy(),
		},
	})
	if err != nil {
		_ = redisClient.Close()
		return nil, nil, err
	}
	return foundation, redisClient, nil
}

func buildMediaFoundation(cfg *config.Config, pool *pgxpool.Pool, storageClient *storage.Client, previewRateLimiter *ratelimit.Limiter) (*httpapi.MediaFoundation, error) {
	admission := cfg.Admission()
	writer, err := outbox.NewWriter(
		admission.ProtectedPayloadKeyVersion(),
		[]byte(admission.ProtectedPayloadKey().Expose()),
	)
	if err != nil {
		return nil, fmt.Errorf("building media outbox writer: %w", err)
	}
	configuredScanner, err := media.NewConfiguredScanner(string(cfg.MediaScannerMode()), string(cfg.Environment()))
	if err != nil {
		return nil, err
	}
	scanner, err := media.NewScannerAdapter(configuredScanner)
	if err != nil {
		return nil, err
	}
	service, err := media.NewService(media.ServiceOptions{
		DB: pool, Store: storageClient, Outbox: writer, Scanner: scanner,
		UploadURLExpiry: cfg.UploadURLExpiry(), MaxUploadBytes: cfg.MaxUploadSizeBytes(),
		OperatingMode: media.OperatingMode(cfg.MediaOperatingMode()),
	})
	if err != nil {
		return nil, err
	}
	entitlementRepository, err := entitlement.NewRepository(pool)
	if err != nil {
		return nil, fmt.Errorf("building entitlement read repository: %w", err)
	}
	evaluator, err := entitlement.NewEvaluator(entitlementRepository)
	if err != nil {
		return nil, fmt.Errorf("building entitlement evaluator: %w", err)
	}
	delivery, err := media.NewDeliveryService(media.DeliveryOptions{
		DB: pool, Store: storageClient, Evaluator: evaluator,
		SignatureLifetime: cfg.PlaybackURLExpiry(), BuyerTagKey: []byte(cfg.PlaybackTokenSecret().Expose()),
	})
	if err != nil {
		return nil, fmt.Errorf("building protected media delivery: %w", err)
	}
	if previewRateLimiter == nil {
		return nil, errors.New("public preview rate limiter is required")
	}
	return httpapi.NewMediaFoundation(httpapi.MediaFoundationOptions{
		Service: service, Delivery: delivery,
		PreviewRateLimiter: previewRateLimiter, PreviewRatePolicy: ratelimit.PublicPreviewPolicy(),
	})
}

func buildAdmissionFoundation(
	cfg *config.Config,
	pool *pgxpool.Pool,
	redisConnection *queue.Connection,
) (*httpapi.AdmissionFoundation, *redis.Client, error) {
	admission := cfg.Admission()
	compromisedSource, err := buildCompromisedPasswordSource(cfg)
	if err != nil {
		return nil, nil, err
	}
	policies, err := buildPolicySetResolver(cfg)
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
	redisClient := redisConnection.NewRedisClient()
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
	endpointPolicies["purchase-requests"] = ratelimit.PurchaseRequestsPolicy()
	endpointPolicies["session-bootstrap"] = sessionPolicies(cfg.Environment())["session-bootstrap"]
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

// buildPurchaseAdmissionFoundation keeps public purchase intent protected in
// deployments where Student registration is intentionally disabled. It uses
// the same anonymous admission and rate-limit primitives without requiring
// registration policy, password-screening, or email dependencies.
func buildPurchaseAdmissionFoundation(
	cfg *config.Config,
	redisConnection *queue.Connection,
) (*httpapi.AdmissionFoundation, *redis.Client, error) {
	admission := cfg.Admission()
	redisClient := redisConnection.NewRedisClient()
	limiter, err := ratelimit.New(
		ratelimit.NewRedisStore(redisClient),
		[]byte(admission.LimiterHMACKey().Expose()),
		admission.RateLimitTimeout(),
	)
	if err != nil {
		_ = redisClient.Close()
		return nil, nil, err
	}
	foundation, err := httpapi.NewPurchaseAdmissionFoundation(httpapi.PurchaseAdmissionFoundationOptions{
		PublicOrigin:        cfg.PublicOrigin(),
		CookieSigningKey:    []byte(admission.AnonymousCookieSigningKey().Expose()),
		CSRFKey:             []byte(admission.AnonymousCSRFKey().Expose()),
		AnonymousSessionTTL: admission.AnonymousSessionTTL(),
		Limiter:             limiter,
		PurchasePolicy:      ratelimit.PurchaseRequestsPolicy(),
	})
	if err != nil {
		_ = redisClient.Close()
		return nil, nil, err
	}
	return foundation, redisClient, nil
}

func buildPolicySetResolver(cfg *config.Config) (identity.PolicySetResolver, error) {
	if cfg == nil {
		return nil, errors.New("configuration is required for registration policy resolution")
	}
	if cfg.Environment() == config.EnvDevelopment {
		return developmentPolicySets(cfg.Admission().PolicySetID())
	}
	resolver, err := identity.NewApprovedPolicySetResolver(
		cfg.PublicOrigin(),
		cfg.Admission().PolicySetID(),
	)
	if err != nil {
		return nil, fmt.Errorf("building approved registration policy resolver: %w", err)
	}
	return resolver, nil
}

func developmentPolicySets(id string) (*identity.StaticPolicySetResolver, error) {
	return identity.NewStaticPolicySetResolver(
		identity.RegistrationPolicySet{
			ID: id, Locale: identity.LocaleEnglish,
			Policies: []identity.RegistrationPolicy{
				{
					Kind: identity.PolicyPrivacyNotice, Version: "dev-privacy-v1",
					Label: "Privacy notice", URL: "/en/privacy",
				},
				{
					Kind: identity.PolicyTermsOfService, Version: "dev-terms-v1",
					Label: "Terms of service", URL: "/en/terms",
				},
			},
		},
		identity.RegistrationPolicySet{
			ID: id, Locale: identity.LocaleArabic,
			Policies: []identity.RegistrationPolicy{
				{
					Kind: identity.PolicyPrivacyNotice, Version: "dev-privacy-v1",
					Label: "إشعار الخصوصية", URL: "/ar/privacy",
				},
				{
					Kind: identity.PolicyTermsOfService, Version: "dev-terms-v1",
					Label: "شروط الخدمة", URL: "/ar/terms",
				},
			},
		},
	)
}

func buildStaffFoundation(
	cfg *config.Config,
	pool *pgxpool.Pool,
	redisConnection *queue.Connection,
) (*httpapi.StaffFoundation, *redis.Client, error) {
	return buildStaffFoundationWithSource(cfg, pool, redisConnection, buildCompromisedPasswordSource)
}

type compromisedSourceFactory func(*config.Config) (identity.CompromisedRangeSource, error)

func buildStaffFoundationWithSource(
	cfg *config.Config,
	pool *pgxpool.Pool,
	redisConnection *queue.Connection,
	newSource compromisedSourceFactory,
) (*httpapi.StaffFoundation, *redis.Client, error) {
	admission := cfg.Admission()
	// The settings read below are shared with Student admission but are not
	// conditional on it: staff onboarding writes protected outbox rows, rate
	// limits its invitation endpoints, and screens the password chosen at
	// completion. Enabled sessions already require ADMISSION_LIMITER_HMAC_KEY
	// and the outbox key is required for every composition, so the only setting
	// a Student-registration-disabled environment can still be missing is
	// PASSWORD_SCREEN_MODE, which fails here by name rather than silently.
	compromisedSource, err := newSource(cfg)
	if err != nil {
		return nil, nil, err
	}
	if err := validateStaffComposition(cfg, pool, redisConnection); err != nil {
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

	redisClient := redisConnection.NewRedisClient()
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
		"staff-invitations-create":   ratelimit.StaffInvitationPolicy("staff-invitations-create"),
		"staff-invitations-preview":  ratelimit.StaffInvitationPolicy("staff-invitations-preview"),
		"staff-invitations-complete": ratelimit.StaffInvitationPolicy("staff-invitations-complete"),
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

func validateStaffComposition(cfg *config.Config, pool *pgxpool.Pool, redisConnection *queue.Connection) error {
	if cfg == nil {
		return errors.New("staff composition precondition failed: configuration is required")
	}
	if cfg.Environment() == config.EnvDevelopment {
		return nil
	}
	if !cfg.Sessions().Enabled() {
		return errors.New("staff composition precondition failed: real session foundation is required")
	}
	if cfg.AuthFakeMode() {
		return errors.New("staff composition precondition failed: fake authentication is not permitted")
	}
	if pool == nil {
		return errors.New("staff composition precondition failed: PostgreSQL staff invitation storage is required")
	}
	if redisConnection == nil {
		return errors.New("staff composition precondition failed: Redis rate limiting is required")
	}
	if cfg.Admission().PasswordScreenMode() != config.PasswordScreenAdapter {
		return errors.New("staff composition precondition failed: production password screening adapter is required")
	}
	email := cfg.Email()
	if !email.Enabled() {
		return errors.New("staff composition precondition failed: transactional email provider is required")
	}
	if email.Provider() != config.EmailProviderResend {
		return errors.New("staff composition precondition failed: EMAIL_PROVIDER=resend is required")
	}
	return nil
}

func buildCompromisedPasswordSource(cfg *config.Config) (identity.CompromisedRangeSource, error) {
	if cfg == nil {
		return nil, errors.New("configuration is required for compromised-password screening")
	}
	source, err := identity.NewRuntimeCompromisedSource(cfg.Environment(), cfg.Admission())
	if err != nil {
		return nil, fmt.Errorf("building compromised-password source: %w", err)
	}
	return source, nil
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

func buildPublicCatalogFoundation(pool *pgxpool.Pool) (*httpapi.PublicCatalogFoundation, error) {
	repository, err := catalogpublic.NewRepository(pool, catalogpublic.PublishedOnly)
	if err != nil {
		return nil, err
	}
	return httpapi.NewPublicCatalogFoundation(httpapi.PublicCatalogFoundationOptions{Repository: repository})
}

func buildAccessFoundation(
	cfg *config.Config,
	pool *pgxpool.Pool,
) (*httpapi.AccessFoundation, error) {
	admission := cfg.Admission()
	writer, err := outbox.NewWriter(
		admission.ProtectedPayloadKeyVersion(),
		[]byte(admission.ProtectedPayloadKey().Expose()),
	)
	if err != nil {
		return nil, err
	}

	repository, err := access.NewRepository(pool, writer)
	if err != nil {
		return nil, err
	}

	return httpapi.NewAccessFoundation(httpapi.AccessFoundationOptions{
		Repository:          repository,
		SalesWhatsAppNumber: cfg.SalesWhatsAppNumber(),
	})
}

type ProductionFoundations struct {
	Options            []httpapi.RouterOption
	SessionRepository  *identity.SessionRepository
	SessionRedis       *redis.Client
	AdmissionRedis     *redis.Client
	StaffRedis         *redis.Client
	PreviewRateLimiter *ratelimit.Limiter
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
	redisConnection *queue.Connection,
) (*ProductionFoundations, error) {
	return buildProductionFoundationsWithStaffSource(cfg, pool, redisConnection, buildCompromisedPasswordSource)
}

// buildProductionFoundationsWithStaffSource is a test-only composition seam.
// Runtime startup always uses buildProductionFoundations and the real HIBP
// factory; no configuration can select this injected dependency.
func buildProductionFoundationsWithStaffSource(
	cfg *config.Config,
	pool *pgxpool.Pool,
	redisConnection *queue.Connection,
	newStaffSource compromisedSourceFactory,
) (*ProductionFoundations, error) {
	pf := &ProductionFoundations{}

	if cfg.Sessions().Enabled() {
		sessionFoundation, sessionRepo, sessionRedis, err := buildSessionFoundation(cfg, pool, redisConnection)
		if err != nil {
			pf.Close()
			return nil, err
		}
		pf.SessionRepository = sessionRepo
		pf.SessionRedis = sessionRedis
		pf.Options = append(pf.Options, httpapi.WithSessionFoundation(sessionFoundation))
	}

	// Purchase requests are anonymous writes even where Student registration is
	// deliberately disabled. In that posture, compose only the same admission
	// security primitives required for purchase intent; registration services
	// and routes remain absent.
	var admissionFoundation *httpapi.AdmissionFoundation
	var limiterClient *redis.Client
	var err error
	if cfg.Admission().Enabled() {
		admissionFoundation, limiterClient, err = buildAdmissionFoundation(cfg, pool, redisConnection)
	} else {
		admissionFoundation, limiterClient, err = buildPurchaseAdmissionFoundation(cfg, redisConnection)
	}
	if err != nil {
		pf.Close()
		return nil, err
	}
	pf.AdmissionRedis = limiterClient
	pf.PreviewRateLimiter = admissionFoundation.RateLimiter()
	if cfg.Admission().Enabled() {
		pf.Options = append(pf.Options, httpapi.WithAdmissionFoundation(admissionFoundation))
	} else {
		pf.Options = append(pf.Options, httpapi.WithAdmissionSecurityFoundation(admissionFoundation))
	}

	// Staff invitation and onboarding is an Admin capability, not part of public
	// Student admission. Gating it on cfg.Admission().Enabled() coupled the two:
	// with STUDENT_REGISTRATION_ENABLED=false — the intended founder posture —
	// no staff foundation was composed and every /api/v1/staff-invitations route
	// answered 404 with route_template="unmatched". Production now evaluates its
	// explicit prerequisites in buildStaffFoundation rather than silently omitting
	// the staff surface.
	//
	// Sessions remain a genuine dependency — staff mutations carry the session
	// and CSRF boundary, and httpapi.NewRouter refuses to build a staff surface
	// without a session foundation — so that gate stays. Student registration is
	// not a dependency and no longer appears here.
	//
	// A staff dependency that is configured wrongly now fails startup with a
	// named error instead of silently dropping the Admin surface, so a
	// misconfigured environment is diagnosable rather than a mystery 404.
	// Founder-Beta is a real staging runtime: it keeps the production session,
	// screening, email, and authorization boundaries while remaining APP_ENV=staging.
	// Staff operations must therefore be composed there too; omitting them makes
	// the authenticated UI fail with an indistinguishable 404.
	if cfg.Sessions().Enabled() && (cfg.Environment() == config.EnvDevelopment ||
		cfg.Environment() == config.EnvStaging || cfg.Environment().IsProduction()) {
		foundation, limiterClient, err := buildStaffFoundationWithSource(cfg, pool, redisConnection, newStaffSource)
		if err != nil {
			pf.Close()
			return nil, fmt.Errorf("composing staff lifecycle: %w", err)
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

	publicCatalogFoundation, err := buildPublicCatalogFoundation(pool)
	if err != nil {
		pf.Close()
		return nil, err
	}
	pf.Options = append(pf.Options, httpapi.WithPublicCatalogFoundation(publicCatalogFoundation))

	accessFoundation, err := buildAccessFoundation(cfg, pool)
	if err != nil {
		pf.Close()
		return nil, err
	}
	pf.Options = append(pf.Options, httpapi.WithAccessFoundation(accessFoundation))

	// D-091 Academic Catalog (T1). Admin-only, and additive: no Course,
	// catalogue, entitlement, or media path reads it yet.
	academicFoundation, err := buildAcademicFoundation(pool)
	if err != nil {
		pf.Close()
		return nil, err
	}
	pf.Options = append(pf.Options, httpapi.WithAcademicFoundation(academicFoundation))

	return pf, nil
}

func buildAcademicFoundation(pool *pgxpool.Pool) (*httpapi.AcademicFoundation, error) {
	repository, err := academic.NewRepository(pool)
	if err != nil {
		return nil, fmt.Errorf("composing academic catalog repository: %w", err)
	}
	foundation, err := httpapi.NewAcademicFoundation(httpapi.AcademicFoundationOptions{Repository: repository})
	if err != nil {
		return nil, fmt.Errorf("composing academic catalog foundation: %w", err)
	}
	return foundation, nil
}

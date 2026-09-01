package config

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// validSettings is a production-shaped deployment that must load cleanly.
// Each test mutates one thing so a failure names exactly one cause.
func validSettings() map[string]string {
	return map[string]string{
		"APP_ENV":                   "production",
		"PUBLIC_ORIGIN":             "https://gradex.example",
		"CORS_ALLOWED_ORIGINS":      "https://gradex.example",
		"CORS_ALLOW_CREDENTIALS":    "true",
		"REDIS_ADDR":                "redis:6379",
		"REDIS_TLS_ENABLED":         "true",
		"S3_ENDPOINT":               "https://storage.example",
		"S3_BUCKET":                 "gradex-media",
		"SALES_WHATSAPP_NUMBER":     "15550000000",
		"AUTH_FAKE_MODE":            "false",
		"LEGAL_IDENTITY_MODE":       "public",
		"LEGAL_OPERATOR_NAME":       "Gradex Courses",
		"LEGAL_REGISTRATION_NUMBER": "KWT-REAL-123",
		"LEGAL_REGISTERED_ADDRESS":  "Kuwait City, Kuwait",
		"PRIVACY_EMAIL":             "privacy@gradex.example",
		"SUPPORT_EMAIL":             "support@gradex.example",
		"SECURITY_EMAIL":            "security@gradex.example",
	}
}

func validSecrets() MapSecretResolver {
	return MapSecretResolver{
		"DATABASE_URL":                 "postgres://gradex:pw@db:5432/gradex",
		"REDIS_PASSWORD":               "redis-password-canary",
		"S3_ACCESS_KEY":                "access",
		"S3_SECRET_KEY":                "secret",
		"PLAYBACK_TOKEN_SECRET":        "9f2c1de4a7b3085c6e1d4f7a2b9c0e3d",
		"SESSION_CSRF_KEY":             strings.Repeat("s", 32),
		"ANONYMOUS_COOKIE_SIGNING_KEY": strings.Repeat("a", 32),
		"ANONYMOUS_CSRF_KEY":           strings.Repeat("b", 32),
		"IDENTITY_OTP_PEPPER":          strings.Repeat("o", 32),
		"ADMISSION_LIMITER_HMAC_KEY":   strings.Repeat("c", 32),
	}
}

func loadWith(t *testing.T, mutate func(map[string]string, MapSecretResolver)) (*Config, error) {
	t.Helper()
	settings, secrets := validSettings(), validSecrets()
	if mutate != nil {
		mutate(settings, secrets)
	}
	return LoadFrom(MapLookup(settings), secrets)
}

func mustLoad(t *testing.T, mutate func(map[string]string, MapSecretResolver)) *Config {
	t.Helper()
	cfg, err := loadWith(t, mutate)
	if err != nil {
		t.Fatalf("expected config to load, got error: %v", err)
	}
	return cfg
}

func wantErrContaining(t *testing.T, mutate func(map[string]string, MapSecretResolver), want string) {
	t.Helper()
	cfg, err := loadWith(t, mutate)
	if err == nil {
		t.Fatalf("expected an error mentioning %q, got a valid config", want)
	}
	if cfg != nil {
		t.Error("a failed Load must not return a Config")
	}
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not mention %q", err.Error(), want)
	}
}

func TestValidProductionConfigLoads(t *testing.T) {
	cfg := mustLoad(t, nil)
	if !cfg.Environment().IsProduction() {
		t.Error("expected production environment")
	}
	sessions := cfg.Sessions()
	if !sessions.Enabled() {
		t.Fatal("production authenticated sessions are disabled")
	}
	for name, test := range map[string]struct {
		window   SessionWindow
		idle     time.Duration
		absolute time.Duration
	}{
		"Student":    {sessions.Student(), 7 * 24 * time.Hour, 30 * 24 * time.Hour},
		"Instructor": {sessions.Instructor(), time.Hour, 24 * time.Hour},
		"Admin":      {sessions.Admin(), 30 * time.Minute, 12 * time.Hour},
	} {
		t.Run(name, func(t *testing.T) {
			if test.window.IdleExpiry() != test.idle {
				t.Errorf("idle expiry = %s, want %s", test.window.IdleExpiry(), test.idle)
			}
			if test.window.AbsoluteExpiry() != test.absolute {
				t.Errorf("absolute expiry = %s, want %s", test.window.AbsoluteExpiry(), test.absolute)
			}
		})
	}
	if sessions.GeneralRecentAuthWindow() != 10*time.Minute {
		t.Errorf("general recent-authentication default = %s, want 10m",
			sessions.GeneralRecentAuthWindow())
	}
	if sessions.HighestRiskRecentAuthWindow() != 5*time.Minute {
		t.Errorf("highest-risk recent-authentication default = %s, want 5m",
			sessions.HighestRiskRecentAuthWindow())
	}
	if sessions.StaleUseWindow() != 5*time.Second {
		t.Errorf("stale-use default = %s, want 5s", sessions.StaleUseWindow())
	}
	if cfg.MediaProcessingTimeout() != 15*time.Minute {
		t.Errorf("media processing timeout default = %s, want 15m", cfg.MediaProcessingTimeout())
	}
	if cfg.MediaOperatingMode() != MediaOperatingModeScanner {
		t.Errorf("media operating mode default = %q, want SCANNER", cfg.MediaOperatingMode())
	}
	login := cfg.LoginAdmission()
	if login.VerificationConcurrency() != 1 || login.VerificationQueue() != 500 ||
		login.VerificationQueueWait() != 45*time.Second || login.RequestTimeout() != time.Minute {
		t.Fatalf("login admission defaults = concurrency %d, queue %d, wait %s, request %s",
			login.VerificationConcurrency(), login.VerificationQueue(),
			login.VerificationQueueWait(), login.RequestTimeout())
	}
	if sessions.CSRFKey().IsEmpty() || strings.Contains(
		fmt.Sprintf("%v", sessions.CSRFKey()), strings.Repeat("s", 32),
	) {
		t.Error("session CSRF key is absent or printable")
	}
}

func TestSalesWhatsAppNumberIsValidatedOutsideDevelopment(t *testing.T) {
	wantErrContaining(t, func(settings map[string]string, _ MapSecretResolver) {
		delete(settings, "SALES_WHATSAPP_NUMBER")
	}, "SALES_WHATSAPP_NUMBER is required outside development")
	wantErrContaining(t, func(settings map[string]string, _ MapSecretResolver) {
		settings["SALES_WHATSAPP_NUMBER"] = "+965 555 00000"
	}, "SALES_WHATSAPP_NUMBER must contain digits only")
	cfg := mustLoad(t, func(settings map[string]string, _ MapSecretResolver) {
		settings["APP_ENV"] = "development"
		delete(settings, "SALES_WHATSAPP_NUMBER")
	})
	if cfg.SalesWhatsAppNumber() != "15550000000" {
		t.Fatalf("development safe number = %q", cfg.SalesWhatsAppNumber())
	}
}

func TestLoginAdmissionConfigurationFailsClosed(t *testing.T) {
	for name, setting := range map[string]string{
		"zero concurrency":     "LOGIN_PASSWORD_VERIFY_CONCURRENCY",
		"zero queue":           "LOGIN_PASSWORD_VERIFY_QUEUE_CAPACITY",
		"zero queue wait":      "LOGIN_PASSWORD_VERIFY_QUEUE_WAIT",
		"zero request timeout": "LOGIN_REQUEST_TIMEOUT",
	} {
		t.Run(name, func(t *testing.T) {
			wantErrContaining(t, func(settings map[string]string, _ MapSecretResolver) {
				settings[setting] = "0"
			}, setting)
		})
	}
	wantErrContaining(t, func(settings map[string]string, _ MapSecretResolver) {
		settings["LOGIN_PASSWORD_VERIFY_QUEUE_WAIT"] = "60s"
		settings["LOGIN_REQUEST_TIMEOUT"] = "60s"
	}, "must be less than LOGIN_REQUEST_TIMEOUT")
}

func TestS3PresignEndpointDefaultsAndValidatesBrowserOrigin(t *testing.T) {
	t.Run("defaults to the internal endpoint", func(t *testing.T) {
		cfg := mustLoad(t, nil)
		if cfg.S3PresignEndpoint() != cfg.S3Endpoint() {
			t.Fatalf("presign endpoint = %q, want internal endpoint %q", cfg.S3PresignEndpoint(), cfg.S3Endpoint())
		}
	})

	t.Run("accepts a distinct HTTPS browser endpoint", func(t *testing.T) {
		cfg := mustLoad(t, func(settings map[string]string, _ MapSecretResolver) {
			settings["S3_PRESIGN_ENDPOINT"] = "https://media.gradex.example"
		})
		if cfg.S3PresignEndpoint() != "https://media.gradex.example" {
			t.Fatalf("presign endpoint = %q", cfg.S3PresignEndpoint())
		}
	})

	for _, environment := range []string{"staging", "production"} {
		t.Run(environment+" requires HTTPS", func(t *testing.T) {
			wantErrContaining(t, func(settings map[string]string, _ MapSecretResolver) {
				settings["APP_ENV"] = environment
				settings["S3_PRESIGN_ENDPOINT"] = "http://storage.internal:9000"
			}, "S3_PRESIGN_ENDPOINT must use HTTPS outside development")
		})
	}

	t.Run("development permits an HTTP browser endpoint", func(t *testing.T) {
		cfg := mustLoad(t, func(settings map[string]string, _ MapSecretResolver) {
			settings["APP_ENV"] = "development"
			settings["S3_PRESIGN_ENDPOINT"] = "http://localhost:9000"
		})
		if cfg.S3PresignEndpoint() != "http://localhost:9000" {
			t.Fatalf("presign endpoint = %q", cfg.S3PresignEndpoint())
		}
	})

	t.Run("rejects credentials and paths", func(t *testing.T) {
		for _, endpoint := range []string{
			"https://user:secret@storage.example",
			"https://storage.example/private",
		} {
			wantErrContaining(t, func(settings map[string]string, _ MapSecretResolver) {
				settings["S3_PRESIGN_ENDPOINT"] = endpoint
			}, "S3_PRESIGN_ENDPOINT must be an exact HTTP origin")
		}
	})
}

func TestRedisSecurityBoundary(t *testing.T) {
	t.Run("development permits explicit plaintext without authentication", func(t *testing.T) {
		cfg := mustLoad(t, func(settings map[string]string, secrets MapSecretResolver) {
			settings["APP_ENV"] = "development"
			settings["REDIS_TLS_ENABLED"] = "false"
			delete(secrets, "REDIS_PASSWORD")
		})
		if cfg.Redis().TLSEnabled() || !cfg.Redis().Password().IsEmpty() {
			t.Fatal("development Redis unexpectedly requires TLS or authentication")
		}
	})

	for _, environment := range []string{"staging", "production"} {
		t.Run(environment+" requires authentication", func(t *testing.T) {
			wantErrContaining(t, func(settings map[string]string, secrets MapSecretResolver) {
				settings["APP_ENV"] = environment
				delete(secrets, "REDIS_PASSWORD")
			}, "REDIS_PASSWORD is required outside development")
		})

		t.Run(environment+" requires TLS", func(t *testing.T) {
			wantErrContaining(t, func(settings map[string]string, _ MapSecretResolver) {
				settings["APP_ENV"] = environment
				settings["REDIS_TLS_ENABLED"] = "false"
			}, "REDIS_TLS_ENABLED must be true outside development")
		})
	}

	t.Run("ACL username requires password", func(t *testing.T) {
		wantErrContaining(t, func(settings map[string]string, secrets MapSecretResolver) {
			settings["APP_ENV"] = "development"
			secrets["REDIS_USERNAME"] = "gradex-acl"
			delete(secrets, "REDIS_PASSWORD")
		}, "REDIS_PASSWORD is required when REDIS_USERNAME is configured")
	})

	t.Run("TLS metadata requires TLS", func(t *testing.T) {
		wantErrContaining(t, func(settings map[string]string, _ MapSecretResolver) {
			settings["APP_ENV"] = "development"
			settings["REDIS_TLS_ENABLED"] = "false"
			settings["REDIS_TLS_SERVER_NAME"] = "redis.internal"
		}, "require REDIS_TLS_ENABLED=true")
	})

	t.Run("credentials in address are refused", func(t *testing.T) {
		wantErrContaining(t, func(settings map[string]string, _ MapSecretResolver) {
			settings["REDIS_ADDR"] = "rediss://gradex:secret@redis.internal:6379"
		}, "credential-free host:port")
	})

	t.Run("credentials remain redacted", func(t *testing.T) {
		cfg := mustLoad(t, func(_ map[string]string, secrets MapSecretResolver) {
			secrets["REDIS_USERNAME"] = "redis-username-canary"
			secrets["REDIS_PASSWORD"] = "redis-password-canary"
		})
		rendered := fmt.Sprintf("%v", cfg.Redis())
		if strings.Contains(rendered, "redis-username-canary") || strings.Contains(rendered, "redis-password-canary") {
			t.Fatal("Redis credentials were printable")
		}
	})
}

func TestMediaOperatingModeIsExplicitAndValidated(t *testing.T) {
	if cfg := mustLoad(t, nil); cfg.MediaOperatingMode() != MediaOperatingModeScanner {
		t.Fatalf("default mode = %q, want SCANNER", cfg.MediaOperatingMode())
	}
	for name, want := range map[string]MediaOperatingMode{
		"SCANNER":            MediaOperatingModeScanner,
		"ADMIN_CATALOGUE":    MediaOperatingModeAdminCatalogue,
		"TRUSTED_INSTRUCTOR": MediaOperatingModeTrustedInstructor,
	} {
		t.Run(name, func(t *testing.T) {
			cfg := mustLoad(t, func(settings map[string]string, _ MapSecretResolver) {
				settings["MEDIA_OPERATING_MODE"] = name
			})
			if cfg.MediaOperatingMode() != want {
				t.Fatalf("mode = %q, want %q", cfg.MediaOperatingMode(), want)
			}
		})
	}
	// An absent or empty value is the safe SCANNER default, consistent with
	// every other setting; only a present unknown value blocks startup.
	for _, unknown := range []string{"bypass", "trusted_instructor", "TRUSTED"} {
		wantErrContaining(t, func(settings map[string]string, _ MapSecretResolver) {
			settings["MEDIA_OPERATING_MODE"] = unknown
		}, "MEDIA_OPERATING_MODE must be SCANNER, ADMIN_CATALOGUE or TRUSTED_INSTRUCTOR")
	}
}

// The no-op scanner inspects nothing, so the only environment allowed to build
// it is development. Staging and production must refuse it outright rather than
// start with an unscanned media pipeline.
func TestDevelopmentNoOpScannerIsDevelopmentOnly(t *testing.T) {
	if cfg := mustLoad(t, func(map[string]string, MapSecretResolver) {}); cfg.MediaScannerMode() != MediaScannerModeUnavailable {
		t.Fatalf("default scanner mode = %q, want UNAVAILABLE", cfg.MediaScannerMode())
	}
	for _, environment := range []string{"staging", "production"} {
		t.Run(environment, func(t *testing.T) {
			wantErrContaining(t, func(s map[string]string, _ MapSecretResolver) {
				s["APP_ENV"] = environment
				s["MEDIA_SCANNER_MODE"] = "DEVELOPMENT_NO_OP"
			}, "MEDIA_SCANNER_MODE=DEVELOPMENT_NO_OP is refused")
		})
	}
	wantErrContaining(t, func(s map[string]string, _ MapSecretResolver) {
		s["MEDIA_SCANNER_MODE"] = "PERMISSIVE"
	}, "MEDIA_SCANNER_MODE must be UNAVAILABLE or DEVELOPMENT_NO_OP")
}

// Production origin rules. There is no environment in which an http production
// origin or a credentialed wildcard is the intended configuration, so neither
// may fall back to a permissive default.
func TestInvalidProductionOrigin(t *testing.T) {
	t.Run("http origin", func(t *testing.T) {
		wantErrContaining(t, func(s map[string]string, _ MapSecretResolver) {
			s["PUBLIC_ORIGIN"] = "http://gradex.example"
		}, "must be an https origin in production")
	})

	t.Run("missing origin", func(t *testing.T) {
		wantErrContaining(t, func(s map[string]string, _ MapSecretResolver) {
			delete(s, "PUBLIC_ORIGIN")
		}, "PUBLIC_ORIGIN is required")
	})

	t.Run("wildcard cors origin in production", func(t *testing.T) {
		wantErrContaining(t, func(s map[string]string, _ MapSecretResolver) {
			s["CORS_ALLOWED_ORIGINS"] = "*"
		}, "may not contain")
	})

	t.Run("http cors origin in production", func(t *testing.T) {
		wantErrContaining(t, func(s map[string]string, _ MapSecretResolver) {
			s["CORS_ALLOWED_ORIGINS"] = "http://gradex.example"
		}, "must be https in production")
	})

	// Refused in every environment, not just production.
	t.Run("credentialed wildcard in development", func(t *testing.T) {
		wantErrContaining(t, func(s map[string]string, _ MapSecretResolver) {
			s["APP_ENV"] = "development"
			s["CORS_ALLOWED_ORIGINS"] = "*"
			s["CORS_ALLOW_CREDENTIALS"] = "true"
		}, "CORS_ALLOW_CREDENTIALS is true")
	})
}

func TestMissingRequiredSecretsBlockStartup(t *testing.T) {
	for _, name := range []string{
		"DATABASE_URL", "S3_ACCESS_KEY", "S3_SECRET_KEY",
		"PLAYBACK_TOKEN_SECRET", "SESSION_CSRF_KEY",
		"ANONYMOUS_COOKIE_SIGNING_KEY", "ANONYMOUS_CSRF_KEY",
		"ADMISSION_LIMITER_HMAC_KEY",
	} {
		t.Run(name, func(t *testing.T) {
			wantErrContaining(t, func(_ map[string]string, sec MapSecretResolver) {
				delete(sec, name)
			}, name+" is required")
		})
	}
}

// The example placeholder is a non-empty string, so an emptiness check alone
// would accept it into production.
func TestPlaceholderPlaybackSecretRejectedInProduction(t *testing.T) {
	wantErrContaining(t, func(_ map[string]string, sec MapSecretResolver) {
		sec["PLAYBACK_TOKEN_SECRET"] = "changeme-generate-with-openssl-rand-hex-32"
	}, "still the example placeholder")
}

func TestInvalidTimeoutRelationships(t *testing.T) {
	for _, role := range []string{"STUDENT", "INSTRUCTOR", "ADMIN"} {
		t.Run(strings.ToLower(role)+" idle must precede absolute", func(t *testing.T) {
			wantErrContaining(t, func(s map[string]string, _ MapSecretResolver) {
				s[role+"_SESSION_IDLE_EXPIRY"] = "800h"
				s[role+"_SESSION_ABSOLUTE_EXPIRY"] = "800h"
			}, role+"_SESSION_IDLE_EXPIRY")
		})
	}

	t.Run("highest risk window cannot exceed general", func(t *testing.T) {
		wantErrContaining(t, func(s map[string]string, _ MapSecretResolver) {
			s["HIGHEST_RISK_RECENT_AUTH_WINDOW"] = "15m"
		}, "must not exceed GENERAL_RECENT_AUTH_WINDOW")
	})

	t.Run("general recent auth must fit shortest idle window", func(t *testing.T) {
		wantErrContaining(t, func(s map[string]string, _ MapSecretResolver) {
			s["GENERAL_RECENT_AUTH_WINDOW"] = "30m"
		}, "must be less than ADMIN_SESSION_IDLE_EXPIRY")
	})

	t.Run("non-positive duration", func(t *testing.T) {
		wantErrContaining(t, func(s map[string]string, _ MapSecretResolver) {
			s["HTTP_READ_TIMEOUT"] = "0s"
		}, "HTTP_READ_TIMEOUT must be positive")
	})

	t.Run("unparseable duration", func(t *testing.T) {
		wantErrContaining(t, func(s map[string]string, _ MapSecretResolver) {
			s["SHUTDOWN_TIMEOUT"] = "20 seconds"
		}, "must be a duration")
	})
}

func TestAuthFakeModeRefusedInProduction(t *testing.T) {
	wantErrContaining(t, func(s map[string]string, _ MapSecretResolver) {
		s["AUTH_FAKE_MODE"] = "true"
	}, "AUTH_FAKE_MODE must be false")
}

// Gated providers fail closed at the smallest safe scope: the process starts,
// the gated surface does not, and the reason is retrievable.
func TestDisabledGatedProviders(t *testing.T) {
	t.Run("tap disabled by default", func(t *testing.T) {
		cfg := mustLoad(t, nil)
		if cfg.Payments().Enabled() {
			t.Error("payments should be disabled when TAP_ENABLED is unset")
		}
		if !strings.Contains(cfg.Payments().Reason(), "TAP_ENABLED is false") {
			t.Errorf("unexpected reason %q", cfg.Payments().Reason())
		}
	})

	t.Run("tap enabled without secret disables payments but still starts", func(t *testing.T) {
		cfg := mustLoad(t, func(s map[string]string, _ MapSecretResolver) {
			s["TAP_ENABLED"] = "true"
			s["TAP_ENVIRONMENT"] = "live"
			s["TAP_ADAPTER_APPROVED"] = "true"
		})
		if cfg.Payments().Enabled() {
			t.Error("payments should be disabled when TAP_SECRET is absent")
		}
		if !strings.Contains(cfg.Payments().Reason(), "TAP_SECRET is absent") {
			t.Errorf("unexpected reason %q", cfg.Payments().Reason())
		}
	})

	// §11.2 lists live Tap without the approved LG-010 adapter as an invalid
	// configuration, not a disabled capability: a deployment that believes it
	// is taking live money must not come up quietly unable to.
	t.Run("live tap without approved adapter blocks startup", func(t *testing.T) {
		wantErrContaining(t, func(s map[string]string, sec MapSecretResolver) {
			s["TAP_ENABLED"] = "true"
			s["TAP_ENVIRONMENT"] = "live"
			sec["TAP_SECRET"] = "tap-key"
		}, "LG-010 authenticity contract is not approved")
	})

	t.Run("tap test environment refused in production", func(t *testing.T) {
		wantErrContaining(t, func(s map[string]string, sec MapSecretResolver) {
			s["TAP_ENABLED"] = "true"
			s["TAP_ENVIRONMENT"] = "test"
			sec["TAP_SECRET"] = "tap-key"
		}, "not permitted when APP_ENV=production")
	})

	t.Run("tap fully configured enables payments", func(t *testing.T) {
		cfg := mustLoad(t, func(s map[string]string, sec MapSecretResolver) {
			s["TAP_ENABLED"] = "true"
			s["TAP_ENVIRONMENT"] = "live"
			s["TAP_ADAPTER_APPROVED"] = "true"
			sec["TAP_SECRET"] = "tap-key"
		})
		if !cfg.Payments().Enabled() {
			t.Errorf("payments should be enabled, reason: %q", cfg.Payments().Reason())
		}
		if cfg.Payments().Reason() != "" {
			t.Error("an enabled capability should report no reason")
		}
	})

	t.Run("production email enabled without credentials blocks startup", func(t *testing.T) {
		wantErrContaining(t, func(s map[string]string, _ MapSecretResolver) {
			s["EMAIL_ENABLED"] = "true"
		}, "EMAIL_API_KEY is required")
	})
}

func TestTransactionalEmailConfiguration(t *testing.T) {
	configureResend := func(s map[string]string, sec MapSecretResolver) {
		s["EMAIL_ENABLED"] = "true"
		s["EMAIL_PROVIDER"] = "resend"
		s["EMAIL_FROM_ADDRESS"] = "notifications@gradex.kw"
		s["EMAIL_FROM_NAME"] = "Gradex"
		s["EMAIL_REPLY_TO"] = "support@gradex.kw"
		s["OUTBOX_PROTECTED_PAYLOAD_KEY_VERSION"] = "email-v1"
		sec["EMAIL_API_KEY"] = "resend-key-canary"
		sec["OUTBOX_PROTECTED_PAYLOAD_KEY"] = strings.Repeat("e", 32)
	}
	configureMailpit := func(s map[string]string, sec MapSecretResolver) {
		s["APP_ENV"] = "development"
		s["PUBLIC_ORIGIN"] = "http://localhost:3000"
		s["EMAIL_ENABLED"] = "true"
		s["EMAIL_PROVIDER"] = "mailpit"
		s["EMAIL_SMTP_ADDR"] = "127.0.0.1:1025"
		s["OUTBOX_PROTECTED_PAYLOAD_KEY_VERSION"] = "dev-v1"
		sec["OUTBOX_PROTECTED_PAYLOAD_KEY"] = strings.Repeat("m", 32)
	}

	t.Run("production Resend loads typed settings", func(t *testing.T) {
		cfg := mustLoad(t, configureResend)
		if !cfg.Email().Enabled() || cfg.Email().Provider() != EmailProviderResend {
			t.Fatalf("email settings = enabled %t provider %q reason %q", cfg.Email().Enabled(), cfg.Email().Provider(), cfg.Email().Reason())
		}
		if cfg.Email().FromAddress() != "notifications@gradex.kw" || cfg.Email().FromName() != "Gradex" || cfg.Email().ReplyTo() != "support@gradex.kw" {
			t.Fatalf("unexpected sender settings: address %q name %q reply-to %q", cfg.Email().FromAddress(), cfg.Email().FromName(), cfg.Email().ReplyTo())
		}
		if cfg.Email().APIKey().Expose() != "resend-key-canary" || cfg.Email().Timeout() != 10*time.Second {
			t.Fatal("Resend secret or timeout was not preserved")
		}
	})

	t.Run("production rejects fake", func(t *testing.T) {
		wantErrContaining(t, func(s map[string]string, sec MapSecretResolver) {
			configureResend(s, sec)
			s["EMAIL_PROVIDER"] = "fake"
		}, "EMAIL_PROVIDER=resend")
	})

	// A provider sandbox sender delivers, which is why it has to be refused
	// rather than merely discouraged: production mail would leave under the
	// provider's identity instead of the Gradex sending domain LG-018 requires.
	t.Run("production rejects a provider sandbox sender", func(t *testing.T) {
		for _, sender := range []string{"onboarding@resend.dev", "no-reply@mail.resend.dev", "no-reply@RESEND.DEV"} {
			wantErrContaining(t, func(s map[string]string, sec MapSecretResolver) {
				configureResend(s, sec)
				s["EMAIL_FROM_ADDRESS"] = sender
			}, "provider sandbox domain")
		}
	})

	t.Run("development may still use a provider sandbox sender", func(t *testing.T) {
		cfg := mustLoad(t, func(s map[string]string, sec MapSecretResolver) {
			configureResend(s, sec)
			s["APP_ENV"] = "development"
			s["PUBLIC_ORIGIN"] = "http://localhost:3000"
			s["EMAIL_FROM_ADDRESS"] = "onboarding@resend.dev"
		})
		if !cfg.Email().Enabled() {
			t.Fatalf("development sandbox sender was refused: %q", cfg.Email().Reason())
		}
	})

	t.Run("production requires an HTTPS public origin for links", func(t *testing.T) {
		wantErrContaining(t, func(s map[string]string, sec MapSecretResolver) {
			configureResend(s, sec)
			s["PUBLIC_ORIGIN"] = "http://gradex.kw"
		}, "HTTPS PUBLIC_ORIGIN")
	})

	t.Run("production worker rejects disabled email", func(t *testing.T) {
		wantErrContaining(t, func(s map[string]string, _ MapSecretResolver) {
			s["SERVICE_ROLE"] = "worker"
		}, "EMAIL_ENABLED must be true")
	})

	t.Run("development fake remains deterministic mode", func(t *testing.T) {
		cfg := mustLoad(t, func(s map[string]string, sec MapSecretResolver) {
			s["APP_ENV"] = "development"
			s["PUBLIC_ORIGIN"] = "http://localhost:3000"
			s["EMAIL_ENABLED"] = "true"
			s["EMAIL_PROVIDER"] = "fake"
			s["OUTBOX_PROTECTED_PAYLOAD_KEY_VERSION"] = "dev-v1"
			sec["OUTBOX_PROTECTED_PAYLOAD_KEY"] = strings.Repeat("d", 32)
		})
		if !cfg.Email().Enabled() || cfg.Email().Provider() != EmailProviderFake {
			t.Fatalf("development fake email not enabled: %q", cfg.Email().Reason())
		}
	})

	t.Run("development Mailpit loads a loopback SMTP address", func(t *testing.T) {
		cfg := mustLoad(t, configureMailpit)
		if !cfg.Email().Enabled() || cfg.Email().Provider() != EmailProviderMailpit || cfg.Email().SMTPAddress() != "127.0.0.1:1025" {
			t.Fatalf("development Mailpit email = enabled %t provider %q address %q reason %q", cfg.Email().Enabled(), cfg.Email().Provider(), cfg.Email().SMTPAddress(), cfg.Email().Reason())
		}
	})

	for _, environment := range []string{"staging", "production"} {
		t.Run(environment+" rejects Mailpit", func(t *testing.T) {
			wantErrContaining(t, func(s map[string]string, sec MapSecretResolver) {
				configureMailpit(s, sec)
				s["APP_ENV"] = environment
			}, "EMAIL_PROVIDER=mailpit is only allowed when APP_ENV=development")
		})
	}

	t.Run("production rejects disabled Mailpit", func(t *testing.T) {
		wantErrContaining(t, func(s map[string]string, _ MapSecretResolver) {
			s["EMAIL_PROVIDER"] = "mailpit"
		}, "EMAIL_PROVIDER=mailpit is only allowed when APP_ENV=development")
	})

	t.Run("Mailpit refuses a non-loopback SMTP address", func(t *testing.T) {
		wantErrContaining(t, func(s map[string]string, sec MapSecretResolver) {
			configureMailpit(s, sec)
			s["EMAIL_SMTP_ADDR"] = "192.0.2.7:1025"
		}, "loopback IP address")
	})

	t.Run("malformed timeout fails closed", func(t *testing.T) {
		wantErrContaining(t, func(s map[string]string, sec MapSecretResolver) {
			configureResend(s, sec)
			s["EMAIL_PROVIDER_TIMEOUT"] = "31s"
		}, "between 1s and 30s")
	})
}

func TestStudentRegistrationIsDisabledByDefault(t *testing.T) {
	cfg := mustLoad(t, nil)
	if cfg.Admission().Enabled() {
		t.Fatal("Student registration must be disabled until its security and policy dependencies are configured")
	}
	if !strings.Contains(cfg.Admission().Reason(), "STUDENT_REGISTRATION_ENABLED is false") {
		t.Errorf("unexpected reason %q", cfg.Admission().Reason())
	}
}

func TestProductionRejectsCredentialScreeningFixturesWhenAdmissionIsDisabled(t *testing.T) {
	wantErrContaining(t, func(s map[string]string, _ MapSecretResolver) {
		s["STUDENT_REGISTRATION_ENABLED"] = "false"
		s["PASSWORD_SCREEN_MODE"] = "deterministic"
	}, "deterministic PASSWORD_SCREEN_MODE")

	wantErrContaining(t, func(s map[string]string, _ MapSecretResolver) {
		s["STUDENT_REGISTRATION_ENABLED"] = "false"
		s["PASSWORD_SCREEN_MODE"] = "adapter"
	}, "COMPROMISED_PASSWORD_ADAPTER_APPROVED")
}

func TestDeterministicCredentialScreeningIsDevelopmentOnly(t *testing.T) {
	for _, environment := range []string{"staging", "production"} {
		t.Run(environment, func(t *testing.T) {
			wantErrContaining(t, func(s map[string]string, _ MapSecretResolver) {
				s["APP_ENV"] = environment
				s["STUDENT_REGISTRATION_ENABLED"] = "false"
				s["PASSWORD_SCREEN_MODE"] = "deterministic"
			}, "deterministic PASSWORD_SCREEN_MODE is permitted only in development")
		})
	}
}

func TestDevelopmentAdmissionConfigurationLoadsExplicitFixtures(t *testing.T) {
	cfg := mustLoad(t, func(s map[string]string, sec MapSecretResolver) {
		s["APP_ENV"] = "development"
		s["STUDENT_REGISTRATION_ENABLED"] = "true"
		s["REGISTRATION_POLICY_SET_ID"] = "dev-registration-v1"
		s["PASSWORD_SCREEN_MODE"] = "deterministic"
		s["OUTBOX_PROTECTED_PAYLOAD_KEY_VERSION"] = "dev-v1"
		sec["ANONYMOUS_COOKIE_SIGNING_KEY"] = strings.Repeat("a", 32)
		sec["ANONYMOUS_CSRF_KEY"] = strings.Repeat("b", 32)
		sec["IDENTITY_OTP_PEPPER"] = strings.Repeat("o", 32)
		sec["ADMISSION_LIMITER_HMAC_KEY"] = strings.Repeat("c", 32)
		sec["OUTBOX_PROTECTED_PAYLOAD_KEY"] = strings.Repeat("d", 32)
	})

	admission := cfg.Admission()
	if !admission.Enabled() {
		t.Fatalf("admission disabled: %s", admission.Reason())
	}
	if admission.PolicySetID() != "dev-registration-v1" {
		t.Errorf("PolicySetID = %q", admission.PolicySetID())
	}
	if admission.PasswordScreenMode() != PasswordScreenDeterministic {
		t.Errorf("PasswordScreenMode = %q", admission.PasswordScreenMode())
	}
	if admission.AnonymousSessionTTL() != 30*time.Minute {
		t.Errorf("AnonymousSessionTTL = %s, want 30m", admission.AnonymousSessionTTL())
	}
	if admission.VerificationTokenTTL() != 24*time.Hour {
		t.Errorf("VerificationTokenTTL = %s, want 24h", admission.VerificationTokenTTL())
	}
	if admission.RateLimitTimeout() != 100*time.Millisecond {
		t.Errorf("RateLimitTimeout = %s, want 100ms", admission.RateLimitTimeout())
	}
	if admission.CompromisedPasswordTimeout() != 3*time.Second {
		t.Errorf("CompromisedPasswordTimeout = %s, want 3s", admission.CompromisedPasswordTimeout())
	}
	for name, secret := range map[string]Secret{
		"anonymous cookie":  admission.AnonymousCookieSigningKey(),
		"anonymous CSRF":    admission.AnonymousCSRFKey(),
		"limiter HMAC":      admission.LimiterHMACKey(),
		"protected payload": admission.ProtectedPayloadKey(),
	} {
		if secret.IsEmpty() {
			t.Errorf("%s secret was not resolved", name)
		}
	}
}

func TestEnabledAdmissionRequiresEveryFailClosedDependency(t *testing.T) {
	base := func(s map[string]string, sec MapSecretResolver) {
		s["APP_ENV"] = "development"
		s["STUDENT_REGISTRATION_ENABLED"] = "true"
		s["REGISTRATION_POLICY_SET_ID"] = "dev-registration-v1"
		s["PASSWORD_SCREEN_MODE"] = "deterministic"
		s["OUTBOX_PROTECTED_PAYLOAD_KEY_VERSION"] = "dev-v1"
		sec["ANONYMOUS_COOKIE_SIGNING_KEY"] = strings.Repeat("a", 32)
		sec["ANONYMOUS_CSRF_KEY"] = strings.Repeat("b", 32)
		sec["IDENTITY_OTP_PEPPER"] = strings.Repeat("o", 32)
		sec["ADMISSION_LIMITER_HMAC_KEY"] = strings.Repeat("c", 32)
		sec["OUTBOX_PROTECTED_PAYLOAD_KEY"] = strings.Repeat("d", 32)
	}

	tests := map[string]struct {
		mutate func(map[string]string, MapSecretResolver)
		want   string
	}{
		"policy set": {
			mutate: func(s map[string]string, _ MapSecretResolver) { delete(s, "REGISTRATION_POLICY_SET_ID") },
			want:   "REGISTRATION_POLICY_SET_ID is required",
		},
		"screen mode": {
			mutate: func(s map[string]string, _ MapSecretResolver) { s["PASSWORD_SCREEN_MODE"] = "unavailable" },
			want:   "PASSWORD_SCREEN_MODE",
		},
		"payload key version": {
			mutate: func(s map[string]string, _ MapSecretResolver) { delete(s, "OUTBOX_PROTECTED_PAYLOAD_KEY_VERSION") },
			want:   "OUTBOX_PROTECTED_PAYLOAD_KEY_VERSION is required",
		},
		"public origin": {
			mutate: func(s map[string]string, _ MapSecretResolver) { delete(s, "PUBLIC_ORIGIN") },
			want:   "PUBLIC_ORIGIN must be an exact HTTP origin",
		},
		"anonymous cookie key": {
			mutate: func(_ map[string]string, sec MapSecretResolver) { delete(sec, "ANONYMOUS_COOKIE_SIGNING_KEY") },
			want:   "ANONYMOUS_COOKIE_SIGNING_KEY is required",
		},
		"anonymous CSRF key": {
			mutate: func(_ map[string]string, sec MapSecretResolver) { delete(sec, "ANONYMOUS_CSRF_KEY") },
			want:   "ANONYMOUS_CSRF_KEY is required",
		},
		"limiter HMAC key": {
			mutate: func(_ map[string]string, sec MapSecretResolver) { delete(sec, "ADMISSION_LIMITER_HMAC_KEY") },
			want:   "ADMISSION_LIMITER_HMAC_KEY is required",
		},
		"protected payload key": {
			mutate: func(_ map[string]string, sec MapSecretResolver) { delete(sec, "OUTBOX_PROTECTED_PAYLOAD_KEY") },
			want:   "OUTBOX_PROTECTED_PAYLOAD_KEY is required",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			wantErrContaining(t, func(s map[string]string, sec MapSecretResolver) {
				base(s, sec)
				tt.mutate(s, sec)
			}, tt.want)
		})
	}
}

func TestProductionAdmissionRequiresApprovedPolicyAndPasswordAdapter(t *testing.T) {
	configure := func(s map[string]string, sec MapSecretResolver) {
		s["STUDENT_REGISTRATION_ENABLED"] = "true"
		s["REGISTRATION_POLICY_SET_ID"] = "registration-v1"
		s["REGISTRATION_POLICY_APPROVED"] = "true"
		s["PASSWORD_SCREEN_MODE"] = "adapter"
		s["COMPROMISED_PASSWORD_ADAPTER_APPROVED"] = "true"
		s["OUTBOX_PROTECTED_PAYLOAD_KEY_VERSION"] = "prod-v1"
		sec["ANONYMOUS_COOKIE_SIGNING_KEY"] = strings.Repeat("a", 32)
		sec["ANONYMOUS_CSRF_KEY"] = strings.Repeat("b", 32)
		sec["IDENTITY_OTP_PEPPER"] = strings.Repeat("o", 32)
		sec["ADMISSION_LIMITER_HMAC_KEY"] = strings.Repeat("c", 32)
		sec["OUTBOX_PROTECTED_PAYLOAD_KEY"] = strings.Repeat("d", 32)
	}

	t.Run("policy approval", func(t *testing.T) {
		wantErrContaining(t, func(s map[string]string, sec MapSecretResolver) {
			configure(s, sec)
			s["REGISTRATION_POLICY_APPROVED"] = "false"
		}, "REGISTRATION_POLICY_APPROVED=true")
	})
	t.Run("password adapter approval", func(t *testing.T) {
		wantErrContaining(t, func(s map[string]string, sec MapSecretResolver) {
			configure(s, sec)
			s["COMPROMISED_PASSWORD_ADAPTER_APPROVED"] = "false"
		}, "COMPROMISED_PASSWORD_ADAPTER_APPROVED=true")
	})
	t.Run("deterministic fixture prohibited", func(t *testing.T) {
		wantErrContaining(t, func(s map[string]string, sec MapSecretResolver) {
			configure(s, sec)
			s["PASSWORD_SCREEN_MODE"] = "deterministic"
		}, "deterministic PASSWORD_SCREEN_MODE")
	})
}

func TestAdmissionDurationsMustBePositive(t *testing.T) {
	for _, key := range []string{
		"ANONYMOUS_SESSION_TTL",
		"VERIFICATION_TOKEN_TTL",
		"ADMISSION_RATE_LIMIT_TIMEOUT",
		"COMPROMISED_PASSWORD_TIMEOUT",
	} {
		t.Run(key, func(t *testing.T) {
			wantErrContaining(t, func(s map[string]string, _ MapSecretResolver) {
				s[key] = "0s"
			}, key+" must be positive")
		})
	}
}

func TestMediaProcessingTimeoutMustBePositive(t *testing.T) {
	wantErrContaining(t, func(s map[string]string, _ MapSecretResolver) {
		s["MEDIA_PROCESSING_TIMEOUT"] = "0s"
	}, "MEDIA_PROCESSING_TIMEOUT must be positive")
}

// A renamed key must fail loudly. Silently falling back to the default would
// drop an operator's deliberate setting with no signal.
func TestRetiredKeysAreRejected(t *testing.T) {
	for old, replacement := range map[string]string{
		"UPLOAD_URL_EXPIRY_MINUTES":   "UPLOAD_URL_EXPIRY",
		"PLAYBACK_URL_EXPIRY_MINUTES": "PLAYBACK_URL_EXPIRY",
		"SESSION_IDLE_EXPIRY":         "role-specific",
		"SESSION_ABSOLUTE_EXPIRY":     "role-specific",
		"RECENT_AUTH_WINDOW":          "GENERAL_RECENT_AUTH_WINDOW",
	} {
		t.Run(old, func(t *testing.T) {
			wantErrContaining(t, func(s map[string]string, _ MapSecretResolver) {
				s[old] = "15"
			}, replacement)
		})
	}
}

func TestInvalidEnvironmentRejected(t *testing.T) {
	wantErrContaining(t, func(s map[string]string, _ MapSecretResolver) {
		s["APP_ENV"] = "prod"
	}, "APP_ENV must be one of")
}

// Load reports every fault at once so an operator fixes one deployment rather
// than discovering faults one restart at a time.
func TestLoadReportsAllFaultsTogether(t *testing.T) {
	_, err := loadWith(t, func(s map[string]string, sec MapSecretResolver) {
		s["PUBLIC_ORIGIN"] = "http://gradex.example"
		s["STUDENT_SESSION_IDLE_EXPIRY"] = "800h"
		delete(sec, "DATABASE_URL")
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"https origin", "STUDENT_SESSION_ABSOLUTE_EXPIRY", "DATABASE_URL is required"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("aggregated error missing %q: %v", want, err)
		}
	}
}

// The returned Config is immutable: the only reference a caller gets to
// internal slice state is a copy.
func TestCORSOriginsAreCopiedOut(t *testing.T) {
	cfg := mustLoad(t, func(s map[string]string, _ MapSecretResolver) {
		s["CORS_ALLOWED_ORIGINS"] = "https://a.example, https://b.example"
	})
	got := cfg.CORSAllowedOrigins()
	if len(got) != 2 {
		t.Fatalf("expected 2 origins, got %v", got)
	}
	got[0] = "https://evil.example"
	if cfg.CORSAllowedOrigins()[0] != "https://a.example" {
		t.Error("mutating the returned slice changed the runtime configuration")
	}
}

func TestDefaultsApplyOutsideProduction(t *testing.T) {
	cfg := mustLoad(t, func(s map[string]string, secrets MapSecretResolver) {
		s["APP_ENV"] = "development"
		delete(s, "PUBLIC_ORIGIN")
		delete(s, "CORS_ALLOWED_ORIGINS")
		delete(s, "CORS_ALLOW_CREDENTIALS")
		delete(secrets, "SESSION_CSRF_KEY")
	})
	if cfg.HTTPReadTimeout() != 15*time.Second {
		t.Errorf("HTTPReadTimeout = %s, want 15s", cfg.HTTPReadTimeout())
	}
	if cfg.Port() != "8080" {
		t.Errorf("Port = %q, want 8080", cfg.Port())
	}
	// The CORS default denies rather than permits.
	if len(cfg.CORSAllowedOrigins()) != 0 {
		t.Errorf("expected no default CORS origins, got %v", cfg.CORSAllowedOrigins())
	}
	if cfg.CORSAllowCredentials() {
		t.Error("CORS credentials should default to false")
	}
}

func TestPublicLegalIdentityConfigurationLoadsAndIsImmutable(t *testing.T) {
	cfg := mustLoad(t, nil)
	legal := cfg.Legal()
	if legal.IdentityMode() != LegalIdentityPublic || legal.OperatorName() != "Gradex Courses" ||
		legal.RegistrationNumber() != "KWT-REAL-123" || legal.RegisteredAddress() != "Kuwait City, Kuwait" ||
		legal.PrivacyEmail() != "privacy@gradex.example" || legal.SupportEmail() != "support@gradex.example" ||
		legal.SecurityEmail() != "security@gradex.example" {
		t.Fatalf("unexpected legal configuration: %+v", legal)
	}
}

func TestNonDevelopmentLegalIdentityRequiresEveryApprovedField(t *testing.T) {
	for _, key := range []string{
		"LEGAL_OPERATOR_NAME", "LEGAL_REGISTRATION_NUMBER", "LEGAL_REGISTERED_ADDRESS",
		"PRIVACY_EMAIL", "SUPPORT_EMAIL", "SECURITY_EMAIL",
	} {
		t.Run(key, func(t *testing.T) {
			wantErrContaining(t, func(settings map[string]string, _ MapSecretResolver) {
				delete(settings, key)
			}, key+" is required outside development")
		})
	}
	for _, key := range []string{"PRIVACY_EMAIL", "SUPPORT_EMAIL", "SECURITY_EMAIL"} {
		t.Run(key+" invalid", func(t *testing.T) {
			wantErrContaining(t, func(settings map[string]string, _ MapSecretResolver) {
				settings[key] = "not-an-email"
			}, key+" must be a valid email address")
		})
	}
}

func TestPublicLegalIdentityRejectsEitherStagingSentinel(t *testing.T) {
	for key, value := range map[string]string{
		"LEGAL_REGISTRATION_NUMBER": StagingLegalRegistrationNumber,
		"LEGAL_REGISTERED_ADDRESS":  StagingLegalRegisteredAddress,
	} {
		t.Run(key, func(t *testing.T) {
			wantErrContaining(t, func(settings map[string]string, _ MapSecretResolver) {
				settings[key] = value
			}, "public legal identity rejects controlled-staging sentinel values")
		})
	}
}

func TestControlledStagingLegalIdentityAcceptsOnlyApprovedContexts(t *testing.T) {
	for name, testCase := range map[string]struct {
		environment Environment
		origin      string
	}{
		"local production-like": {EnvProduction, ControlledStagingLocalOrigin},
		"LG-019 public staging": {EnvStaging, ControlledStagingLG019Origin},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := mustLoad(t, controlledStagingLegalSettings(testCase.environment, testCase.origin, nil))
			if cfg.Legal().IdentityMode() != LegalIdentityControlledStaging {
				t.Fatalf("legal identity mode = %q", cfg.Legal().IdentityMode())
			}
		})
	}

	for name, testCase := range map[string]struct {
		environment Environment
		origin      string
		mutate      func(map[string]string)
	}{
		"production with LG-019 origin": {EnvProduction, ControlledStagingLG019Origin, nil},
		"staging with local origin":     {EnvStaging, ControlledStagingLocalOrigin, nil},
		"arbitrary staging origin":      {EnvStaging, "https://other.gradex.network", nil},
		"HTTP staging origin":           {EnvStaging, "http://staging.gradex.network", nil},
		"wrong registration sentinel": {EnvStaging, ControlledStagingLG019Origin, func(settings map[string]string) {
			settings["LEGAL_REGISTRATION_NUMBER"] = "KWT-REAL-123"
		}},
		"wrong address sentinel": {EnvStaging, ControlledStagingLG019Origin, func(settings map[string]string) {
			settings["LEGAL_REGISTERED_ADDRESS"] = "Kuwait City, Kuwait"
		}},
	} {
		t.Run(name, func(t *testing.T) {
			wantErrContaining(t, controlledStagingLegalSettings(testCase.environment, testCase.origin, testCase.mutate),
				"controlled-staging legal identity requires an exact approved disposable context and sentinel values")
		})
	}
}

func controlledStagingLegalSettings(
	environment Environment,
	origin string,
	mutate func(map[string]string),
) func(map[string]string, MapSecretResolver) {
	return func(settings map[string]string, _ MapSecretResolver) {
		settings["APP_ENV"] = string(environment)
		settings["PUBLIC_ORIGIN"] = origin
		settings["CORS_ALLOWED_ORIGINS"] = origin
		settings["LEGAL_IDENTITY_MODE"] = string(LegalIdentityControlledStaging)
		settings["LEGAL_REGISTRATION_NUMBER"] = StagingLegalRegistrationNumber
		settings["LEGAL_REGISTERED_ADDRESS"] = StagingLegalRegisteredAddress
		if mutate != nil {
			mutate(settings)
		}
	}
}

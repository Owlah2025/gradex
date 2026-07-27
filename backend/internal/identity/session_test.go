package identity

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/argon2"
)

// The hash must carry the parameters it was made with, so raising them later
// upgrades credentials on next use instead of invalidating them.
func TestArgon2idParametersAreRFC9106SecondOption(t *testing.T) {
	hash, err := HashPassword(goodPassword)
	if err != nil {
		t.Fatalf("hashing: %v", err)
	}

	want := fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$", argon2.Version, 64*1024, 3, 4)
	if !strings.HasPrefix(hash.Expose(), want) {
		t.Fatalf("hash prefix = %q, want %q", hash.Expose(), want)
	}
}

func TestLastPrimaryAuthenticationPrefersReauthentication(t *testing.T) {
	base := time.Date(2026, 7, 29, 9, 0, 0, 0, time.UTC)
	reauth := base.Add(2 * time.Hour)

	s := Session{AuthenticatedAt: base, ReauthenticatedAt: &reauth}
	if got := s.LastPrimaryAuthentication(); !got.Equal(reauth) {
		t.Fatalf("got %s, want the reauthentication time %s", got, reauth)
	}

	// Activity is not authentication. A session clicking around for hours has
	// recent activity and stale authentication.
	s = Session{AuthenticatedAt: base, LastActivityAt: base.Add(10 * time.Hour)}
	if got := s.LastPrimaryAuthentication(); !got.Equal(base) {
		t.Fatalf("activity was treated as authentication: got %s, want %s", got, base)
	}
}

func TestPasswordChangeRequiresRecentAuthentication(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	window := 15 * time.Minute

	t.Run("inside the window", func(t *testing.T) {
		s := Session{AuthenticatedAt: now.Add(-5 * time.Minute)}
		if err := CheckRecentAuthentication(s, window, now); err != nil {
			t.Fatalf("a recent session was refused: %v", err)
		}
	})

	t.Run("outside the window", func(t *testing.T) {
		s := Session{AuthenticatedAt: now.Add(-60 * time.Minute)}
		err := CheckRecentAuthentication(s, window, now)
		if !errors.Is(err, ErrRecentAuthRequired) {
			t.Fatalf("err = %v, want ErrRecentAuthRequired", err)
		}
	})

	t.Run("recent activity does not substitute", func(t *testing.T) {
		s := Session{
			AuthenticatedAt: now.Add(-6 * time.Hour),
			LastActivityAt:  now.Add(-1 * time.Second),
		}
		if err := CheckRecentAuthentication(s, window, now); !errors.Is(err, ErrRecentAuthRequired) {
			t.Fatalf("recent activity satisfied a recent-authentication requirement: %v", err)
		}
	})

	// A misconfigured window must not read as "no requirement".
	t.Run("non-positive window fails closed", func(t *testing.T) {
		s := Session{AuthenticatedAt: now}
		if err := CheckRecentAuthentication(s, 0, now); !errors.Is(err, ErrRecentAuthRequired) {
			t.Fatalf("a zero window was treated as no requirement: %v", err)
		}
	})

}

func TestPasswordChangeKindsRequireCurrentPasswordAsDesigned(t *testing.T) {
	if BootstrapMandatoryChange.RequiresCurrentPassword() {
		t.Error("the bootstrap mandatory change demanded the current password")
	}
	if !VoluntaryChange.RequiresCurrentPassword() {
		t.Error("a voluntary change did not require the current password")
	}
}

func TestSessionUsability(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	live := func() Session {
		return Session{
			State:             SessionActive,
			AdmittedEpoch:     3,
			IdleExpiresAt:     now.Add(time.Hour),
			AbsoluteExpiresAt: now.Add(24 * time.Hour),
		}
	}

	if err := live().Usable(3, now); err != nil {
		t.Fatalf("a live session was unusable: %v", err)
	}

	// The epoch check is what makes suspension and logout-all immediate.
	if err := live().Usable(4, now); !errors.Is(err, ErrSessionNotUsable) {
		t.Errorf("a session admitted under a superseded epoch was usable: %v", err)
	}

	revoked := live()
	revoked.State = SessionRevoked
	if err := revoked.Usable(3, now); !errors.Is(err, ErrSessionNotUsable) {
		t.Errorf("a revoked session was usable: %v", err)
	}

	idle := live()
	idle.IdleExpiresAt = now.Add(-time.Second)
	if err := idle.Usable(3, now); !errors.Is(err, ErrSessionNotUsable) {
		t.Errorf("an idle-expired session was usable: %v", err)
	}

	absolute := live()
	absolute.AbsoluteExpiresAt = now.Add(-time.Second)
	if err := absolute.Usable(3, now); !errors.Is(err, ErrSessionNotUsable) {
		t.Errorf("an absolutely expired session was usable: %v", err)
	}
}

func TestSupersededCredentialUseClassification(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	replacedAt := now.Add(-2 * time.Second)
	tests := map[string]struct {
		use  SupersededCredentialUse
		want StaleUseDecision
	}{
		"first immediate read is replacement": {
			use: SupersededCredentialUse{
				Kind: UseReadOnly, SupersededAt: replacedAt, PriorStaleUseCount: 0,
			},
			want: StaleUseRejectReplaced,
		},
		"second immediate read confirms reuse": {
			use: SupersededCredentialUse{
				Kind: UseReadOnly, SupersededAt: replacedAt, PriorStaleUseCount: 1,
			},
			want: StaleUseRevokeFamily,
		},
		"late read confirms reuse": {
			use: SupersededCredentialUse{
				Kind: UseReadOnly, SupersededAt: now.Add(-6 * time.Second),
			},
			want: StaleUseRevokeFamily,
		},
		"read at window boundary is replacement": {
			use: SupersededCredentialUse{
				Kind: UseReadOnly, SupersededAt: now.Add(-5 * time.Second),
			},
			want: StaleUseRejectReplaced,
		},
		"future supersession evidence fails closed": {
			use: SupersededCredentialUse{
				Kind: UseReadOnly, SupersededAt: now.Add(time.Second),
			},
			want: StaleUseRevokeFamily,
		},
		"renewal confirms reuse": {
			use:  SupersededCredentialUse{Kind: UseRenewal, SupersededAt: replacedAt},
			want: StaleUseRevokeFamily,
		},
		"mutation confirms reuse": {
			use:  SupersededCredentialUse{Kind: UseStateChanging, SupersededAt: replacedAt},
			want: StaleUseRevokeFamily,
		},
		"security-sensitive read confirms reuse": {
			use:  SupersededCredentialUse{Kind: UseSecuritySensitive, SupersededAt: replacedAt},
			want: StaleUseRevokeFamily,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if got := ClassifySupersededCredentialUse(test.use, now, 5*time.Second); got != test.want {
				t.Fatalf("decision = %q, want %q", got, test.want)
			}
		})
	}
}

// A leak of the sessions tables must not yield usable cookies, and two families
// must never collide.
func TestSessionCredentialsAreRandomAndStoredOnlyAsDigests(t *testing.T) {
	first, err := NewSessionCredential()
	if err != nil {
		t.Fatalf("minting: %v", err)
	}
	second, err := NewSessionCredential()
	if err != nil {
		t.Fatalf("minting: %v", err)
	}

	if first.Credential.Expose() == second.Credential.Expose() {
		t.Fatal("two credentials were identical")
	}
	if first.CSRFToken.Expose() == first.Credential.Expose() {
		t.Fatal("the CSRF token equals the session credential")
	}

	if first.CredentialDigest == first.Credential.Expose() {
		t.Fatal("the stored digest is the credential itself")
	}
	if first.CredentialDigest != DigestToken(first.Credential.Expose()) {
		t.Fatal("the digest does not match the credential it was derived from")
	}
	if first.CSRFDigest == first.CSRFToken.Expose() {
		t.Fatal("the stored CSRF digest is the token itself")
	}

	// The plaintexts are Secrets so an accidental log renders the marker.
	for _, verb := range []string{"%s", "%v", "%q", "%#v"} {
		rendered := fmt.Sprintf(verb, first.Credential)
		if strings.Contains(rendered, first.Credential.Expose()) {
			t.Fatalf("formatting the credential with %s leaked it", verb)
		}
	}
}

func TestGenerationBoundCSRFCanBeRehydratedWithoutDatabasePlaintext(t *testing.T) {
	key := bytes.Repeat([]byte{0x71}, 32)
	issued, err := NewSessionCredentialForGeneration(key, "session-1", 1)
	if err != nil {
		t.Fatalf("minting generation: %v", err)
	}
	rehydrated, err := DeriveSessionCSRFToken(
		key, "session-1", 1, issued.CredentialDigest,
	)
	if err != nil {
		t.Fatalf("rehydrating CSRF: %v", err)
	}
	if rehydrated.Expose() != issued.CSRFToken.Expose() {
		t.Fatal("rehydrated CSRF token differs from the issued value")
	}
	if DigestToken(rehydrated.Expose()) != issued.CSRFDigest {
		t.Fatal("rehydrated CSRF token does not match the stored digest")
	}

	next, err := DeriveSessionCSRFToken(key, "session-1", 2, issued.CredentialDigest)
	if err != nil {
		t.Fatalf("deriving next generation: %v", err)
	}
	if next.Expose() == rehydrated.Expose() {
		t.Fatal("generation rotation did not rotate CSRF")
	}

	if _, err := NewSessionCredentialForGeneration(
		bytes.Repeat([]byte{0x71}, 31), "session-1", 1,
	); err == nil {
		t.Fatal("short session CSRF key was accepted")
	}
}

package identity

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// A compliant password used across tests: 15+ runes, no common substring.
const goodPassword = "correct-horse-battery-staple-7"

func TestValidatePasswordLengthBounds(t *testing.T) {
	tests := []struct {
		name     string
		password string
		wantErr  bool
	}{
		{"below minimum", strings.Repeat("x", MinPasswordRunes-1), true},
		{"at minimum", strings.Repeat("x", MinPasswordRunes), false},
		{"at maximum", strings.Repeat("x", MaxPasswordRunes), false},
		{"above maximum", strings.Repeat("x", MaxPasswordRunes+1), true},
		{"empty", "", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidatePassword(tc.password, nil)
			if tc.wantErr && err == nil {
				t.Fatalf("expected a policy error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
		})
	}
}

// The policy is specified in Unicode characters. A byte-length check would
// reject a compliant Arabic passphrase, since each character costs two bytes —
// on an Arabic-default platform that is a real rejection, not a hypothetical.
func TestValidatePasswordCountsRunesNotBytes(t *testing.T) {
	// 16 Arabic characters: 16 runes, 32 bytes. Above the 15-rune minimum.
	arabic := "كلمةسرطويلةجدااا"

	if got := len([]rune(arabic)); got < MinPasswordRunes {
		t.Fatalf("test fixture is only %d runes; it must exceed the %d-rune minimum", got, MinPasswordRunes)
	}
	if len(arabic) <= MinPasswordRunes {
		t.Fatalf("test fixture is %d bytes; it must be multi-byte for this test to mean anything", len(arabic))
	}

	if err := ValidatePassword(arabic, nil); err != nil {
		t.Fatalf("a %d-rune Arabic passphrase was rejected: %v", len([]rune(arabic)), err)
	}
}

func TestValidatePasswordRejectsCommonValues(t *testing.T) {
	// Each is long enough to clear the length rule and still a bad choice —
	// which is the point of checking containment rather than equality.
	for _, password := range []string{
		"mypasswordisgreat",
		"Password123456789",
		"qwertyuiopasdfghjkl",
		"gradexadministrator",
	} {
		t.Run(password, func(t *testing.T) {
			err := ValidatePassword(password, nil)
			if err == nil {
				t.Fatalf("expected rejection for %q", password)
			}
			if !errors.Is(err, ErrPasswordPolicy) {
				t.Fatalf("expected ErrPasswordPolicy, got %v", err)
			}
		})
	}
}

// A policy error is frequently what lands in a deploy log. It must describe the
// rule, never the input.
func TestValidatePasswordErrorNeverContainsThePassword(t *testing.T) {
	secretish := "hunter2hunter2hunter2password"
	err := ValidatePassword(secretish, nil)
	if err == nil {
		t.Fatal("expected this password to be rejected")
	}
	if strings.Contains(err.Error(), secretish) || strings.Contains(err.Error(), "hunter2") {
		t.Fatalf("policy error leaked the password: %q", err.Error())
	}
}

type stubChecker struct {
	compromised bool
	err         error
}

func (s stubChecker) IsCompromised(string) (bool, error) { return s.compromised, s.err }

func TestValidatePasswordConsultsCompromisedChecker(t *testing.T) {
	err := ValidatePassword(goodPassword, stubChecker{compromised: true})
	if !errors.Is(err, ErrPasswordPolicy) {
		t.Fatalf("expected a policy rejection, got %v", err)
	}
}

// A checker that cannot answer must not be read as answering "not
// compromised".
func TestValidatePasswordFailsClosedWhenCheckerErrors(t *testing.T) {
	err := ValidatePassword(goodPassword, stubChecker{err: errors.New("provider unreachable")})
	if err == nil {
		t.Fatal("expected the password to be rejected when the checker failed")
	}
}

func TestHashPasswordProducesVerifiableArgon2id(t *testing.T) {
	hash, err := HashPassword(goodPassword)
	if err != nil {
		t.Fatalf("hashing: %v", err)
	}

	// The database CHECK constraint recognizes this prefix; a hash without it
	// would be rejected at insert time.
	if !strings.HasPrefix(hash.Expose(), "$argon2id$") {
		t.Fatalf("hash is not in Argon2id PHC format: %q", hash.Expose())
	}

	ok, err := VerifyPassword(hash.Expose(), goodPassword)
	if err != nil {
		t.Fatalf("verifying: %v", err)
	}
	if !ok {
		t.Fatal("the correct password did not verify against its own hash")
	}

	ok, err = VerifyPassword(hash.Expose(), goodPassword+"x")
	if err != nil {
		t.Fatalf("verifying a wrong password should not error: %v", err)
	}
	if ok {
		t.Fatal("a wrong password verified")
	}
}

// The salt is what makes two identical passwords store differently. Without
// this, a database leak reveals which accounts share a password.
func TestHashPasswordIsSaltedPerCall(t *testing.T) {
	first, err := HashPassword(goodPassword)
	if err != nil {
		t.Fatalf("hashing: %v", err)
	}
	second, err := HashPassword(goodPassword)
	if err != nil {
		t.Fatalf("hashing: %v", err)
	}
	if first.Expose() == second.Expose() {
		t.Fatal("the same password hashed identically twice; the salt is not random")
	}
}

// Bootstrap close condition 2, at the type level: the hash is returned as a
// config.Secret, so the ordinary ways a value reaches a log render the
// redaction marker instead of credential material.
func TestHashPasswordReturnsRedactingSecret(t *testing.T) {
	hash, err := HashPassword(goodPassword)
	if err != nil {
		t.Fatalf("hashing: %v", err)
	}

	for _, verb := range []string{"%s", "%v", "%q", "%#v"} {
		rendered := fmt.Sprintf(verb, hash)
		if strings.Contains(rendered, "argon2id") {
			t.Fatalf("formatting the hash with %s leaked it: %q", verb, rendered)
		}
	}
}

func TestVerifyPasswordRejectsMalformedStoredHash(t *testing.T) {
	for _, stored := range []string{
		"",
		"plaintext",
		"$argon2i$v=19$m=65536,t=3,p=4$c2FsdA$aGFzaA",  // wrong variant
		"$argon2id$v=99$m=65536,t=3,p=4$c2FsdA$aGFzaA", // unsupported version
		"$argon2id$v=19$m=65536,t=3,p=4$$",             // empty salt and key
	} {
		t.Run(stored, func(t *testing.T) {
			if _, err := VerifyPassword(stored, goodPassword); err == nil {
				t.Fatalf("expected an error for stored hash %q", stored)
			}
		})
	}
}

func TestNeedsRehashDetectsWeakerParameters(t *testing.T) {
	weak := "$argon2id$v=19$m=1024,t=1,p=1$c2FsdHNhbHRzYWx0$aGFzaGhhc2hoYXNoaGFzaA"
	needs, err := NeedsRehash(weak)
	if err != nil {
		t.Fatalf("reading parameters: %v", err)
	}
	if !needs {
		t.Fatal("a hash below current parameters should need a rehash")
	}

	current, err := HashPassword(goodPassword)
	if err != nil {
		t.Fatalf("hashing: %v", err)
	}
	needs, err = NeedsRehash(current.Expose())
	if err != nil {
		t.Fatalf("reading parameters: %v", err)
	}
	if needs {
		t.Fatal("a freshly created hash should not need a rehash")
	}
}

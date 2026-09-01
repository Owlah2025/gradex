package identity

import (
	"bytes"
	"crypto/sha256"
	"strings"
	"testing"
	"time"

	"github.com/Owlah2025/gradex/backend/internal/config"
)

func testPepper(t *testing.T, fill byte) EmailOTPPepper {
	t.Helper()
	pepper, err := NewEmailOTPPepper(config.NewSecret(strings.Repeat(string(fill), 32)))
	if err != nil {
		t.Fatalf("NewEmailOTPPepper: %v", err)
	}
	return pepper
}

func TestEmailOTPPepperRefusesShortKeyMaterial(t *testing.T) {
	for _, length := range []int{0, 1, 31} {
		if _, err := NewEmailOTPPepper(config.NewSecret(strings.Repeat("x", length))); err == nil {
			t.Fatalf("a %d-byte pepper was accepted", length)
		}
	}
}

func TestIssuedCodeIsSixDigitsAndUniformOverTheSpace(t *testing.T) {
	pepper := testPepper(t, 'k')
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	counts := map[byte]int{}
	const draws = 2000
	for i := 0; i < draws; i++ {
		otp, err := newEmailOTP(emailOTPOptions{Pepper: pepper, Now: now, TTL: time.Minute})
		if err != nil {
			t.Fatalf("newEmailOTP: %v", err)
		}
		code := otp.Code.Expose()
		if len(code) != emailOTPDigits {
			t.Fatalf("code %q is not %d digits", code, emailOTPDigits)
		}
		for i := 0; i < len(code); i++ {
			if code[i] < '0' || code[i] > '9' {
				t.Fatalf("code %q contains a non-digit", code)
			}
			counts[code[i]]++
		}
	}
	// Modular reduction over uniform bytes biases toward '0'..'5'. With
	// 2000*6 = 12000 digits the expected count per digit is 1200; a biased
	// generator lands far outside this band while an unbiased one is
	// overwhelmingly inside it.
	for digit := byte('0'); digit <= '9'; digit++ {
		if counts[digit] < 950 || counts[digit] > 1450 {
			t.Fatalf("digit %q appeared %d times in %d draws, which is not uniform",
				string(digit), counts[digit], draws*emailOTPDigits)
		}
	}
}

// TestStoredDigestIsNotAPlainHashOfTheCode is the central storage guarantee.
//
// A six-digit space is one million entries. If the stored value were any
// unkeyed function of the code alone, a database copy would be an offline
// dictionary attack completed in microseconds. The digest must depend on
// server-held key material that never enters the row.
func TestStoredDigestIsNotAPlainHashOfTheCode(t *testing.T) {
	pepper := testPepper(t, 'k')
	otp, err := newEmailOTP(emailOTPOptions{
		Pepper: pepper, Now: time.Now().UTC(), TTL: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	code := otp.Code.Expose()
	plain := sha256.Sum256([]byte(code))
	if bytes.Equal(otp.Digest, plain[:]) {
		t.Fatal("the stored digest is a bare SHA-256 of the code")
	}
	// And exhausting the space with the wrong key must not find it either.
	other := testPepper(t, 'q')
	for candidate := 0; candidate < 1000; candidate++ {
		guess, err := other.digest(otp.ChallengeID, code)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Equal(guess, otp.Digest) {
			t.Fatal("a different pepper reproduced the stored digest")
		}
		break
	}
}

// TestDigestIsBoundToItsChallenge proves one recovered code is worth nothing
// against a different challenge, which is what keeps a single compromise from
// becoming a general forgery.
func TestDigestIsBoundToItsChallenge(t *testing.T) {
	pepper := testPepper(t, 'k')
	now := time.Now().UTC()
	first, err := newEmailOTP(emailOTPOptions{Pepper: pepper, Now: now, TTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	second, err := newEmailOTP(emailOTPOptions{Pepper: pepper, Now: now, TTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if first.ChallengeID == second.ChallengeID {
		t.Fatal("two challenges share an identifier")
	}
	if pepper.MatchesEmailOTP(second.ChallengeID, first.Code.Expose(), first.Digest) {
		t.Fatal("a code matched a digest under the wrong challenge")
	}
	if !pepper.MatchesEmailOTP(first.ChallengeID, first.Code.Expose(), first.Digest) {
		t.Fatal("a code did not match its own digest")
	}
}

func TestMatchesRejectsEveryWrongCode(t *testing.T) {
	pepper := testPepper(t, 'k')
	otp, err := newEmailOTP(emailOTPOptions{Pepper: pepper, Now: time.Now().UTC(), TTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	code := otp.Code.Expose()
	for _, wrong := range []string{"000000", "999999", code[:5] + string(rune('0'+(int(code[5]-'0')+1)%10)), "", "12345", "1234567"} {
		if wrong == code {
			continue
		}
		if pepper.MatchesEmailOTP(otp.ChallengeID, wrong, otp.Digest) {
			t.Fatalf("wrong code %q matched", wrong)
		}
	}
}

// TestUnusablePepperFailsClosed proves an absent pepper issues nothing and
// accepts nothing, rather than silently degrading to an unkeyed digest.
func TestUnusablePepperFailsClosed(t *testing.T) {
	var absent EmailOTPPepper
	if _, err := newEmailOTP(emailOTPOptions{Pepper: absent, Now: time.Now(), TTL: time.Minute}); err != ErrOTPUnavailable {
		t.Fatalf("issuing with no pepper returned %v, want ErrOTPUnavailable", err)
	}
	if absent.MatchesEmailOTP("11111111-1111-4111-8111-111111111111", "123456", make([]byte, 32)) {
		t.Fatal("a match succeeded with no pepper")
	}
}

func TestNormalizeEmailOTPInputAcceptsWhatPeopleActuallyType(t *testing.T) {
	accepted := map[string]string{
		"482913":     "482913",
		"482 913":    "482913",
		"482-913":    "482913",
		" 4 8 2913 ": "482913",
		"4\t82913":   "482913",
	}
	for input, want := range accepted {
		got, ok := NormalizeEmailOTPInput(input)
		if !ok || got != want {
			t.Errorf("NormalizeEmailOTPInput(%q) = %q, %v; want %q, true", input, got, ok, want)
		}
	}
	for _, rejected := range []string{"", "12345", "1234567", "48291a", "abcdef", "48291٣", "482913 482913"} {
		if _, ok := NormalizeEmailOTPInput(rejected); ok {
			t.Errorf("NormalizeEmailOTPInput(%q) was accepted", rejected)
		}
	}
}

// TestMaskEmailShowsEnoughToRecogniseAndNotEnoughToRead keeps the verification
// screen honest without turning it into an address disclosure.
func TestMaskEmailShowsEnoughToRecogniseAndNotEnoughToRead(t *testing.T) {
	cases := map[string]string{
		// Case is normalized away: two spellings of one address must mask
		// identically, or the mask echoes the caller's own input back as a
		// distinguishing feature.
		"Ahmed@Example.com":   "ah***@e***.com",
		"ahmed@example.com":   "ah***@e***.com",
		"a@example.com":       "a***@e***.com",
		"student@gradex.test": "st***@g***.test",
		"not-an-address":      "***",
		"@example.com":        "***",
		"user@localhost":      "us***@***",
	}
	for input, want := range cases {
		if got := MaskEmail(input); got != want {
			t.Errorf("MaskEmail(%q) = %q, want %q", input, got, want)
		}
	}
	// Whatever the shape, the full local part must never survive intact.
	if strings.Contains(MaskEmail("ahmedhazem@example.com"), "ahmedhazem") {
		t.Fatal("the local part survived masking")
	}
}

// TestResendCooldownIsDerivedFromIssueTime keeps the countdown the screen
// renders and the refusal the server applies on one clock.
func TestResendCooldownIsDerivedFromIssueTime(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	otp, err := newEmailOTP(emailOTPOptions{Pepper: testPepper(t, 'k'), Now: now, TTL: 10 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if !otp.ResendAvailableAt().Equal(now.Add(EmailOTPResendCooldown)) {
		t.Fatalf("resend available at %v, want %v", otp.ResendAvailableAt(), now.Add(EmailOTPResendCooldown))
	}
	if !otp.ExpiresAt.Equal(now.Add(10 * time.Minute)) {
		t.Fatalf("expiry %v, want %v", otp.ExpiresAt, now.Add(10*time.Minute))
	}
}

package media

import (
	"strings"
	"testing"
	"time"
)

func TestWatermarkDisplayNameShortensToFirstNameAndSurnameInitial(t *testing.T) {
	for _, testCase := range []struct{ name, in, want string }{
		{"full name", "Ahmed Hazem Elmelegy", "Ahmed E."},
		{"two parts", "Ahmed Elmelegy", "Ahmed E."},
		{"single name", "Ahmed", "Ahmed"},
		{"extra whitespace", "  Ahmed   Hazem  ", "Ahmed H."},
		{"arabic", "أحمد حازم المليجي", "أحمد ا."},
		{"empty", "   ", ""},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := watermarkDisplayName(testCase.in); got != testCase.want {
				t.Fatalf("watermarkDisplayName(%q) = %q, want %q", testCase.in, got, testCase.want)
			}
		})
	}
}

// A display name is untrusted free text, so the bound is a property of the
// function rather than of the data that happens to be in the database.
func TestWatermarkDisplayNameIsBoundedOnRuneBoundaries(t *testing.T) {
	long := strings.Repeat("ا", 400) + " " + strings.Repeat("ب", 400)
	got := watermarkDisplayName(long)
	if runes := []rune(got); len(runes) != maxWatermarkNameRunes+3 {
		t.Fatalf("bounded name is %d runes, want %d", len(runes), maxWatermarkNameRunes+3)
	}
	if !strings.HasSuffix(got, " ب.") {
		t.Fatalf("bounded name %q lost its surname initial", got)
	}
	// Truncating by bytes would have split the two-byte Arabic runes.
	if strings.ContainsRune(got, '�') {
		t.Fatalf("bounded name %q contains a replacement rune", got)
	}
}

func TestMaskWatermarkIdentifierKeepsDomainAndHidesLocalPart(t *testing.T) {
	for _, testCase := range []struct{ name, in, want string }{
		{"ordinary address", "ahmed@example.com", "ah***@example.com"},
		{"long local part", "ahmedhazemelmelegy@example.com", "ah***@example.com"},
		{"single-character local part", "a@example.com", "a***@example.com"},
		{"plus addressing", "ahmed+gradex@example.com", "ah***@example.com"},
		{"subdomain", "ahmed@mail.example.com", "ah***@mail.example.com"},
		{"surrounding whitespace", "  ahmed@example.com  ", "ah***@example.com"},
		{"no at sign", "not-an-address", "***"},
		{"empty local part", "@example.com", "***"},
		{"empty domain", "ahmed@", "***"},
		{"empty", "", "***"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := maskWatermarkIdentifier(testCase.in); got != testCase.want {
				t.Fatalf("maskWatermarkIdentifier(%q) = %q, want %q", testCase.in, got, testCase.want)
			}
		})
	}
}

// The whole point of masking is that the address stops being deliverable.
func TestMaskWatermarkIdentifierNeverEmitsTheFullLocalPart(t *testing.T) {
	const local = "ahmedhazemelmelegy"
	masked := maskWatermarkIdentifier(local + "@example.com")
	if strings.Contains(masked, local) {
		t.Fatalf("masked identifier %q still carries the full local part", masked)
	}
}

func watermarkTestService(t *testing.T, key string) *DeliveryService {
	t.Helper()
	return &DeliveryService{buyerTagKey: []byte(key)}
}

func TestWatermarkCodeIsStablePerAccountAndDistinctBetweenAccounts(t *testing.T) {
	service := watermarkTestService(t, "delivery-key")
	const student = "7f9c2ba4-7777-4f5a-9c1e-2b4d6e8a0c11"
	const other = "7f9c2ba4-7777-4f5a-9c1e-2b4d6e8a0c12"

	code := service.watermarkCode(student)
	if code != service.watermarkCode(student) {
		t.Fatal("the same Account produced two different codes")
	}
	if code == service.watermarkCode(other) {
		t.Fatal("two Accounts produced the same code")
	}
}

func TestWatermarkCodeIsFourUnambiguousCharacters(t *testing.T) {
	service := watermarkTestService(t, "delivery-key")
	code := service.watermarkCode("7f9c2ba4-7777-4f5a-9c1e-2b4d6e8a0c11")
	if len(code) != watermarkCodeLength {
		t.Fatalf("code %q is %d characters, want %d", code, len(code), watermarkCodeLength)
	}
	for _, character := range code {
		if !strings.ContainsRune(watermarkCodeAlphabet, character) {
			t.Fatalf("code %q uses %q, which is outside the unambiguous alphabet", code, character)
		}
	}
	// Crockford's exclusions are the reason a code read off a recording can be
	// transcribed without guessing.
	for _, ambiguous := range "ILOU" {
		if strings.ContainsRune(watermarkCodeAlphabet, ambiguous) {
			t.Fatalf("the code alphabet admits the ambiguous character %q", ambiguous)
		}
	}
}

// The code carries no key material and cannot be produced without the server's
// key, which is what stops a client minting a watermark for another Account.
func TestWatermarkCodeDependsOnTheServerKey(t *testing.T) {
	const student = "7f9c2ba4-7777-4f5a-9c1e-2b4d6e8a0c11"
	if watermarkTestService(t, "key-one").watermarkCode(student) ==
		watermarkTestService(t, "key-two").watermarkCode(student) {
		t.Fatal("the code did not change with the signing key")
	}
}

// Domain separation is the reason sharing one key across derivations is safe:
// no tag can be made to produce another tag's value for the same input.
func TestWatermarkCodeIsDomainSeparatedFromTheOtherDerivations(t *testing.T) {
	service := watermarkTestService(t, "delivery-key")
	const student = "7f9c2ba4-7777-4f5a-9c1e-2b4d6e8a0c11"
	const version = "9a1b2c3d-4444-4e5f-8a9b-0c1d2e3f4a5b"

	code := service.watermarkCode(student)
	if strings.HasPrefix(service.buyerTag(student, version), code) {
		t.Fatal("the watermark code is a prefix of the buyer tag for the same input")
	}
	session := service.playbackSession(student, "lesson", version, time.Unix(0, 0).UTC())
	if strings.Contains(session, code) {
		t.Fatal("the watermark code appears inside the playback session token")
	}
}

// The visible code must never be a truncation of the capability that authorises
// playback: a code is meant to be readable off a screen, a session token is not.
func TestWatermarkCodeIsNotDerivedFromThePlaybackSession(t *testing.T) {
	service := watermarkTestService(t, "delivery-key")
	const student = "7f9c2ba4-7777-4f5a-9c1e-2b4d6e8a0c11"
	session := service.playbackSession(student, "lesson", "version", time.Unix(0, 0).UTC())
	code := service.watermarkCode(student)
	if strings.HasPrefix(session, code) || strings.HasSuffix(session, code) {
		t.Fatal("the code is an end of the playback session token")
	}
	// The session is bound to a Lesson and an expiry; the code is bound to
	// neither, so a second session for the same Student still shows one code.
	later := service.playbackSession(student, "other-lesson", "version", time.Unix(0, 0).UTC())
	if session == later {
		t.Fatal("the playback session did not change with its Lesson")
	}
	if code != service.watermarkCode(student) {
		t.Fatal("the code changed with the session it was issued beside")
	}
}

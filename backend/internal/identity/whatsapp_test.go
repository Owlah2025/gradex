package identity

import (
	"errors"
	"testing"
)

func TestNormalizeWhatsAppNumberAcceptsInternationalMobiles(t *testing.T) {
	cases := map[string]string{
		"51234567":           "+96551234567", // Kuwait is the default region
		"9912 3456":          "+96599123456",
		"+965 6001 2345":     "+96560012345",
		"00965 5123 4567":    "+96551234567",
		"+20 100 123 4567":   "+201001234567", // Egypt
		"+201001234567":      "+201001234567",
		"+966 50 123 4567":   "+966501234567", // Saudi Arabia
		"+971 50 123 4567":   "+971501234567", // UAE
		"+44 7400 123456":    "+447400123456", // UK
		"+1 (201) 555-0123":  "+12015550123",  // NANP: fixed-line-or-mobile
		"٥١٢٣٤٥٦٧":           "+96551234567",  // Arabic-Indic digits
		"  +965-5123-4567  ": "+96551234567",
		"+965 (5123) 4567":   "+96551234567",
		"۰۰۹۶۵۵۱۲۳۴۵۶۷":      "+96551234567", // Eastern Arabic-Indic digits with 00
	}
	for input, want := range cases {
		got, err := NormalizeWhatsAppNumber(input)
		if err != nil {
			t.Errorf("NormalizeWhatsAppNumber(%q) error = %v, want %q", input, err, want)
			continue
		}
		if got != want {
			t.Errorf("NormalizeWhatsAppNumber(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeWhatsAppNumberRefusesInvalidAndNonMobile(t *testing.T) {
	for _, input := range []string{
		"",
		"   ",
		"abc",
		"123",
		"5123456",                             // one digit short for Kuwait
		"512345678",                           // one digit long for Kuwait
		"22123456",                            // Kuwait fixed line
		"+965 2212 3456",                      // Kuwait fixed line, explicit
		"+20 2 2345 6789",                     // Cairo fixed line
		"+44 800 123 4567",                    // UK toll-free
		"+999 1234 5678",                      // unassigned country code
		"+96551234567x",                       // trailing garbage
		"+9655123456712345678901234567890123", // too long to be a number
		"javascript:alert(1)",
		"+965 9123 4567",        // unallocated Kuwaiti range
		"+965 5123 4567 ext 12", // extensions are not WhatsApp accounts
		"5123+4567",             // a "+" anywhere but first
	} {
		if got, err := NormalizeWhatsAppNumber(input); !errors.Is(err, ErrInvalidWhatsAppNumber) {
			t.Errorf("NormalizeWhatsAppNumber(%q) = %q, %v; want ErrInvalidWhatsAppNumber", input, got, err)
		}
	}
}

func TestIsE164WhatsAppNumber(t *testing.T) {
	for value, want := range map[string]bool{
		"+96551234567":      true,
		"+201001234567":     true,
		"96551234567":       false,
		"+0965512345":       false,
		"+965 51234567":     false,
		"+1234567":          true,
		"+123456":           false,
		"+1234567890123456": false,
		"":                  false,
	} {
		if got := IsE164WhatsAppNumber(value); got != want {
			t.Errorf("IsE164WhatsAppNumber(%q) = %v, want %v", value, got, want)
		}
	}
}

func TestWhatsAppChatURLNeverBuildsABrokenLink(t *testing.T) {
	if got := WhatsAppChatURL("+96551234567"); got != "https://wa.me/96551234567" {
		t.Fatalf("WhatsAppChatURL = %q", got)
	}
	for _, value := range []string{"", "51234567", "+965 5123 4567", "+abc"} {
		if got := WhatsAppChatURL(value); got != "" {
			t.Errorf("WhatsAppChatURL(%q) = %q, want empty", value, got)
		}
	}
}

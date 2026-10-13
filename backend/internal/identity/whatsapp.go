package identity

import (
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/nyaruka/phonenumbers"
)

// DefaultWhatsAppRegion is the region assumed for a number typed without an
// international prefix. Gradex serves Kuwaiti universities first, so a Student
// who types "51234567" means +965 51234567. Any other country is reachable by
// typing its "+" prefix, and nothing here restricts a Student to Kuwait.
const DefaultWhatsAppRegion = "KW"

// maxWhatsAppInputRunes bounds what is handed to the parser. A real number,
// with every separator a person might type, is far shorter.
const maxWhatsAppInputRunes = 32

// ErrInvalidWhatsAppNumber means the value is not a dialable mobile number in
// any country. The value itself is never echoed back or logged.
var ErrInvalidWhatsAppNumber = errors.New("whatsapp number is not a valid mobile number")

// NormalizeWhatsAppNumber validates a Student's WhatsApp number and returns it
// in E.164 form ("+96551234567").
//
// The backend is the authority: the browser may pre-validate for a faster
// answer, but only a value that passes here is ever stored. A number must be a
// valid number for its region and must be able to receive mobile messages, so
// fixed lines, toll-free, and premium-rate numbers are refused. Nothing here
// claims the number belongs to the Student; Gradex does not verify ownership,
// so the product calls such a number "provided", never "verified".
func NormalizeWhatsAppNumber(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" || utf8.RuneCountInString(value) > maxWhatsAppInputRunes {
		return "", ErrInvalidWhatsAppNumber
	}
	// Arabic-Indic and Eastern Arabic-Indic digits are what an Arabic keyboard
	// produces. They are the same number, so they are read as such.
	value = asciiDigits(value)
	// Only what a person types into a phone field: digits, one leading "+",
	// and visual separators. Letters would otherwise reach the parser's
	// extension and vanity-number handling, neither of which is a WhatsApp
	// account.
	for index, r := range value {
		switch {
		case r >= '0' && r <= '9', r == ' ', r == '-', r == '(', r == ')', r == '.':
		case r == '+' && index == 0:
		default:
			return "", ErrInvalidWhatsAppNumber
		}
	}
	// "00" is the international call prefix most Gulf and Egyptian users type
	// in place of "+".
	if strings.HasPrefix(value, "00") {
		value = "+" + strings.TrimPrefix(value, "00")
	}
	parsed, err := phonenumbers.Parse(value, DefaultWhatsAppRegion)
	if err != nil {
		return "", ErrInvalidWhatsAppNumber
	}
	if parsed.GetExtension() != "" || !phonenumbers.IsValidNumber(parsed) {
		return "", ErrInvalidWhatsAppNumber
	}
	switch phonenumbers.GetNumberType(parsed) {
	case phonenumbers.MOBILE, phonenumbers.FIXED_LINE_OR_MOBILE, phonenumbers.PERSONAL_NUMBER:
	default:
		return "", ErrInvalidWhatsAppNumber
	}
	normalized := phonenumbers.Format(parsed, phonenumbers.E164)
	if !IsE164WhatsAppNumber(normalized) {
		return "", ErrInvalidWhatsAppNumber
	}
	return normalized, nil
}

// IsE164WhatsAppNumber reports whether value has the stored shape. It mirrors
// the database CHECK constraint on accounts.contact_phone.
func IsE164WhatsAppNumber(value string) bool {
	if len(value) < 8 || len(value) > 16 || value[0] != '+' || value[1] == '0' {
		return false
	}
	for _, r := range value[1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// WhatsAppChatURL is the direct chat link for a stored E.164 number, or "" for
// a value that is not one. A broken link is worse than no link, so callers that
// receive "" render a "not provided" state instead.
func WhatsAppChatURL(e164 string) string {
	if !IsE164WhatsAppNumber(e164) {
		return ""
	}
	return "https://wa.me/" + strings.TrimPrefix(e164, "+")
}

func asciiDigits(value string) string {
	var builder strings.Builder
	builder.Grow(len(value))
	for _, r := range value {
		switch {
		case r >= '٠' && r <= '٩':
			builder.WriteRune('0' + (r - '٠'))
		case r >= '۰' && r <= '۹':
			builder.WriteRune('0' + (r - '۰'))
		default:
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

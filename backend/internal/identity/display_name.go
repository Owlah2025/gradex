package identity

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	minStudentDisplayNameRunes = 2
	maxStudentDisplayNameRunes = 50
)

// ValidateDisplayName applies BR-105 to a Student profile label. It returns
// the trimmed correspondence form without normalizing or transliterating it.
func ValidateDisplayName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	runeCount := utf8.RuneCountInString(name)
	if runeCount < minStudentDisplayNameRunes || runeCount > maxStudentDisplayNameRunes {
		return "", fmt.Errorf("%w: must contain 2 to 50 characters", ErrInvalidDisplayName)
	}

	letters := 0
	for _, char := range name {
		switch {
		case unicode.IsLetter(char) &&
			(unicode.In(char, unicode.Latin) || unicode.In(char, unicode.Arabic)):
			letters++
		case unicode.IsMark(char):
		case unicode.IsSpace(char):
		case char == '-' || char == '\'' || char == '’':
		default:
			return "", fmt.Errorf("%w: contains an unsupported character", ErrInvalidDisplayName)
		}
	}
	if letters < minStudentDisplayNameRunes {
		return "", fmt.Errorf("%w: must contain at least two letters", ErrInvalidDisplayName)
	}
	return name, nil
}

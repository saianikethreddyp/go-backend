// Package phone normalizes phone numbers to E.164 so the same person is never
// stored twice under different spellings.
package phone

import (
	"regexp"
	"strings"
)

var (
	e164     = regexp.MustCompile(`^\+[1-9][0-9]{7,14}$`)
	indianMN = regexp.MustCompile(`^[6-9][0-9]{9}$`) // Indian mobile numbers
)

// Normalize converts raw user input to E.164 (for example +919876543210).
//
// Accepted forms: "+<country><number>", "00<country><number>", and Indian
// numbers written as 10 digits, 0 + 10 digits, or 91 + 10 digits. Spaces,
// dashes, dots and brackets are ignored. It reports false for anything else.
func Normalize(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	if s == "" || len(s) > 40 {
		return "", false
	}

	plus := strings.HasPrefix(s, "+")
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '.' || r == '(' || r == ')' || r == '+':
			// ignore separators; a plus is only meaningful at the start
		default:
			return "", false
		}
	}
	digits := b.String()

	var out string
	switch {
	case plus:
		out = "+" + digits
	case strings.HasPrefix(digits, "00"):
		out = "+" + digits[2:]
	case indianMN.MatchString(digits):
		out = "+91" + digits
	case len(digits) == 11 && digits[0] == '0' && indianMN.MatchString(digits[1:]):
		out = "+91" + digits[1:]
	case len(digits) == 12 && strings.HasPrefix(digits, "91") && indianMN.MatchString(digits[2:]):
		out = "+" + digits
	default:
		return "", false
	}

	if !e164.MatchString(out) {
		return "", false
	}
	if strings.HasPrefix(out, "+91") && !indianMN.MatchString(out[3:]) {
		return "", false
	}
	return out, true
}

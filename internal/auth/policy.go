package auth

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// PasswordPolicy is enforced whenever a local password is set through the
// API (create user, admin reset, self-service change). Imported Nexus
// hashes and the bootstrap admin password are not re-validated, but the
// health check flags the default admin password.
type PasswordPolicy struct {
	MinLength        int  `json:"minLength"`
	MaxLength        int  `json:"maxLength"`
	RequireUpper     bool `json:"requireUpper"`
	RequireLower     bool `json:"requireLower"`
	RequireDigit     bool `json:"requireDigit"`
	RequireSymbol    bool `json:"requireSymbol"`
	DisallowUsername bool `json:"disallowUsername"`
	// DisallowCommon rejects a built-in list of frequently used passwords.
	DisallowCommon bool `json:"disallowCommon"`
}

func defaultPasswordPolicy() PasswordPolicy {
	return PasswordPolicy{MinLength: 12, MaxLength: 128, RequireUpper: true, RequireLower: true, RequireDigit: true, RequireSymbol: false, DisallowUsername: true, DisallowCommon: true}
}

// PolicyError carries a stable code and params for client-side localisation.
type PolicyError struct {
	Code   string
	Format string
	Params []any
}

func (e *PolicyError) Error() string { return fmt.Sprintf(e.Format, e.Params...) }

func perr(code, format string, params ...any) error {
	return &PolicyError{Code: code, Format: format, Params: params}
}

var commonPasswords = map[string]bool{}

func init() {
	for _, p := range []string{
		"password", "password1", "password123", "passw0rd", "p@ssw0rd", "p@ssword", "123456", "1234567", "12345678", "123456789", "1234567890", "12345", "qwerty", "qwerty123", "qwertyuiop", "abc123", "111111", "123123", "letmein", "welcome", "welcome1", "admin", "admin123", "administrator", "root", "toor", "changeme", "change-me", "iloveyou", "monkey", "dragon", "sunshine", "princess", "football", "baseball", "master", "login", "starwars", "hello", "freedom", "whatever", "trustno1", "654321", "superman", "1q2w3e4r", "1qaz2wsx", "zaq12wsx", "qazwsx", "asdfgh", "zxcvbn", "test", "test123", "guest", "user", "default", "secret", "nexus", "holiaokho", "aa123456", "a123456", "abcd1234", "1234qwer", "pass", "pass123", "password!", "password1!", "summer2024", "winter2024", "spring2025", "summer2025",
	} {
		commonPasswords[p] = true
	}
}

// Validate checks pw against the policy; username may be empty.
func (p PasswordPolicy) Validate(pw, username string) error {
	if p.MinLength <= 0 {
		p.MinLength = 1
	}
	n := len([]rune(pw))
	if n < p.MinLength {
		return perr("password.too_short", "password must be at least %d characters", p.MinLength)
	}
	if p.MaxLength > 0 && n > p.MaxLength {
		return perr("password.too_long", "password must be at most %d characters", p.MaxLength)
	}
	var upper, lower, digit, symbol bool
	for _, r := range pw {
		switch {
		case unicode.IsUpper(r):
			upper = true
		case unicode.IsLower(r):
			lower = true
		case unicode.IsDigit(r):
			digit = true
		case unicode.IsPunct(r) || unicode.IsSymbol(r) || r == ' ':
			symbol = true
		}
	}
	if p.RequireUpper && !upper {
		return perr("password.need_upper", "password must contain an uppercase letter")
	}
	if p.RequireLower && !lower {
		return perr("password.need_lower", "password must contain a lowercase letter")
	}
	if p.RequireDigit && !digit {
		return perr("password.need_digit", "password must contain a digit")
	}
	if p.RequireSymbol && !symbol {
		return perr("password.need_symbol", "password must contain a symbol")
	}
	if p.DisallowUsername && username != "" && len(username) >= 3 && strings.Contains(strings.ToLower(pw), strings.ToLower(username)) {
		return perr("password.contains_username", "password must not contain the username")
	}
	if p.DisallowCommon {
		norm := strings.ToLower(strings.TrimSpace(pw))
		if commonPasswords[norm] || commonPasswords[strings.TrimRight(norm, "!.0123456789")] {
			return perr("password.common", "password is too common")
		}
	}
	return nil
}

// ErrPolicy is a sentinel usable with errors.Is via PolicyError.
var ErrPolicy = errors.New("password policy")

func (e *PolicyError) Is(target error) bool { return target == ErrPolicy }

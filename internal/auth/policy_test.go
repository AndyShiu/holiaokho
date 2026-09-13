package auth

import (
	"errors"
	"testing"
)

func TestPasswordPolicy(t *testing.T) {
	p := defaultPasswordPolicy()
	cases := map[string]string{
		"short1A":              "password.too_short",
		"alllowercase123456":   "password.need_upper",
		"ALLUPPERCASE123456":   "password.need_lower",
		"NoDigitsHereAtAll":    "password.need_digit",
		"AliceLiu2026secure":   "password.contains_username",
		"Password123456":       "password.common",
		"Correct-Horse-9-Batt": "",
	}
	for pw, want := range cases {
		err := p.Validate(pw, "aliceliu")
		if want == "" {
			if err != nil {
				t.Errorf("%q should pass: %v", pw, err)
			}
			continue
		}
		var pe *PolicyError
		if !errors.As(err, &pe) || pe.Code != want || !errors.Is(err, ErrPolicy) {
			t.Errorf("%q: got %v want %s", pw, err, want)
		}
	}
	strict := p
	strict.RequireSymbol = true
	if err := strict.Validate("Correct-Horse-9-Batt", ""); err != nil {
		t.Errorf("hyphen counts as symbol: %v", err)
	}
	if err := strict.Validate("CorrectHorse9Batt", ""); err == nil {
		t.Error("symbol required")
	}
}

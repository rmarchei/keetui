package keepass

import (
	"testing"
)

func TestEvaluatePasswordStrengthEmpty(t *testing.T) {
	result := EvaluatePasswordStrength("")
	if result.Score != StrengthWeak {
		t.Errorf("empty password score = %d, want %d (Weak)", result.Score, StrengthWeak)
	}
	if result.Entropy != 0 {
		t.Errorf("empty password entropy = %f, want 0", result.Entropy)
	}
}

func TestEvaluatePasswordStrengthWeak(t *testing.T) {
	result := EvaluatePasswordStrength("abc")
	if result.Score != StrengthWeak {
		t.Errorf("'abc' score = %d (%s), want Weak", result.Score, result.Label)
	}
}

func TestEvaluatePasswordStrengthCommonPattern(t *testing.T) {
	result := EvaluatePasswordStrength("password123")
	// Should be penalized for containing "password"
	if result.Score >= StrengthStrong {
		t.Errorf("'password123' score = %d (%s), should not be Strong or better", result.Score, result.Label)
	}
}

func TestEvaluatePasswordStrengthStrong(t *testing.T) {
	result := EvaluatePasswordStrength("Tr0ub4dor&3#$!xK9mQ")
	if result.Score < StrengthGood {
		t.Errorf("strong password score = %d (%s), want Good or better", result.Score, result.Label)
	}
}

func TestEvaluatePasswordStrengthExcellent(t *testing.T) {
	result := EvaluatePasswordStrength("correct-horse-battery-staple-extra-long-passphrase")
	if result.Score < StrengthStrong {
		t.Errorf("excellent password score = %d (%s), want Strong or better", result.Score, result.Label)
	}
}

func TestEvaluatePasswordStrengthAllDigits(t *testing.T) {
	result := EvaluatePasswordStrength("12345678")
	// All same class + sequential penalty
	if result.Score > StrengthFair {
		t.Errorf("all-digit password score = %d (%s), want Fair or worse", result.Score, result.Label)
	}
}

func TestStrengthLevelString(t *testing.T) {
	tests := []struct {
		level StrengthLevel
		want  string
	}{
		{StrengthWeak, "Weak"},
		{StrengthFair, "Fair"},
		{StrengthGood, "Good"},
		{StrengthStrong, "Strong"},
		{StrengthExcellent, "Excellent"},
	}
	for _, tt := range tests {
		if got := tt.level.String(); got != tt.want {
			t.Errorf("StrengthLevel(%d).String() = %q, want %q", tt.level, got, tt.want)
		}
	}
}

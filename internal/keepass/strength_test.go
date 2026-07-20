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

func TestEvaluatePasswordStrengthCharClasses(t *testing.T) {
	tests := []struct {
		name       string
		password   string
		wantUpper  bool
		wantLower  bool
		wantDigit  bool
		wantSymbol bool
		wantLen    int
	}{
		{"mixed", "aB1!", true, true, true, true, 4},
		{"lower only", "abcdef", false, true, false, false, 6},
		{"upper+digits", "ABC123", true, false, true, false, 6},
		{"symbols only", "!@#$%^", false, false, false, true, 6},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := EvaluatePasswordStrength(tt.password)
			if r.HasUpper != tt.wantUpper {
				t.Errorf("HasUpper = %v, want %v", r.HasUpper, tt.wantUpper)
			}
			if r.HasLower != tt.wantLower {
				t.Errorf("HasLower = %v, want %v", r.HasLower, tt.wantLower)
			}
			if r.HasDigit != tt.wantDigit {
				t.Errorf("HasDigit = %v, want %v", r.HasDigit, tt.wantDigit)
			}
			if r.HasSymbol != tt.wantSymbol {
				t.Errorf("HasSymbol = %v, want %v", r.HasSymbol, tt.wantSymbol)
			}
			if r.Length != tt.wantLen {
				t.Errorf("Length = %d, want %d", r.Length, tt.wantLen)
			}
		})
	}
}

func TestEntropyToScoreBoundaries(t *testing.T) {
	// Verify the exact boundaries: <28 Weak, <36 Fair, <60 Good, <80 Strong, >=80 Excellent
	tests := []struct {
		name    string
		entropy float64
		want    StrengthLevel
	}{
		{"0 bits", 0, StrengthWeak},
		{"27.9 bits", 27.9, StrengthWeak},
		{"28 bits", 28, StrengthFair},
		{"35.9 bits", 35.9, StrengthFair},
		{"36 bits", 36, StrengthGood},
		{"59.9 bits", 59.9, StrengthGood},
		{"60 bits", 60, StrengthStrong},
		{"79.9 bits", 79.9, StrengthStrong},
		{"80 bits", 80, StrengthExcellent},
		{"100 bits", 100, StrengthExcellent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := entropyToScore(tt.entropy)
			if got != tt.want {
				t.Errorf("entropyToScore(%v) = %v (%s), want %v (%s)",
					tt.entropy, got, got.String(), tt.want, tt.want.String())
			}
		})
	}
}

func TestSequentialAndRepeatedPenalties(t *testing.T) {
	// Sequential: "abcdef" has 5 sequential pairs, penalty = 5*2 = 10
	seqResult := EvaluatePasswordStrength("abcdef")
	// Same length, no sequences: "afbgch"
	noSeqResult := EvaluatePasswordStrength("afbgch")
	if seqResult.Entropy >= noSeqResult.Entropy {
		t.Errorf("sequential 'abcdef' entropy (%v) should be less than non-sequential 'afbgch' (%v)",
			seqResult.Entropy, noSeqResult.Entropy)
	}

	// Repeated: "aaaaaa" has 5 repeat pairs (>2), penalty = 5*2 = 10
	repResult := EvaluatePasswordStrength("aaaaaa")
	noRepResult := EvaluatePasswordStrength("afbgch")
	if repResult.Entropy >= noRepResult.Entropy {
		t.Errorf("repeated 'aaaaaa' entropy (%v) should be less than 'afbgch' (%v)",
			repResult.Entropy, noRepResult.Entropy)
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

func TestPassphraseEntropyIsCombinatorial(t *testing.T) {
	pp, err := GeneratePassphrase(6, "-")
	if err != nil {
		t.Fatalf("GeneratePassphrase: %v", err)
	}
	r := EvaluatePasswordStrength(pp)
	// Real entropy is 6 * log2(len(list)) ≈ 60 bits; the char-pool model
	// used to report ~170.
	if r.Entropy > 70 {
		t.Errorf("entropy = %.1f bits, want ~60 (combinatorial), not char-pool inflated", r.Entropy)
	}
	if r.Entropy < 50 {
		t.Errorf("entropy = %.1f bits, unexpectedly low for a 6-word passphrase", r.Entropy)
	}

	// Non-passphrase passwords keep the char-pool estimate.
	r = EvaluatePasswordStrength("Xk9#mQ2$vL7@")
	if r.Entropy < 70 {
		t.Errorf("random password entropy = %.1f, expected char-pool estimate", r.Entropy)
	}
}

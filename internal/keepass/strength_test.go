package keepass

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestEvaluatePasswordStrengthEmpty(t *testing.T) {
	result := EvaluatePasswordStrength("")
	if result.Score != StrengthWeak {
		t.Errorf("empty password score = %d, want %d (Weak)", result.Score, StrengthWeak)
	}
	if result.GuessBits != 0 {
		t.Errorf("empty password guess bits = %f, want 0", result.GuessBits)
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

func TestGuessBitsToScoreBoundaries(t *testing.T) {
	// Labels follow estimated guesses, not character-pool entropy.
	tests := []struct {
		name      string
		guessBits float64
		want      StrengthLevel
	}{
		{"0 bits", 0, StrengthWeak},
		{"below million", 6*math.Log2(10) - 0.01, StrengthWeak},
		{"million", 6 * math.Log2(10), StrengthFair},
		{"below hundred million", 8*math.Log2(10) - 0.01, StrengthFair},
		{"hundred million", 8 * math.Log2(10), StrengthGood},
		{"below trillion", 12*math.Log2(10) - 0.01, StrengthGood},
		{"trillion", 12 * math.Log2(10), StrengthStrong},
		{"below ten quadrillion", 16*math.Log2(10) - 0.01, StrengthStrong},
		{"ten quadrillion", 16 * math.Log2(10), StrengthExcellent},
		{"100 bits", 100, StrengthExcellent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := guessBitsToScore(tt.guessBits)
			if got != tt.want {
				t.Errorf("guessBitsToScore(%v) = %v (%s), want %v (%s)",
					tt.guessBits, got, got.String(), tt.want, tt.want.String())
			}
		})
	}
}

func TestSequentialAndRepeatedPenalties(t *testing.T) {
	// A sequence and a repeated character must be easier to guess.
	seqResult := EvaluatePasswordStrength("abcdef")
	// Same length, no sequences: "afbgch"
	noSeqResult := EvaluatePasswordStrength("afbgch")
	if seqResult.GuessBits >= noSeqResult.GuessBits {
		t.Errorf("sequential 'abcdef' guess bits (%v) should be less than non-sequential 'afbgch' (%v)",
			seqResult.GuessBits, noSeqResult.GuessBits)
	}

	// Repeated characters.
	repResult := EvaluatePasswordStrength("aaaaaa")
	noRepResult := EvaluatePasswordStrength("afbgch")
	if repResult.GuessBits >= noRepResult.GuessBits {
		t.Errorf("repeated 'aaaaaa' guess bits (%v) should be less than 'afbgch' (%v)",
			repResult.GuessBits, noRepResult.GuessBits)
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

func TestPassphraseGuessCeiling(t *testing.T) {
	// Fixed samples keep this regression independent of random word selection.
	result := EvaluatePasswordStrength(strings.Join(dicewareWordList[:6], "-"))
	ceiling := 6 * math.Log2(float64(len(dicewareWordList)))
	if result.GuessBits > ceiling+0.1 {
		t.Errorf("guess bits = %.1f, exceeds generator search space %.1f", result.GuessBits, ceiling)
	}
	repeated := EvaluatePasswordStrength("apple-apple-apple-apple-apple-apple")
	if repeated.Score > StrengthFair || repeated.GuessBits >= result.GuessBits {
		t.Fatalf("repeated words must not inherit random-passphrase strength: %+v", repeated)
	}
}

func TestPredictablePasswordsStayWeak(t *testing.T) {
	for _, password := range []string{
		"P@ssw0rd!", "qwertyuiop",
		"12/12/1990", "abcabcabcabc", "password123", "🌈🌈🌈🌈",
	} {
		t.Run(password, func(t *testing.T) {
			result := EvaluatePasswordStrength(password)
			if result.Score != StrengthWeak {
				t.Errorf("predictable password rated %+v", result)
			}
		})
	}
}

func TestGuessScaleDoesNotFlagUnmatchedEightCharactersAsWeak(t *testing.T) {
	for _, password := range []string{"Xk9#mQ2$", "vR7!zN4@", "PASS-e1-v2"} {
		r := EvaluatePasswordStrength(password)
		if r.Score != StrengthGood || r.GuessBits < 26 || r.GuessBits > 29 {
			t.Errorf("unexpected guess-based rating for %q: %+v", password, r)
		}
	}
}

func TestStrengthBoundsAndUnicode(t *testing.T) {
	for _, password := range []string{"é", "🔑aB2!", "密碼密碼密碼", "pässwörd123", strings.Repeat("🌈", 5000)} {
		result := EvaluatePasswordStrength(password)
		if result.Length != utf8.RuneCountInString(password) || result.AnalyzedLength > maxStrengthRunes {
			t.Fatalf("incorrect length accounting: %+v", result)
		}
		if math.IsNaN(result.GuessBits) || math.IsInf(result.GuessBits, 0) || result.GuessBits < 0 {
			t.Fatalf("invalid estimate: %+v", result)
		}
	}
	prefix := strings.Repeat("a", maxStrengthRunes)
	long := EvaluatePasswordStrength(prefix + "K9!random-looking-suffix")
	if long.GuessBits != EvaluatePasswordStrength(prefix).GuessBits || long.AnalyzedLength != maxStrengthRunes {
		t.Fatalf("long password should explicitly rate only its prefix: %+v", long)
	}
	if result := EvaluatePasswordStrength(string([]byte{0xff, 0x80})); !result.Unavailable {
		t.Fatal("invalid UTF-8 should be unrated, not assigned an estimate")
	}
}

func BenchmarkEvaluatePasswordStrength(b *testing.B) {
	for _, password := range []string{"PASS-e1-v2", "Xk9#mQ2$vL7@", strings.Repeat("abC9!?", 20)} {
		b.Run(fmt.Sprintf("length-%d", len(password)), func(b *testing.B) {
			for b.Loop() {
				EvaluatePasswordStrength(password)
			}
		})
	}
}

func FuzzEvaluatePasswordStrength(f *testing.F) {
	for _, password := range []string{"PASS-e1-v2", "é", "🌈🌈🌈", "密碼密碼密碼", "İpassword", "\xff", strings.Repeat("abc", 40)} {
		f.Add(password)
	}
	f.Fuzz(func(t *testing.T, password string) {
		done := make(chan StrengthResult, 1)
		go func() { done <- EvaluatePasswordStrength(password) }()
		var result StrengthResult
		select {
		case result = <-done:
		case <-time.After(time.Second):
			t.Fatalf("scorer did not finish for %q", password)
		}
		if math.IsNaN(result.GuessBits) || math.IsInf(result.GuessBits, 0) || result.GuessBits < 0 {
			t.Fatalf("invalid estimate: %+v", result)
		}
	})
}

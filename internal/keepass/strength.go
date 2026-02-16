package keepass

import (
	"math"
	"strings"
	"unicode"
)

// StrengthLevel represents a password strength rating.
type StrengthLevel int

const (
	StrengthWeak      StrengthLevel = 0
	StrengthFair      StrengthLevel = 1
	StrengthGood      StrengthLevel = 2
	StrengthStrong    StrengthLevel = 3
	StrengthExcellent StrengthLevel = 4
)

// StrengthResult holds the output of a password strength evaluation.
type StrengthResult struct {
	Score     StrengthLevel
	Label     string
	Entropy   float64 // bits of entropy
	Length    int
	HasUpper  bool
	HasLower  bool
	HasDigit  bool
	HasSymbol bool
}

// String returns the human-readable label.
func (s StrengthLevel) String() string {
	switch s {
	case StrengthWeak:
		return "Weak"
	case StrengthFair:
		return "Fair"
	case StrengthGood:
		return "Good"
	case StrengthStrong:
		return "Strong"
	case StrengthExcellent:
		return "Excellent"
	default:
		return "Unknown"
	}
}

// EvaluatePasswordStrength estimates the strength of a password based on
// its length, character class diversity, and common weakness patterns.
func EvaluatePasswordStrength(password string) StrengthResult {
	if password == "" {
		return StrengthResult{Score: StrengthWeak, Label: "Weak", Entropy: 0}
	}

	entropy, classes := calculateEntropy(password)

	// Apply penalties for common patterns
	entropy -= patternPenalty(password)
	if entropy < 0 {
		entropy = 0
	}

	score := entropyToScore(entropy)

	return StrengthResult{
		Score:     score,
		Label:     score.String(),
		Entropy:   math.Round(entropy*10) / 10,
		Length:    len([]rune(password)),
		HasUpper:  classes.upper,
		HasLower:  classes.lower,
		HasDigit:  classes.digit,
		HasSymbol: classes.symbol,
	}
}

type charClasses struct {
	lower, upper, digit, symbol bool
}

// calculateEntropy estimates the bits of entropy in a password based on
// the character pool size and length.
func calculateEntropy(password string) (float64, charClasses) {
	var cc charClasses
	var hasUnicode bool
	poolSize := 0

	for _, r := range password {
		switch {
		case unicode.IsLower(r) && r < 128:
			cc.lower = true
		case unicode.IsUpper(r) && r < 128:
			cc.upper = true
		case unicode.IsDigit(r) && r < 128:
			cc.digit = true
		case r < 128:
			cc.symbol = true
		default:
			hasUnicode = true
		}
	}

	if cc.lower {
		poolSize += 26
	}
	if cc.upper {
		poolSize += 26
	}
	if cc.digit {
		poolSize += 10
	}
	if cc.symbol {
		poolSize += 32
	}
	if hasUnicode {
		poolSize += 100
	}

	if poolSize == 0 {
		poolSize = 1
	}

	return float64(len([]rune(password))) * math.Log2(float64(poolSize)), cc
}

// patternPenalty reduces entropy for passwords with common weaknesses.
func patternPenalty(password string) float64 {
	penalty := 0.0
	lower := strings.ToLower(password)

	// Penalty for common passwords/patterns
	commonPatterns := []string{
		"password", "123456", "qwerty", "abc123", "letmein",
		"admin", "welcome", "monkey", "master", "dragon",
		"login", "princess", "football", "shadow", "sunshine",
		"trustno1", "iloveyou", "batman", "access", "hello",
	}
	for _, pattern := range commonPatterns {
		if strings.Contains(lower, pattern) {
			penalty += 15
			break
		}
	}

	// Penalty for sequential characters (abc, 123, etc.)
	seqCount := 0
	runes := []rune(password)
	for i := 1; i < len(runes); i++ {
		if runes[i] == runes[i-1]+1 {
			seqCount++
		}
	}
	if seqCount > 2 {
		penalty += float64(seqCount) * 2
	}

	// Penalty for repeated characters (aaa, 111, etc.)
	repCount := 0
	for i := 1; i < len(runes); i++ {
		if runes[i] == runes[i-1] {
			repCount++
		}
	}
	if repCount > 2 {
		penalty += float64(repCount) * 2
	}

	// Penalty for all same character class
	allSame := true
	if len(runes) > 0 {
		first := charClass(runes[0])
		for _, r := range runes[1:] {
			if charClass(r) != first {
				allSame = false
				break
			}
		}
	}
	if allSame && len(runes) > 0 {
		penalty += 10
	}

	return penalty
}

func charClass(r rune) int {
	switch {
	case unicode.IsLower(r):
		return 0
	case unicode.IsUpper(r):
		return 1
	case unicode.IsDigit(r):
		return 2
	default:
		return 3
	}
}

func entropyToScore(entropy float64) StrengthLevel {
	switch {
	case entropy < 28:
		return StrengthWeak
	case entropy < 36:
		return StrengthFair
	case entropy < 60:
		return StrengthGood
	case entropy < 80:
		return StrengthStrong
	default:
		return StrengthExcellent
	}
}

package keepass

import (
	"math"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/trustelem/zxcvbn"
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

// Limit expensive pattern matching, without modifying the stored password.
// The UI explicitly labels estimates made from a prefix.
const maxStrengthRunes = 100

// StrengthResult contains an offline guessability estimate, not a measurement
// of entropy. Character classes are descriptive and do not increase the score.
type StrengthResult struct {
	Score          StrengthLevel
	Label          string
	GuessBits      float64 // log2 of estimated guesses; not generation entropy
	Length         int
	AnalyzedLength int
	Unavailable    bool
	HasUpper       bool
	HasLower       bool
	HasDigit       bool
	HasSymbol      bool
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

// EvaluatePasswordStrength recognizes dictionary words, leet substitutions,
// keyboard walks, dates, sequences and repeats using zxcvbn. Passwords stay
// local; neither matching sequences nor passwords are retained in the result.
// Labels use a guess-count scale, not the previous character-pool entropy
// thresholds or zxcvbn's online-attack score.
func EvaluatePasswordStrength(password string) StrengthResult {
	result := StrengthResult{Score: StrengthWeak, Label: "Weak"}
	if password == "" {
		return result
	}
	result.Length = utf8.RuneCountInString(password)
	for _, r := range password {
		switch {
		case unicode.IsLower(r):
			result.HasLower = true
		case unicode.IsUpper(r):
			result.HasUpper = true
		case unicode.IsDigit(r):
			result.HasDigit = true
		default:
			result.HasSymbol = true
		}
	}
	if !utf8.ValidString(password) {
		result.Label = "Not rated"
		result.Unavailable = true
		return result
	}
	result.AnalyzedLength = min(result.Length, maxStrengthRunes)
	count := 0
	for i := range password {
		if count == maxStrengthRunes {
			password = password[:i]
			break
		}
		count++
	}
	guesses := zxcvbn.PasswordStrength(password, nil).Guesses
	bits := math.Log2(max(1, guesses))

	// Knowing the generator's small word list gives an attacker an additional
	// strategy. Use its search space only as a ceiling: recognizing the shape
	// of a passphrase cannot prove the words were chosen independently.
	if ceiling, ok := passphraseGuessCeiling(password); ok {
		bits = min(bits, ceiling)
	}

	result.Score = guessBitsToScore(bits)
	result.Label = result.Score.String()
	result.GuessBits = math.Round(bits*10) / 10
	return result
}

// dicewareWordSet indexes the generator's word list for membership checks.
var dicewareWordSet = func() map[string]bool {
	s := make(map[string]bool, len(dicewareWordList))
	for _, w := range dicewareWordList {
		s[w] = true
	}
	return s
}()

func passphraseGuessCeiling(password string) (float64, bool) {
	words := strings.FieldsFunc(password, func(r rune) bool {
		return !unicode.IsLetter(r)
	})
	if len(words) < 3 {
		return 0, false
	}
	for _, w := range words {
		if !dicewareWordSet[strings.ToLower(w)] {
			return 0, false
		}
	}
	return float64(len(words)) * math.Log2(float64(len(dicewareWordList))), true
}

func guessBitsToScore(bits float64) StrengthLevel {
	// zxcvbn uses 10 guesses per unmatched character, regardless of charset.
	// Rate on that scale: <1e6 Weak, <1e8 Fair, <1e12 Good, <1e16 Strong.
	// These labels are guidance, not claims about entropy or cracking time.
	switch {
	case bits < 6*math.Log2(10):
		return StrengthWeak
	case bits < 8*math.Log2(10):
		return StrengthFair
	case bits < 12*math.Log2(10):
		return StrengthGood
	case bits < 16*math.Log2(10):
		return StrengthStrong
	default:
		return StrengthExcellent
	}
}

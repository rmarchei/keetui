package keepass

import (
	"strings"
	"testing"
)

func TestGeneratePassphrase(t *testing.T) {
	passphrase, err := GeneratePassphrase(6, "-")
	if err != nil {
		t.Fatalf("GeneratePassphrase: %v", err)
	}

	words := strings.Split(passphrase, "-")
	if len(words) != 6 {
		t.Errorf("word count = %d, want 6", len(words))
	}

	// Each word should be non-empty
	for i, w := range words {
		if w == "" {
			t.Errorf("word %d is empty", i)
		}
	}
}

func TestGeneratePassphraseMinWords(t *testing.T) {
	// Should enforce minimum of 3 words
	passphrase, err := GeneratePassphrase(1, " ")
	if err != nil {
		t.Fatalf("GeneratePassphrase: %v", err)
	}

	words := strings.Split(passphrase, " ")
	if len(words) < 3 {
		t.Errorf("word count = %d, want at least 3", len(words))
	}
}

func TestGeneratePassphraseMaxWords(t *testing.T) {
	// Should enforce maximum of 10 words
	passphrase, err := GeneratePassphrase(20, " ")
	if err != nil {
		t.Fatalf("GeneratePassphrase: %v", err)
	}

	words := strings.Split(passphrase, " ")
	if len(words) > 10 {
		t.Errorf("word count = %d, want at most 10", len(words))
	}
}

func TestGeneratePassphraseUniqueness(t *testing.T) {
	// Generate two passphrases; they should (almost certainly) be different
	p1, err := GeneratePassphrase(6, "-")
	if err != nil {
		t.Fatalf("GeneratePassphrase: %v", err)
	}
	p2, err := GeneratePassphrase(6, "-")
	if err != nil {
		t.Fatalf("GeneratePassphrase: %v", err)
	}
	if p1 == p2 {
		t.Error("two generated passphrases are identical (extremely unlikely)")
	}
}

func TestGeneratePassphraseCustomSeparator(t *testing.T) {
	passphrase, err := GeneratePassphrase(4, ".")
	if err != nil {
		t.Fatalf("GeneratePassphrase: %v", err)
	}

	if !strings.Contains(passphrase, ".") {
		t.Error("passphrase does not contain custom separator '.'")
	}
}

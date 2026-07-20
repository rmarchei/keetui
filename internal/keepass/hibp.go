package keepass

import (
	"bufio"
	"crypto/sha1"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const hibpAPIURL = "https://api.pwnedpasswords.com/range/"

// HIBPResult holds the result of a breach check.
type HIBPResult struct {
	Breached bool
	Count    int
	Err      error
}

// CheckHIBP checks a password against the Have I Been Pwned API using
// k-anonymity (only the first 5 characters of the SHA-1 hash are sent).
func CheckHIBP(password string) HIBPResult {
	return checkHIBPWithURL(password, hibpAPIURL)
}

// checkHIBPWithURL is the testable core of CheckHIBP, accepting a custom base URL.
func checkHIBPWithURL(password, baseURL string) HIBPResult {
	hash := fmt.Sprintf("%X", sha1.Sum([]byte(password)))
	prefix := hash[:5]
	suffix := hash[5:]

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest(http.MethodGet, baseURL+prefix, nil)
	if err != nil {
		return HIBPResult{Err: fmt.Errorf("HIBP request failed: %w", err)}
	}
	// Padding equalizes response sizes so a passive observer cannot infer
	// which hash range was queried from the TLS response length.
	req.Header.Set("Add-Padding", "true")
	resp, err := client.Do(req)
	if err != nil {
		return HIBPResult{Err: fmt.Errorf("HIBP request failed: %w", err)}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return HIBPResult{Err: fmt.Errorf("HIBP returned status %d", resp.StatusCode)}
	}

	// Limit read to 1MB to prevent abuse
	reader := io.LimitReader(resp.Body, 1<<20)
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		if strings.EqualFold(parts[0], suffix) {
			count, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
			// Padding entries carry a count of 0 and are not real breaches.
			if count > 0 {
				return HIBPResult{Breached: true, Count: count}
			}
			return HIBPResult{Breached: false, Count: 0}
		}
	}
	if err := scanner.Err(); err != nil {
		return HIBPResult{Err: fmt.Errorf("reading HIBP response: %w", err)}
	}

	return HIBPResult{Breached: false, Count: 0}
}

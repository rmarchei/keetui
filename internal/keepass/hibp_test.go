package keepass

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheckHIBPBreached(t *testing.T) {
	// SHA-1("password123") = CBFDAC6008F9CAB4083784CBD1874F76618D2A97
	// prefix = CBFDA, suffix = C6008F9CAB4083784CBD1874F76618D2A97
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintln(w, "0000000000000000000000000000000000000:1")
		_, _ = fmt.Fprintln(w, "C6008F9CAB4083784CBD1874F76618D2A97:42")
		_, _ = fmt.Fprintln(w, "1111111111111111111111111111111111111:5")
	}))
	defer srv.Close()

	result := checkHIBPWithURL("password123", srv.URL+"/")
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if !result.Breached {
		t.Error("expected Breached=true")
	}
	if result.Count != 42 {
		t.Errorf("Count = %d, want 42", result.Count)
	}
}

func TestCheckHIBPSafe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintln(w, "0000000000000000000000000000000000000:1")
		_, _ = fmt.Fprintln(w, "1111111111111111111111111111111111111:5")
		_, _ = fmt.Fprintln(w, "2222222222222222222222222222222222222:3")
	}))
	defer srv.Close()

	result := checkHIBPWithURL("password123", srv.URL+"/")
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if result.Breached {
		t.Error("expected Breached=false")
	}
	if result.Count != 0 {
		t.Errorf("Count = %d, want 0", result.Count)
	}
}

func TestCheckHIBPNetworkError(t *testing.T) {
	result := checkHIBPWithURL("password123", "http://127.0.0.1:1/")
	if result.Err == nil {
		t.Error("expected error for unreachable server")
	}
}

func TestCheckHIBPNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	result := checkHIBPWithURL("password123", srv.URL+"/")
	if result.Err == nil {
		t.Error("expected error for 429 status")
	}
}

func TestCheckHIBPMalformedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintln(w, "garbage data without colons")
		_, _ = fmt.Fprintln(w, "more nonsense")
	}))
	defer srv.Close()

	result := checkHIBPWithURL("password123", srv.URL+"/")
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if result.Breached {
		t.Error("expected Breached=false for malformed response")
	}
}

func TestCheckHIBPCaseInsensitive(t *testing.T) {
	// Return the suffix in lowercase to verify case-insensitive matching
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintln(w, "c6008f9cab4083784cbd1874f76618d2a97:7")
	}))
	defer srv.Close()

	result := checkHIBPWithURL("password123", srv.URL+"/")
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if !result.Breached {
		t.Error("expected Breached=true with case-insensitive match")
	}
	if result.Count != 7 {
		t.Errorf("Count = %d, want 7", result.Count)
	}
}

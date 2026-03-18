package traffic

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseDestination(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "host port", input: "127.0.0.1:8080", wantErr: false},
		{name: "http url", input: "http://127.0.0.1:8080", wantErr: false},
		{name: "https url", input: "https://example.com:443", wantErr: false},
		{name: "invalid no port", input: "example.com", wantErr: true},
		{name: "invalid scheme", input: "grpc://127.0.0.1:8080", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseDestination(tt.input)
			if tt.wantErr && err == nil {
				t.Fatalf("expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestNewReverseProxy(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	target, err := parseDestination(upstream.URL)
	if err != nil {
		t.Fatalf("parse destination failed: %v", err)
	}

	proxy := newReverseProxy(target)
	req := httptest.NewRequest(http.MethodGet, "http://local.test/demo", nil)
	w := httptest.NewRecorder()
	proxy.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != "ok" {
		t.Fatalf("expected ok body, got %s", w.Body.String())
	}
}

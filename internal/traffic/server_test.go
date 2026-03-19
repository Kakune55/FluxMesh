package traffic

import (
	"encoding/json"
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

func TestNewReverseProxyRewriteSecurityHeaders(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload := map[string]string{
			"forwarded":            r.Header.Get("Forwarded"),
			"x_forwarded_for":      r.Header.Get("X-Forwarded-For"),
			"x_forwarded_host":     r.Header.Get("X-Forwarded-Host"),
			"x_forwarded_proto":    r.Header.Get("X-Forwarded-Proto"),
			"upstream_host_header": r.Host,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer upstream.Close()

	target, err := parseDestination(upstream.URL)
	if err != nil {
		t.Fatalf("parse destination failed: %v", err)
	}

	proxy := newReverseProxy(target)
	if proxy.Director != nil {
		t.Fatalf("expected Director=nil when Rewrite is set")
	}
	if proxy.Rewrite == nil {
		t.Fatalf("expected Rewrite to be set")
	}

	req := httptest.NewRequest(http.MethodGet, "http://mesh.local/check", nil)
	req.Host = "pay.example.com"
	req.Header.Set("Forwarded", "for=1.2.3.4;proto=http")
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	req.Header.Set("X-Forwarded-Host", "spoof.example.com")
	req.Header.Set("X-Forwarded-Proto", "http")

	w := httptest.NewRecorder()
	proxy.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", w.Code, w.Body.String())
	}

	var payload map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if payload["forwarded"] != "" {
		t.Fatalf("expected Forwarded to be rebuilt/empty in upstream, got %q", payload["forwarded"])
	}
	if payload["x_forwarded_for"] == "1.2.3.4" {
		t.Fatalf("expected spoofed X-Forwarded-For to be replaced")
	}
	if payload["x_forwarded_host"] != "pay.example.com" {
		t.Fatalf("expected X-Forwarded-Host=pay.example.com, got %q", payload["x_forwarded_host"])
	}
	if payload["upstream_host_header"] != target.Host {
		t.Fatalf("expected upstream Host header %q, got %q", target.Host, payload["upstream_host_header"])
	}
}

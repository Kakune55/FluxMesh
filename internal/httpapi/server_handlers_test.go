package httpapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseExpectedRevision(t *testing.T) {
	tests := []struct {
		name      string
		target    string
		fallback  int64
		want      int64
		wantError bool
	}{
		{name: "query wins", target: "/api/v1/services/svc?resource_version=12", fallback: 5, want: 12},
		{name: "fallback used", target: "/api/v1/services/svc", fallback: 7, want: 7},
		{name: "missing revision", target: "/api/v1/services/svc", fallback: 0, wantError: true},
		{name: "invalid revision", target: "/api/v1/services/svc?resource_version=abc", fallback: 0, wantError: true},
		{name: "non-positive revision", target: "/api/v1/services/svc?resource_version=0", fallback: 0, wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPut, tt.target, nil)
			got, err := parseExpectedRevision(req, tt.fallback)
			if tt.wantError {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("expected %d, got %d", tt.want, got)
			}
		})
	}
}

func TestHandleServiceByNameValidation(t *testing.T) {
	s := &Server{}

	t.Run("method not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/services/payment-svc", bytes.NewBufferString("{}"))
		w := httptest.NewRecorder()
		s.handleServiceByName(w, req)
		if w.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected %d, got %d", http.StatusMethodNotAllowed, w.Code)
		}
	})

	t.Run("name mismatch", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPut, "/api/v1/services/payment-svc", bytes.NewBufferString(`{"name":"other-svc","routes":[{"path_prefix":"/","destination":"v1"}]}`))
		w := httptest.NewRecorder()
		s.handleServiceByName(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, w.Code)
		}
	})

	t.Run("missing resource version", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPut, "/api/v1/services/payment-svc", bytes.NewBufferString(`{"name":"payment-svc","routes":[{"path_prefix":"/","destination":"v1"}]}`))
		w := httptest.NewRecorder()
		s.handleServiceByName(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, w.Code)
		}
	})
}

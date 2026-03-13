package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"fluxmesh/internal/model"
	"fluxmesh/internal/registry"
	"fluxmesh/internal/testutil/etcdtest"
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

func TestHandleServiceByNameCASAndDelete(t *testing.T) {
	emb := etcdtest.Start(t)
	s := &Server{services: registry.NewServices(emb.Client)}

	seed := model.ServiceConfig{
		Name:      "payment-svc",
		Namespace: "prod",
		Version:   "v1",
		Routes: []model.ServiceRoute{
			{PathPrefix: "/", Destination: "payment-v1", Weight: 100},
		},
	}
	if err := s.services.Put(t.Context(), seed); err != nil {
		t.Fatalf("seed put failed: %v", err)
	}

	t.Run("get not found", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/services/missing-svc", nil)
		w := httptest.NewRecorder()
		s.handleServiceByName(w, req)
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected %d, got %d", http.StatusNotFound, w.Code)
		}
	})

	var current model.ServiceConfig
	t.Run("get existing", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/services/payment-svc", nil)
		w := httptest.NewRecorder()
		s.handleServiceByName(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, w.Code)
		}
		if err := json.Unmarshal(w.Body.Bytes(), &current); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if current.ResourceVersion <= 0 {
			t.Fatalf("expected resource_version > 0, got %d", current.ResourceVersion)
		}
	})

	t.Run("put success then conflict", func(t *testing.T) {
		bodyOK := bytes.NewBufferString(`{"name":"payment-svc","namespace":"prod","version":"v2","routes":[{"path_prefix":"/","destination":"payment-v2","weight":100}]}`)
		reqOK := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/v1/services/payment-svc?resource_version=%d", current.ResourceVersion), bodyOK)
		wOK := httptest.NewRecorder()
		s.handleServiceByName(wOK, reqOK)
		if wOK.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, wOK.Code)
		}

		bodyConflict := bytes.NewBufferString(`{"name":"payment-svc","namespace":"prod","version":"v3","routes":[{"path_prefix":"/","destination":"payment-v3","weight":100}]}`)
		reqConflict := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/v1/services/payment-svc?resource_version=%d", current.ResourceVersion), bodyConflict)
		wConflict := httptest.NewRecorder()
		s.handleServiceByName(wConflict, reqConflict)
		if wConflict.Code != http.StatusConflict {
			t.Fatalf("expected %d, got %d", http.StatusConflict, wConflict.Code)
		}

		var conflictPayload map[string]any
		if err := json.Unmarshal(wConflict.Body.Bytes(), &conflictPayload); err != nil {
			t.Fatalf("failed to decode conflict payload: %v", err)
		}
		rv, ok := conflictPayload["current_resource_version"].(float64)
		if !ok || int64(rv) <= current.ResourceVersion {
			t.Fatalf("expected newer current_resource_version, got %+v", conflictPayload["current_resource_version"])
		}
	})

	t.Run("delete and get not found", func(t *testing.T) {
		reqDel := httptest.NewRequest(http.MethodDelete, "/api/v1/services/payment-svc", nil)
		wDel := httptest.NewRecorder()
		s.handleServiceByName(wDel, reqDel)
		if wDel.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, wDel.Code)
		}

		reqGet := httptest.NewRequest(http.MethodGet, "/api/v1/services/payment-svc", nil)
		wGet := httptest.NewRecorder()
		s.handleServiceByName(wGet, reqGet)
		if wGet.Code != http.StatusNotFound {
			t.Fatalf("expected %d, got %d", http.StatusNotFound, wGet.Code)
		}
	})
}

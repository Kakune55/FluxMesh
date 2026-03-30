package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"fluxmesh/internal/registry"
	"fluxmesh/internal/softkv"
	"fluxmesh/internal/testutil/etcdtest"
)

func TestHandleTrafficPlanExtraBranches(t *testing.T) {
	t.Run("method not allowed", func(t *testing.T) {
		s := &Server{}
		req := httptest.NewRequest(http.MethodPost, "/api/v1/traffic/plan", nil)
		w := httptest.NewRecorder()
		s.handleTrafficPlan(w, req)
		if w.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected %d, got %d", http.StatusMethodNotAllowed, w.Code)
		}
	})

	t.Run("services nil", func(t *testing.T) {
		s := &Server{}
		req := httptest.NewRequest(http.MethodGet, "/api/v1/traffic/plan", nil)
		w := httptest.NewRecorder()
		s.handleTrafficPlan(w, req)
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, w.Code)
		}
	})

	t.Run("services list error", func(t *testing.T) {
		emb := etcdtest.Start(t)
		s := &Server{services: registry.NewServices(emb.Client)}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/traffic/plan", nil).WithContext(ctx)
		w := httptest.NewRecorder()
		s.handleTrafficPlan(w, req)
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, w.Code)
		}
	})

	t.Run("build plan error", func(t *testing.T) {
		emb := etcdtest.Start(t)
		s := &Server{services: registry.NewServices(emb.Client)}
		// Write invalid service directly to etcd to trigger BuildPlan validation error in handler.
		_, err := emb.Client.Put(t.Context(), "/mesh/services/bad", `{"name":"bad","traffic_policy":{"listener":{"port":70000}},"routes":[{"path_prefix":"/","destination":"x","weight":100}]}`)
		if err != nil {
			t.Fatalf("seed invalid service failed: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/api/v1/traffic/plan", nil)
		w := httptest.NewRecorder()
		s.handleTrafficPlan(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d body=%s", http.StatusBadRequest, w.Code, w.Body.String())
		}
	})
}

func TestHandleSoftKVStatsAndByKeyExtraBranches(t *testing.T) {
	t.Run("softkv stats method not allowed", func(t *testing.T) {
		s := &Server{softStore: softkv.NewStore()}
		req := httptest.NewRequest(http.MethodPost, "/api/v1/softkv/stats", nil)
		w := httptest.NewRecorder()
		s.handleSoftKVStats(w, req)
		if w.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected %d, got %d", http.StatusMethodNotAllowed, w.Code)
		}
	})

	t.Run("softkv stats nil store", func(t *testing.T) {
		s := &Server{}
		req := httptest.NewRequest(http.MethodGet, "/api/v1/softkv/stats", nil)
		w := httptest.NewRecorder()
		s.handleSoftKVStats(w, req)
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, w.Code)
		}
	})

	t.Run("softkv by key method not allowed", func(t *testing.T) {
		s := &Server{softStore: softkv.NewStore()}
		req := httptest.NewRequest(http.MethodPost, "/api/v1/softkv/key", nil)
		w := httptest.NewRecorder()
		s.handleSoftKVByKey(w, req)
		if w.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected %d, got %d", http.StatusMethodNotAllowed, w.Code)
		}
	})

	t.Run("softkv by key nil store", func(t *testing.T) {
		s := &Server{}
		req := httptest.NewRequest(http.MethodGet, "/api/v1/softkv/key", nil)
		w := httptest.NewRecorder()
		s.handleSoftKVByKey(w, req)
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, w.Code)
		}
	})

	t.Run("softkv by key missing path", func(t *testing.T) {
		s := &Server{softStore: softkv.NewStore()}
		req := httptest.NewRequest(http.MethodGet, "/api/v1/softkv/", nil)
		w := httptest.NewRecorder()
		s.handleSoftKVByKey(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, w.Code)
		}
	})

	t.Run("softkv by key invalid escape", func(t *testing.T) {
		s := &Server{softStore: softkv.NewStore()}
		req := httptest.NewRequest(http.MethodGet, "/api/v1/softkv/x", nil)
		req.URL.Path = "/api/v1/softkv/%ZZ"
		w := httptest.NewRecorder()
		s.handleSoftKVByKey(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d body=%s", http.StatusBadRequest, w.Code, w.Body.String())
		}
	})
}

func TestHandleTrafficMatchExtraBranches(t *testing.T) {
	t.Run("method not allowed", func(t *testing.T) {
		s := &Server{}
		req := httptest.NewRequest(http.MethodPost, "/api/v1/traffic/match", nil)
		w := httptest.NewRecorder()
		s.handleTrafficMatch(w, req)
		if w.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected %d, got %d", http.StatusMethodNotAllowed, w.Code)
		}
	})

	t.Run("services nil", func(t *testing.T) {
		s := &Server{}
		req := httptest.NewRequest(http.MethodGet, "/api/v1/traffic/match?port=18080&host=a.com", nil)
		w := httptest.NewRecorder()
		s.handleTrafficMatch(w, req)
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, w.Code)
		}
	})
}

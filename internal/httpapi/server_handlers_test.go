package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"fluxmesh/internal/model"
	"fluxmesh/internal/registry"
	"fluxmesh/internal/softkv"
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
		req := httptest.NewRequest(http.MethodPut, "/api/v1/services/payment-svc", bytes.NewBufferString(`{"name":"other-svc","traffic_policy":{"listener":{"port":18080}},"routes":[{"path_prefix":"/","destination":"v1"}]}`))
		w := httptest.NewRecorder()
		s.handleServiceByName(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, w.Code)
		}
	})

	t.Run("missing resource version", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPut, "/api/v1/services/payment-svc", bytes.NewBufferString(`{"name":"payment-svc","traffic_policy":{"listener":{"port":18080}},"routes":[{"path_prefix":"/","destination":"v1"}]}`))
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
		TrafficPolicy: model.ServiceTrafficPolicy{
			Listener: model.ListenerPolicy{Port: 18080},
		},
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
		bodyOK := bytes.NewBufferString(`{"name":"payment-svc","namespace":"prod","version":"v2","traffic_policy":{"listener":{"port":18080}},"routes":[{"path_prefix":"/","destination":"payment-v2","weight":100}]}`)
		reqOK := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/v1/services/payment-svc?resource_version=%d", current.ResourceVersion), bodyOK)
		reqOK.Header.Set("X-Operator", "qa-bot")
		wOK := httptest.NewRecorder()
		s.handleServiceByName(wOK, reqOK)
		if wOK.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, wOK.Code)
		}

		var updated model.ServiceConfig
		if err := json.Unmarshal(wOK.Body.Bytes(), &updated); err != nil {
			t.Fatalf("failed to decode update response: %v", err)
		}
		if updated.UpdatedBy != "qa-bot" {
			t.Fatalf("expected updated_by=qa-bot, got %s", updated.UpdatedBy)
		}
		if updated.UpdatedAt == "" {
			t.Fatalf("expected updated_at to be set")
		}

		bodyConflict := bytes.NewBufferString(`{"name":"payment-svc","namespace":"prod","version":"v3","traffic_policy":{"listener":{"port":18080}},"routes":[{"path_prefix":"/","destination":"payment-v3","weight":100}]}`)
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

		cfg, ok := conflictPayload["current_config"].(map[string]any)
		if !ok {
			t.Fatalf("expected current_config object in conflict payload")
		}
		if cfg["name"] != "payment-svc" {
			t.Fatalf("expected current_config.name=payment-svc, got %+v", cfg["name"])
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

func TestHandleSoftKV(t *testing.T) {
	store := softkv.NewStore()
	s := &Server{softStore: store}

	_, err := store.Put(t.Context(), "metrics/nodes/node-1", map[string]any{"cpu": 12.3}, 10*time.Second, "node-1")
	if err != nil {
		t.Fatalf("seed softkv failed: %v", err)
	}

	t.Run("method not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/softkv", nil)
		w := httptest.NewRecorder()
		s.handleSoftKV(w, req)
		if w.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected %d, got %d", http.StatusMethodNotAllowed, w.Code)
		}
	})

	t.Run("list with prefix", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/softkv?prefix=metrics/nodes/", nil)
		w := httptest.NewRecorder()
		s.handleSoftKV(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, w.Code)
		}

		var items []softkv.Entry
		if err := json.Unmarshal(w.Body.Bytes(), &items); err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		if len(items) != 1 {
			t.Fatalf("expected 1 entry, got %d", len(items))
		}
		if items[0].Key != "metrics/nodes/node-1" {
			t.Fatalf("unexpected key: %s", items[0].Key)
		}
	})

	t.Run("get by encoded key", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/softkv/metrics%2Fnodes%2Fnode-1", nil)
		w := httptest.NewRecorder()
		s.handleSoftKVByKey(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, w.Code)
		}

		var item softkv.Entry
		if err := json.Unmarshal(w.Body.Bytes(), &item); err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		if item.Key != "metrics/nodes/node-1" {
			t.Fatalf("unexpected key: %s", item.Key)
		}
	})

	t.Run("get by key not found", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/softkv/missing-key", nil)
		w := httptest.NewRecorder()
		s.handleSoftKVByKey(w, req)
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected %d, got %d", http.StatusNotFound, w.Code)
		}
	})

	t.Run("stats", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/softkv/stats", nil)
		w := httptest.NewRecorder()
		s.handleSoftKVStats(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, w.Code)
		}

		var stats softkv.StoreStats
		if err := json.Unmarshal(w.Body.Bytes(), &stats); err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		if stats.PutTotal == 0 {
			t.Fatalf("expected put_total > 0, got %d", stats.PutTotal)
		}
		if stats.LiveEntries == 0 {
			t.Fatalf("expected live_entries > 0, got %d", stats.LiveEntries)
		}
	})
}

func TestHandleNodesAttachSysLoadFromSoftKV(t *testing.T) {
	emb := etcdtest.Start(t)
	nodesSvc := registry.NewService(emb.Client)
	store := softkv.NewStore()
	s := &Server{nodes: nodesSvc, softStore: store}

	node := model.Node{
		ID:      "node-a",
		IP:      "10.0.0.1",
		Version: "v1.0.0",
		NodeStatus: model.NodeStatus{
			NodeRole:   "agent",
			NodeStatus: "Ready",
		},
	}

	keepAliveCtx, cancel := context.WithCancel(t.Context())
	defer cancel()

	if _, _, err := nodesSvc.RegisterWithLease(t.Context(), keepAliveCtx, node, 30); err != nil {
		t.Fatalf("register node failed: %v", err)
	}

	_, err := store.Put(t.Context(), "metrics/nodes/node-a", map[string]any{
		"CPUUsage":      12.34,
		"MemoryUsage":   56.78,
		"SystemLoad1m":  0.9,
		"SystemLoad5m":  0.7,
		"SystemLoad15m": 0.5,
	}, 30*time.Second, "node-a")
	if err != nil {
		t.Fatalf("seed softkv failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes", nil)
	w := httptest.NewRecorder()
	s.handleNodes(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, w.Code)
	}

	var nodes []model.Node
	if err := json.Unmarshal(w.Body.Bytes(), &nodes); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}

	if nodes[0].ID != "node-a" {
		t.Fatalf("unexpected node id: %s", nodes[0].ID)
	}
	if nodes[0].SysLoad.CPUUsage != 12.34 {
		t.Fatalf("expected cpu_usage=12.34, got %v", nodes[0].SysLoad.CPUUsage)
	}
	if nodes[0].SysLoad.MemoryUsage != 56.78 {
		t.Fatalf("expected memory_usage=56.78, got %v", nodes[0].SysLoad.MemoryUsage)
	}
}

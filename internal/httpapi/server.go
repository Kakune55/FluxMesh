package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"fluxmesh/internal/logx"
	"fluxmesh/internal/model"
	"fluxmesh/internal/registry"
	"fluxmesh/internal/softkv"
	"fluxmesh/internal/traffic"
)

type Server struct {
	httpServer *http.Server
	nodes      *registry.Service
	services   *registry.Services
	softStore  *softkv.Store
	version    string
}

func NewServer(addr string, nodes *registry.Service, services *registry.Services, softStore *softkv.Store, version string) *Server {
	s := &Server{nodes: nodes, services: services, softStore: softStore, version: version}
	mux := http.NewServeMux()
	// 管理接口
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/api/v1/cluster/status", s.handleClusterStatus)
	mux.HandleFunc("/api/v1/nodes", s.handleNodes)
	mux.HandleFunc("/api/v1/nodes/", s.handleNodeByID)
	mux.HandleFunc("/api/v1/services", s.handleServices)
	mux.HandleFunc("/api/v1/services/", s.handleServiceByName)
	mux.HandleFunc("/api/v1/traffic/plan", s.handleTrafficPlan)
	mux.HandleFunc("/api/v1/traffic/match", s.handleTrafficMatch)
	mux.HandleFunc("/api/v1/softkv", s.handleSoftKV)
	mux.HandleFunc("/api/v1/softkv/stats", s.handleSoftKVStats)
	mux.HandleFunc("/api/v1/softkv/", s.handleSoftKVByKey)

	s.httpServer = &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	return s
}

func (s *Server) Start() error {
	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logx.Error("HTTP 服务运行失败", "err", err)
		}
	}()
	logx.Info("管理面 HTTP 服务已启动", "addr", s.httpServer.Addr)
	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "UP",
		"version": s.version,
	})
}

func (s *Server) handleNodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	nodes, err := s.nodes.ListNodesWithEtcdRole(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	nodes = s.attachNodeMetricsFromSoftKV(r.Context(), nodes)
	writeJSON(w, http.StatusOK, nodes)
}

func (s *Server) attachNodeMetricsFromSoftKV(ctx context.Context, nodes []model.Node) []model.Node {
	if s.softStore == nil || len(nodes) == 0 {
		return nodes
	}

	entries := s.softStore.List(ctx, "metrics/nodes/")
	if len(entries) == 0 {
		return nodes
	}

	metricByNode := make(map[string]model.SysLoad, len(entries))
	for _, entry := range entries {
		nodeID := strings.TrimSpace(strings.TrimPrefix(entry.Key, "metrics/nodes/"))
		if nodeID == "" {
			nodeID = strings.TrimSpace(entry.SourceID)
		}
		if nodeID == "" {
			continue
		}

		load, ok := decodeSysLoadFromValue(entry.Value)
		if !ok {
			continue
		}
		metricByNode[nodeID] = load
	}

	for i := range nodes {
		if load, ok := metricByNode[nodes[i].ID]; ok {
			nodes[i].SysLoad = load
		}
	}

	return nodes
}

func decodeSysLoadFromValue(value any) (model.SysLoad, bool) {
	raw, err := json.Marshal(value)
	if err != nil {
		return model.SysLoad{}, false
	}

	var payload struct {
		CPUUsage      float64 `json:"CPUUsage"`
		MemoryUsage   float64 `json:"MemoryUsage"`
		SystemLoad1m  float64 `json:"SystemLoad1m"`
		SystemLoad5m  float64 `json:"SystemLoad5m"`
		SystemLoad15m float64 `json:"SystemLoad15m"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return model.SysLoad{}, false
	}

	return model.SysLoad{
		CPUUsage:      payload.CPUUsage,
		MemoryUsage:   payload.MemoryUsage,
		SystemLoad1m:  payload.SystemLoad1m,
		SystemLoad5m:  payload.SystemLoad5m,
		SystemLoad15m: payload.SystemLoad15m,
	}, true
}

func (s *Server) handleClusterStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	status, err := s.nodes.ClusterStatus(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleNodeByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/nodes/")
	id = strings.TrimSpace(id)
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "node id is required"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		node, err := s.nodes.GetNode(r.Context(), id)
		if err != nil {
			if errors.Is(err, registry.ErrNodeNotFound) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "node not found"})
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, node)
	case http.MethodDelete:
		force := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("force")), "true")
		result, err := s.nodes.EvictNode(r.Context(), id, force)
		if err != nil {
			if errors.Is(err, registry.ErrNodeNotFound) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "node not found"})
				return
			}
			if errors.Is(err, registry.ErrLeaderEvictRequiresForce) {
				writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, result)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (s *Server) handleServices(w http.ResponseWriter, r *http.Request) {
	if s.services == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "services storage not initialized"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		services, err := s.services.List(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, services)
	case http.MethodPost:
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "failed to read request body"})
			return
		}

		var cfg model.ServiceConfig
		if err := json.Unmarshal(body, &cfg); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json payload"})
			return
		}

		applyOperatorMetadata(&cfg, r)
		cfg.ApplyDefaults()

		if err := cfg.Validate(); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}

		if err := s.services.Put(r.Context(), cfg); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		writeJSON(w, http.StatusCreated, cfg)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (s *Server) handleServiceByName(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/api/v1/services/")
	name = strings.TrimSpace(name)
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "service name is required"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		item, err := s.services.Get(r.Context(), name)
		if err != nil {
			if errors.Is(err, registry.ErrServiceNotFound) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "service not found"})
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		writeJSON(w, http.StatusOK, item)
	case http.MethodPut:
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "failed to read request body"})
			return
		}

		var cfg model.ServiceConfig
		if err := json.Unmarshal(body, &cfg); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json payload"})
			return
		}

		if cfg.Name != "" && cfg.Name != name {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "service name in path and payload must match"})
			return
		}
		cfg.Name = name
		applyOperatorMetadata(&cfg, r)
		cfg.ApplyDefaults()

		if err := cfg.Validate(); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}

		rev, err := parseExpectedRevision(r, cfg.ResourceVersion)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}

		updated, err := s.services.UpdateWithRevision(r.Context(), name, cfg, rev)
		if err != nil {
			if errors.Is(err, registry.ErrServiceConflict) {
				latest, getErr := s.services.Get(r.Context(), name)
				if getErr == nil {
					writeJSON(w, http.StatusConflict, map[string]any{
						"error":                    err.Error(),
						"current_resource_version": latest.ResourceVersion,
						"current_config":           latest,
					})
					return
				}
				writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
				return
			}
			if errors.Is(err, registry.ErrServiceNotFound) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "service not found"})
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		writeJSON(w, http.StatusOK, updated)
	case http.MethodDelete:
		err := s.services.Delete(r.Context(), name)
		if err != nil {
			if errors.Is(err, registry.ErrServiceNotFound) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "service not found"})
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "name": name})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (s *Server) handleTrafficPlan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if s.services == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "services storage not initialized"})
		return
	}

	items, err := s.services.List(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	plan, err := traffic.BuildPlan(items)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"listeners": plan.Listeners()})
}

func (s *Server) handleTrafficMatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if s.services == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "services storage not initialized"})
		return
	}

	addr := strings.TrimSpace(r.URL.Query().Get("addr"))
	if addr == "" {
		addr = "0.0.0.0"
	}

	portRaw := strings.TrimSpace(r.URL.Query().Get("port"))
	port, err := strconv.Atoi(portRaw)
	if err != nil || port < 1 || port > 65535 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "port must be between 1 and 65535"})
		return
	}

	host := strings.TrimSpace(r.URL.Query().Get("host"))
	if host == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "host is required"})
		return
	}

	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		path = "/"
	}

	items, err := s.services.List(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	plan, err := traffic.BuildPlan(items)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	result, ok := plan.Match(addr, port, host, path)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no route matched"})
		return
	}

	resolved, err := plan.ResolveDestination(result.Destination)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"listener":             result.Listener,
		"service_name":         result.ServiceName,
		"destination":          result.Destination,
		"resolved_destination": resolved,
		"path_prefix":          result.PathPrefix,
	})
}

func (s *Server) handleSoftKV(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if s.softStore == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "softkv storage not initialized"})
		return
	}

	prefix := strings.TrimSpace(r.URL.Query().Get("prefix"))
	items := s.softStore.List(r.Context(), prefix)
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleSoftKVStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if s.softStore == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "softkv storage not initialized"})
		return
	}

	writeJSON(w, http.StatusOK, s.softStore.Stats())
}

func (s *Server) handleSoftKVByKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if s.softStore == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "softkv storage not initialized"})
		return
	}

	raw := strings.TrimPrefix(r.URL.Path, "/api/v1/softkv/")
	raw = strings.TrimSpace(raw)
	if raw == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "softkv key is required"})
		return
	}

	key, err := url.PathUnescape(raw)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid softkv key"})
		return
	}

	entry, ok := s.softStore.Get(r.Context(), key)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "softkv key not found"})
		return
	}

	writeJSON(w, http.StatusOK, entry)
}

func parseExpectedRevision(r *http.Request, fallback int64) (int64, error) {
	revRaw := strings.TrimSpace(r.URL.Query().Get("resource_version"))
	if revRaw == "" {
		if fallback > 0 {
			return fallback, nil
		}
		return 0, fmt.Errorf("resource_version is required for update")
	}

	rev, err := strconv.ParseInt(revRaw, 10, 64)
	if err != nil || rev <= 0 {
		return 0, fmt.Errorf("resource_version must be a positive integer")
	}

	return rev, nil
}

func applyOperatorMetadata(cfg *model.ServiceConfig, r *http.Request) {
	operator := strings.TrimSpace(r.Header.Get("X-Operator"))
	if operator == "" {
		operator = strings.TrimSpace(cfg.UpdatedBy)
	}
	if operator == "" {
		operator = "api"
	}
	cfg.UpdatedBy = operator
}

func writeJSON(w http.ResponseWriter, code int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(data)
}

package traffic

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"fluxmesh/internal/logx"
	"fluxmesh/internal/registry"
)

type Server struct {
	services *registry.Services

	mu        sync.RWMutex
	plan      Plan
	listeners map[string]*http.Server
	proxies   map[string]*httputil.ReverseProxy
	transport *http.Transport
	metrics   trafficMetrics
	metricsSampleRate atomic.Uint32
	metricsSampleSeq  atomic.Uint64
}

type trafficMetrics struct {
	requestsTotal      atomic.Uint64
	errorTotal         atomic.Uint64
	retryAttemptsTotal atomic.Uint64
	relayHitTotal      atomic.Uint64
	totalLatencyNs     atomic.Uint64
	relayLatencyNs     atomic.Uint64
}

type TrafficStats struct {
	RequestsTotal      uint64 `json:"requests_total"`
	SuccessTotal       uint64 `json:"success_total"`
	ErrorTotal         uint64 `json:"error_total"`
	RetryAttemptsTotal uint64 `json:"retry_attempts_total"`
	RelayHitTotal      uint64 `json:"relay_hit_total"`
	TotalLatencyNs     uint64 `json:"total_latency_ns"`
	RelayLatencyNs     uint64 `json:"relay_latency_ns"`
}

const (
	headerFluxMeshService     = "X-FluxMesh-Service"
	headerFluxMeshDestination = "X-FluxMesh-Destination"
	headerFluxMeshUpstream    = "X-FluxMesh-Upstream"
	headerFluxMeshHops        = "X-FluxMesh-Hops"
)

// NewServer 创建流量面运行时实例并初始化监听器与代理缓存。
func NewServer(services *registry.Services) *Server {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = 4096
	transport.MaxIdleConnsPerHost = 1024
	transport.MaxConnsPerHost = 0
	transport.DisableCompression = true

	s := &Server{
		services:  services,
		listeners: make(map[string]*http.Server),
		proxies:   make(map[string]*httputil.ReverseProxy),
		transport: transport,
	}
	s.metricsSampleRate.Store(1)
	return s
}

// Start 启动首次监听规划并进入周期性重配置循环。
func (s *Server) Start(ctx context.Context) error {
	if s.services == nil {
		return fmt.Errorf("services registry is required")
	}

	if err := s.refresh(ctx); err != nil {
		return err
	}

	go s.reconcileLoop(ctx)
	return nil
}

// Shutdown 优雅关闭当前所有流量监听服务。
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	listeners := make([]*http.Server, 0, len(s.listeners))
	for _, srv := range s.listeners {
		listeners = append(listeners, srv)
	}
	s.listeners = make(map[string]*http.Server)
	s.mu.Unlock()

	var firstErr error
	for _, srv := range listeners {
		if err := srv.Shutdown(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// reconcileLoop 定时刷新服务配置并收敛监听状态。
func (s *Server) reconcileLoop(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.refresh(ctx); err != nil {
				logx.Warn("流量面监听规划刷新失败", "err", err)
			}
		}
	}
}

// refresh 根据最新服务配置增删监听器并替换当前路由规划。
func (s *Server) refresh(ctx context.Context) error {
	items, err := s.services.List(ctx)
	if err != nil {
		return err
	}

	plan, err := BuildPlan(items)
	if err != nil {
		return err
	}

	desired := make(map[string]ListenerPlan)
	for _, listener := range plan.Listeners() {
		desired[listenerMapKey(listener.Listener.Addr, listener.Listener.Port)] = listener
	}

	s.mu.Lock()
	if s.plan.state != nil {
		// 保留轮询状态，避免每次刷新重置后端选择序列。
		plan.state = s.plan.state
	}
	s.plan = plan

	for key, srv := range s.listeners {
		if _, ok := desired[key]; ok {
			continue
		}
		delete(s.listeners, key)
		go func(server *http.Server, k string) {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := server.Shutdown(shutdownCtx); err != nil {
				logx.Warn("流量监听关闭失败", "listener", k, "err", err)
			}
		}(srv, key)
	}

	for key, listener := range desired {
		if _, ok := s.listeners[key]; ok {
			continue
		}

		addr := net.JoinHostPort(listener.Listener.Addr, strconv.Itoa(listener.Listener.Port))
		handler := s.newListenerHandler(listener.Listener)
		srv := &http.Server{
			Addr:              addr,
			Handler:           handler,
			ReadHeaderTimeout: 5 * time.Second,
		}

		s.listeners[key] = srv
		go func(server *http.Server, bindAddr string) {
			logx.Info("流量监听已启动", "addr", bindAddr)
			if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				logx.Error("流量监听运行失败", "addr", bindAddr, "err", err)
			}
		}(srv, addr)
	}
	s.mu.Unlock()

	return nil
}

// newListenerHandler 处理入站请求并完成匹配、解析和反向代理转发。
func (s *Server) newListenerHandler(listener ListenerKey) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startedAt := time.Now()
		finalStatus, attemptsUsed, relayHit, sampleRate := s.serveRequest(w, r, listener)
		s.recordTrafficMetrics(time.Since(startedAt), finalStatus, attemptsUsed, relayHit, sampleRate)
	})
}

func (s *Server) serveRequest(w http.ResponseWriter, r *http.Request, listener ListenerKey) (int, int, bool, uint32) {
	host := r.Host
	path := r.URL.Path
	if strings.TrimSpace(path) == "" {
		path = "/"
	}

	s.mu.RLock()
	plan := s.plan
	s.mu.RUnlock()

	match, ok := plan.Match(listener.Addr, listener.Port, host, path)
	if !ok {
		http.Error(w, "no route matched", http.StatusNotFound)
		return http.StatusNotFound, 0, false, 0
	}

	policy, _ := plan.ServicePolicy(match.ServiceName)
	sampleRate := effectiveMetricsSampleRate(policy.Observability.MetricsSampleRate, s.metricsSampleRate.Load())
	maxAttempts := effectiveMaxAttempts(policy.Retry.MaxAttempts)
	maxAttempts = applyRetryBudget(maxAttempts, policy.Retry.BudgetRatio)

	incomingHops, err := parseIncomingHops(r.Header.Get(headerFluxMeshHops))
	if err != nil {
		http.Error(w, "invalid relay hops header", http.StatusBadRequest)
		return http.StatusBadRequest, 0, false, sampleRate
	}

	maxHops := effectiveMaxHops(policy.Relay.MaxHops)
	if incomingHops > maxHops {
		http.Error(w, "relay max hops exceeded", http.StatusLoopDetected)
		return http.StatusLoopDetected, 0, false, sampleRate
	}

	if maxAttempts <= 1 {
		resolved, err := plan.ResolveDestination(match.Destination)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return http.StatusBadGateway, 1, false, sampleRate
		}
		relayHit := plan.IsRelayCandidate(match.Destination, resolved)

		proxy, err := s.proxyForDestination(resolved)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return http.StatusBadGateway, 1, relayHit, sampleRate
		}

		r.Header.Set(headerFluxMeshService, match.ServiceName)
		r.Header.Set(headerFluxMeshDestination, match.Destination)
		r.Header.Set(headerFluxMeshUpstream, resolved)
		r.Header.Set(headerFluxMeshHops, strconv.Itoa(incomingHops+1))

		cw := &statusCaptureWriter{ResponseWriter: w, statusCode: http.StatusOK}
		proxy.ServeHTTP(cw, r)
		return cw.statusCode, 1, relayHit, sampleRate
	}

	candidates, err := plan.ResolveDestinationsForAttempts(match.Destination, maxAttempts)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return http.StatusBadGateway, 0, false, sampleRate
	}

	if len(candidates) == 1 {
		resolved := candidates[0]
		relayHit := plan.IsRelayCandidate(match.Destination, resolved)

		proxy, err := s.proxyForDestination(resolved)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return http.StatusBadGateway, 1, relayHit, sampleRate
		}

		r.Header.Set(headerFluxMeshService, match.ServiceName)
		r.Header.Set(headerFluxMeshDestination, match.Destination)
		r.Header.Set(headerFluxMeshUpstream, resolved)
		r.Header.Set(headerFluxMeshHops, strconv.Itoa(incomingHops+1))

		cw := &statusCaptureWriter{ResponseWriter: w, statusCode: http.StatusOK}
		proxy.ServeHTTP(cw, r)
		return cw.statusCode, 1, relayHit, sampleRate
	}

	body, err := maybeReadRequestBodyForRetry(r)
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return http.StatusBadRequest, 0, false, sampleRate
	}

	relayHit := false
	for attempt, resolved := range candidates {
		if !relayHit && plan.IsRelayCandidate(match.Destination, resolved) {
			relayHit = true
		}

		proxy, err := s.proxyForDestination(resolved)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return http.StatusBadGateway, attempt + 1, relayHit, sampleRate
		}

		attemptReq := r
		if attempt == 0 {
			if body != nil {
				attemptReq.Body = io.NopCloser(bytes.NewReader(body))
				attemptReq.ContentLength = int64(len(body))
			}
		} else {
			attemptReq, err = cloneRequestWithBody(r, body)
			if err != nil {
				http.Error(w, "failed to clone request", http.StatusInternalServerError)
				return http.StatusInternalServerError, attempt + 1, relayHit, sampleRate
			}
		}

		attemptReq.Header.Set(headerFluxMeshService, match.ServiceName)
		attemptReq.Header.Set(headerFluxMeshDestination, match.Destination)
		attemptReq.Header.Set(headerFluxMeshUpstream, resolved)
		attemptReq.Header.Set(headerFluxMeshHops, strconv.Itoa(incomingHops+1))

		rr := httptest.NewRecorder()
		proxy.ServeHTTP(rr, attemptReq)

		if !shouldRetryStatus(rr.Code) || attempt == len(candidates)-1 {
			writeRecordedResponse(w, rr)
			return rr.Code, attempt + 1, relayHit, sampleRate
		}
	}

	http.Error(w, "upstream proxy error: no candidate executed", http.StatusBadGateway)
	return http.StatusBadGateway, 0, relayHit, sampleRate
}

func (s *Server) recordTrafficMetrics(elapsed time.Duration, statusCode int, attemptsUsed int, relayHit bool, sampleRate uint32) {
	weight := uint64(1)
	rate := sampleRate
	if rate == 0 {
		rate = s.metricsSampleRate.Load()
	}
	if rate > 1 {
		if s.metricsSampleSeq.Add(1)%uint64(rate) != 0 {
			return
		}
		weight = uint64(rate)
	}

	s.metrics.requestsTotal.Add(weight)
	s.metrics.totalLatencyNs.Add(uint64(elapsed.Nanoseconds()) * weight)

	if statusCode >= 500 {
		s.metrics.errorTotal.Add(weight)
	}

	if attemptsUsed > 1 {
		s.metrics.retryAttemptsTotal.Add(uint64(attemptsUsed-1) * weight)
	}

	if relayHit {
		s.metrics.relayHitTotal.Add(weight)
		s.metrics.relayLatencyNs.Add(uint64(elapsed.Nanoseconds()) * weight)
	}
}

func effectiveMetricsSampleRate(configured int, fallback uint32) uint32 {
	if configured > 0 {
		return uint32(configured)
	}
	if fallback == 0 {
		return 1
	}
	return fallback
}

// Stats 返回流量面轻量统计快照（原子读取，低开销）。
func (s *Server) Stats() TrafficStats {
	requests := s.metrics.requestsTotal.Load()
	errors := s.metrics.errorTotal.Load()
	success := uint64(0)
	if requests >= errors {
		success = requests - errors
	}

	return TrafficStats{
		RequestsTotal:      requests,
		SuccessTotal:       success,
		ErrorTotal:         errors,
		RetryAttemptsTotal: s.metrics.retryAttemptsTotal.Load(),
		RelayHitTotal:      s.metrics.relayHitTotal.Load(),
		TotalLatencyNs:     s.metrics.totalLatencyNs.Load(),
		RelayLatencyNs:     s.metrics.relayLatencyNs.Load(),
	}
}

// SetMetricsSampleRate 设置观测采样率，1 表示全量采样。
func (s *Server) SetMetricsSampleRate(rate uint32) {
	if rate == 0 {
		rate = 1
	}
	s.metricsSampleRate.Store(rate)
}

func effectiveMaxAttempts(configured int) int {
	if configured <= 0 {
		return 1
	}
	return configured
}

func effectiveMaxHops(configured int) int {
	if configured <= 0 {
		return 2
	}
	return configured
}

func applyRetryBudget(maxAttempts int, budgetRatio float64) int {
	if maxAttempts <= 1 {
		return 1
	}
	if budgetRatio <= 0 {
		return 1
	}
	if budgetRatio >= 1 {
		return maxAttempts
	}

	extra := maxAttempts - 1
	allowedExtra := int(math.Floor(float64(extra) * budgetRatio))
	if allowedExtra == 0 {
		allowedExtra = 1
	}
	if allowedExtra > extra {
		allowedExtra = extra
	}
	return 1 + allowedExtra
}

func parseIncomingHops(raw string) (int, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, nil
	}
	hops, err := strconv.Atoi(trimmed)
	if err != nil || hops < 0 {
		return 0, fmt.Errorf("invalid hops")
	}
	return hops, nil
}

func readRequestBodyBytes(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}

func maybeReadRequestBodyForRetry(r *http.Request) ([]byte, error) {
	if r == nil {
		return nil, nil
	}
	if r.ContentLength > 0 {
		return readRequestBodyBytes(r)
	}
	if len(r.TransferEncoding) > 0 {
		return readRequestBodyBytes(r)
	}
	return nil, nil
}

func cloneRequestWithBody(r *http.Request, body []byte) (*http.Request, error) {
	req := r.Clone(r.Context())
	if body == nil {
		req.Body = nil
		req.ContentLength = 0
		return req, nil
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.ContentLength = int64(len(body))
	return req, nil
}

func shouldRetryStatus(statusCode int) bool {
	return statusCode >= http.StatusBadGateway
}

func writeRecordedResponse(w http.ResponseWriter, rr *httptest.ResponseRecorder) {
	for k, values := range rr.Header() {
		for _, v := range values {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(rr.Code)
	_, _ = w.Write(rr.Body.Bytes())
}

type statusCaptureWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *statusCaptureWriter) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

// proxyForDestination 为解析后的上游地址返回可复用的反向代理实例。
func (s *Server) proxyForDestination(destination string) (*httputil.ReverseProxy, error) {
	destination = strings.TrimSpace(destination)
	if destination == "" {
		return nil, fmt.Errorf("route destination is empty")
	}

	s.mu.RLock()
	proxy, ok := s.proxies[destination]
	s.mu.RUnlock()
	if ok {
		return proxy, nil
	}

	target, err := parseDestination(destination)
	if err != nil {
		return nil, err
	}

	proxy = newReverseProxy(target)
	if s.transport != nil {
		proxy.Transport = s.transport
	}

	s.mu.Lock()
	if existing, exists := s.proxies[destination]; exists {
		s.mu.Unlock()
		return existing, nil
	}
	s.proxies[destination] = proxy
	s.mu.Unlock()

	return proxy, nil
}

// parseDestination 将 host:port 或 http(s) URL 规范化为上游 URL。
func parseDestination(destination string) (*url.URL, error) {
	if strings.Contains(destination, "://") {
		u, err := url.Parse(destination)
		if err != nil {
			return nil, fmt.Errorf("invalid destination url: %w", err)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return nil, fmt.Errorf("unsupported destination scheme: %s", u.Scheme)
		}
		if strings.TrimSpace(u.Host) == "" {
			return nil, fmt.Errorf("destination host is required")
		}
		return u, nil
	}

	if _, _, err := net.SplitHostPort(destination); err != nil {
		return nil, fmt.Errorf("destination must be host:port or http(s)://host:port")
	}

	u, err := url.Parse("http://" + destination)
	if err != nil {
		return nil, fmt.Errorf("invalid destination: %w", err)
	}
	return u, nil
}

// newReverseProxy 构造安全默认的反向代理并重建转发头。
func newReverseProxy(target *url.URL) *httputil.ReverseProxy {
	proxy := httputil.NewSingleHostReverseProxy(target)
	// NewSingleHostReverseProxy 会预置 Director，使用 Rewrite 时必须清空。
	proxy.Director = nil
	proxy.Rewrite = func(pr *httputil.ProxyRequest) {
		pr.Out.Header.Del("Forwarded")
		pr.Out.Header.Del("X-Forwarded-For")
		pr.Out.Header.Del("X-Forwarded-Host")
		pr.Out.Header.Del("X-Forwarded-Proto")

		pr.SetURL(target)
		pr.SetXForwarded()
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		http.Error(w, "upstream proxy error: "+err.Error(), http.StatusBadGateway)
	}
	return proxy
}

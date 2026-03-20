package traffic

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"testing"
	"time"

	"fluxmesh/internal/model"
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

func TestHandleTCPConnProxy(t *testing.T) {
	upstream, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start tcp upstream: %v", err)
	}
	defer upstream.Close()

	go func() {
		for {
			conn, err := upstream.Accept()
			if err != nil {
				return
			}

			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 128)
				n, readErr := c.Read(buf)
				if readErr != nil {
					return
				}
				_, _ = c.Write([]byte("echo:" + string(buf[:n])))
			}(conn)
		}
	}()

	plan, err := BuildPlan([]model.ServiceConfig{{
		Name: "tcp-gateway",
		TrafficPolicy: model.ServiceTrafficPolicy{
			Proxy:    model.ProxyPolicy{Layer: "l4-tcp"},
			Listener: model.ListenerPolicy{Addr: "127.0.0.1", Port: 19090},
		},
		Routes: []model.ServiceRoute{{PathPrefix: "/", Destination: upstream.Addr().String(), Weight: 100}},
	}})
	if err != nil {
		t.Fatalf("build plan failed: %v", err)
	}

	s := &Server{plan: plan, listeners: map[string]*http.Server{}, proxies: map[string]*httputil.ReverseProxy{}}
	client, server := net.Pipe()
	defer client.Close()

	go s.handleTCPConn(server, ListenerKey{Addr: "127.0.0.1", Port: 19090})

	if _, err := client.Write([]byte("ping")); err != nil {
		t.Fatalf("write to proxy failed: %v", err)
	}

	out := make([]byte, 64)
	n, err := io.ReadAtLeast(client, out, len("echo:ping"))
	if err != nil {
		t.Fatalf("read from proxy failed: %v", err)
	}

	if string(out[:n]) != "echo:ping" {
		t.Fatalf("unexpected tcp proxy response: %s", string(out[:n]))
	}
}

func TestListenerHandlerRetrySuccessOnSecondAttempt(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	unreachable := reserveUnusedLocalAddr(t)

	plan, err := BuildPlan([]model.ServiceConfig{{
		Name: "retry-svc",
		TrafficPolicy: model.ServiceTrafficPolicy{
			Listener: model.ListenerPolicy{Addr: "0.0.0.0", Port: 18080},
			LB:       model.LBPolicy{Strategy: "round-robin"},
			Retry:    model.RetryPolicy{MaxAttempts: 2, BudgetRatio: 1},
		},
		BackendGroups: []model.BackendGroup{{
			Name: "retry-backends",
			Targets: []model.BackendTarget{
				{Addr: unreachable, Weight: 100},
				{Addr: upstream.Listener.Addr().String(), Weight: 100},
			},
		}},
		Routes: []model.ServiceRoute{{Hosts: []string{"retry.example.com"}, PathPrefix: "/", Destination: "retry-backends", Weight: 100}},
	}})
	if err != nil {
		t.Fatalf("build plan failed: %v", err)
	}

	s := &Server{plan: plan, listeners: map[string]*http.Server{}, proxies: map[string]*httputil.ReverseProxy{}}
	h := s.newListenerHandler(ListenerKey{Addr: "0.0.0.0", Port: 18080})

	req := httptest.NewRequest(http.MethodGet, "http://mesh.local/demo", nil)
	req.Host = "retry.example.com"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 after retry, got %d, body=%s", rr.Code, rr.Body.String())
	}
}

func TestListenerHandlerRetryBudgetZeroDisablesRetry(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	unreachable := reserveUnusedLocalAddr(t)

	plan, err := BuildPlan([]model.ServiceConfig{{
		Name: "retry-budget-svc",
		TrafficPolicy: model.ServiceTrafficPolicy{
			Listener: model.ListenerPolicy{Addr: "0.0.0.0", Port: 18080},
			LB:       model.LBPolicy{Strategy: "round-robin"},
			Retry:    model.RetryPolicy{MaxAttempts: 3, BudgetRatio: 0},
		},
		BackendGroups: []model.BackendGroup{{
			Name: "retry-budget-backends",
			Targets: []model.BackendTarget{
				{Addr: unreachable, Weight: 100},
				{Addr: upstream.Listener.Addr().String(), Weight: 100},
			},
		}},
		Routes: []model.ServiceRoute{{Hosts: []string{"budget.example.com"}, PathPrefix: "/", Destination: "retry-budget-backends", Weight: 100}},
	}})
	if err != nil {
		t.Fatalf("build plan failed: %v", err)
	}

	s := &Server{plan: plan, listeners: map[string]*http.Server{}, proxies: map[string]*httputil.ReverseProxy{}}
	h := s.newListenerHandler(ListenerKey{Addr: "0.0.0.0", Port: 18080})

	req := httptest.NewRequest(http.MethodGet, "http://mesh.local/demo", nil)
	req.Host = "budget.example.com"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 when retry budget disables retries, got %d", rr.Code)
	}
}

func TestListenerHandlerRejectsWhenHopsExceeded(t *testing.T) {
	plan, err := BuildPlan([]model.ServiceConfig{{
		Name: "relay-svc",
		TrafficPolicy: model.ServiceTrafficPolicy{
			Listener: model.ListenerPolicy{Addr: "0.0.0.0", Port: 18080},
			Relay:    model.RelayPolicy{MaxHops: 1},
		},
		Routes: []model.ServiceRoute{{Hosts: []string{"relay.example.com"}, PathPrefix: "/", Destination: "127.0.0.1:29999", Weight: 100}},
	}})
	if err != nil {
		t.Fatalf("build plan failed: %v", err)
	}

	s := &Server{plan: plan, listeners: map[string]*http.Server{}, proxies: map[string]*httputil.ReverseProxy{}}
	h := s.newListenerHandler(ListenerKey{Addr: "0.0.0.0", Port: 18080})

	req := httptest.NewRequest(http.MethodGet, "http://mesh.local/demo", nil)
	req.Host = "relay.example.com"
	req.Header.Set(headerFluxMeshHops, "2")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusLoopDetected {
		t.Fatalf("expected 508 when hops exceeded, got %d", rr.Code)
	}
}

func TestListenerHandlerLoadFirstFallbackToNextCandidate(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	unreachable := reserveUnusedLocalAddr(t)

	plan, err := BuildPlan([]model.ServiceConfig{{
		Name: "fallback-svc",
		TrafficPolicy: model.ServiceTrafficPolicy{
			Listener: model.ListenerPolicy{Addr: "0.0.0.0", Port: 18080},
			LB:       model.LBPolicy{Strategy: "load-first"},
			Retry:    model.RetryPolicy{MaxAttempts: 2, BudgetRatio: 1},
		},
		BackendGroups: []model.BackendGroup{{
			Name: "fallback-backends",
			Targets: []model.BackendTarget{
				{Addr: unreachable, Weight: 100},
				{Addr: upstream.Listener.Addr().String(), Weight: 80},
			},
		}},
		Routes: []model.ServiceRoute{{Hosts: []string{"fallback.example.com"}, PathPrefix: "/", Destination: "fallback-backends", Weight: 100}},
	}})
	if err != nil {
		t.Fatalf("build plan failed: %v", err)
	}

	s := &Server{plan: plan, listeners: map[string]*http.Server{}, proxies: map[string]*httputil.ReverseProxy{}}
	h := s.newListenerHandler(ListenerKey{Addr: "0.0.0.0", Port: 18080})

	req := httptest.NewRequest(http.MethodGet, "http://mesh.local/demo", nil)
	req.Host = "fallback.example.com"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 after fallback to next candidate, got %d, body=%s", rr.Code, rr.Body.String())
	}
}

func TestListenerHandlerDirectFailoverToRelayTaggedTarget(t *testing.T) {
	relayUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("relay-ok"))
	}))
	defer relayUpstream.Close()

	directUnreachable := reserveUnusedLocalAddr(t)

	plan, err := BuildPlan([]model.ServiceConfig{{
		Name: "relay-fallback-svc",
		TrafficPolicy: model.ServiceTrafficPolicy{
			Listener: model.ListenerPolicy{Addr: "0.0.0.0", Port: 18080},
			LB:       model.LBPolicy{Strategy: "load-first"},
			Retry:    model.RetryPolicy{MaxAttempts: 2, BudgetRatio: 1},
		},
		BackendGroups: []model.BackendGroup{{
			Name: "relay-fallback-group",
			Targets: []model.BackendTarget{
				{Addr: directUnreachable, Weight: 100},
				{Addr: relayUpstream.Listener.Addr().String(), Weight: 10, Tags: map[string]string{"relay": "true"}},
			},
		}},
		Routes: []model.ServiceRoute{{Hosts: []string{"relay-fallback.example.com"}, PathPrefix: "/", Destination: "relay-fallback-group", Weight: 100}},
	}})
	if err != nil {
		t.Fatalf("build plan failed: %v", err)
	}

	s := &Server{plan: plan, listeners: map[string]*http.Server{}, proxies: map[string]*httputil.ReverseProxy{}}
	h := s.newListenerHandler(ListenerKey{Addr: "0.0.0.0", Port: 18080})

	req := httptest.NewRequest(http.MethodGet, "http://mesh.local/demo", nil)
	req.Host = "relay-fallback.example.com"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected relay fallback success, got %d body=%s", rr.Code, rr.Body.String())
	}
	if rr.Body.String() != "relay-ok" {
		t.Fatalf("expected relay upstream body, got %s", rr.Body.String())
	}
}

func TestServerStatsCountsRetryAndRelayHit(t *testing.T) {
	relayUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("relay-ok"))
	}))
	defer relayUpstream.Close()

	directUnreachable := reserveUnusedLocalAddr(t)

	plan, err := BuildPlan([]model.ServiceConfig{{
		Name: "stats-svc",
		TrafficPolicy: model.ServiceTrafficPolicy{
			Listener: model.ListenerPolicy{Addr: "0.0.0.0", Port: 18080},
			LB:       model.LBPolicy{Strategy: "load-first"},
			Retry:    model.RetryPolicy{MaxAttempts: 2, BudgetRatio: 1},
		},
		BackendGroups: []model.BackendGroup{{
			Name: "stats-group",
			Targets: []model.BackendTarget{
				{Addr: directUnreachable, Weight: 100},
				{Addr: relayUpstream.Listener.Addr().String(), Weight: 10, Tags: map[string]string{"relay": "true"}},
			},
		}},
		Routes: []model.ServiceRoute{{Hosts: []string{"stats.example.com"}, PathPrefix: "/", Destination: "stats-group", Weight: 100}},
	}})
	if err != nil {
		t.Fatalf("build plan failed: %v", err)
	}

	s := &Server{plan: plan, listeners: map[string]*http.Server{}, proxies: map[string]*httputil.ReverseProxy{}}
	h := s.newListenerHandler(ListenerKey{Addr: "0.0.0.0", Port: 18080})

	req := httptest.NewRequest(http.MethodGet, "http://mesh.local/demo", nil)
	req.Host = "stats.example.com"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	stats := s.Stats()
	if stats.RequestsTotal != 1 {
		t.Fatalf("expected requests_total=1, got %d", stats.RequestsTotal)
	}
	if stats.RetryAttemptsTotal != 1 {
		t.Fatalf("expected retry_attempts_total=1, got %d", stats.RetryAttemptsTotal)
	}
	if stats.RelayHitTotal != 1 {
		t.Fatalf("expected relay_hit_total=1, got %d", stats.RelayHitTotal)
	}
	if stats.SuccessTotal != 1 || stats.ErrorTotal != 0 {
		t.Fatalf("unexpected success/error counters: success=%d error=%d", stats.SuccessTotal, stats.ErrorTotal)
	}
}

func TestServerStatsSamplingRate(t *testing.T) {
	s := NewServer(nil)
	s.SetMetricsSampleRate(2)

	s.recordTrafficMetrics(1*time.Millisecond, http.StatusOK, 1, false, 0)
	stats := s.Stats()
	if stats.RequestsTotal != 0 {
		t.Fatalf("expected first sampled-out request to be ignored, got %d", stats.RequestsTotal)
	}

	s.recordTrafficMetrics(1*time.Millisecond, http.StatusOK, 1, false, 0)
	stats = s.Stats()
	if stats.RequestsTotal != 2 {
		t.Fatalf("expected sampled weight to scale counts to 2, got %d", stats.RequestsTotal)
	}
	if stats.SuccessTotal != 2 || stats.ErrorTotal != 0 {
		t.Fatalf("unexpected success/error totals after sampling: success=%d error=%d", stats.SuccessTotal, stats.ErrorTotal)
	}
}

func TestEffectiveMetricsSampleRate(t *testing.T) {
	if got := effectiveMetricsSampleRate(8, 1); got != 8 {
		t.Fatalf("expected service sample rate 8, got %d", got)
	}
	if got := effectiveMetricsSampleRate(0, 4); got != 4 {
		t.Fatalf("expected fallback sample rate 4, got %d", got)
	}
	if got := effectiveMetricsSampleRate(0, 0); got != 1 {
		t.Fatalf("expected hard default sample rate 1, got %d", got)
	}
}

func reserveUnusedLocalAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port failed: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

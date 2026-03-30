package traffic

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"fluxmesh/internal/model"

)

type closeErrListener struct{}

func (l *closeErrListener) Accept() (net.Conn, error) { return nil, errors.New("not implemented") }
func (l *closeErrListener) Close() error              { return errors.New("close failed") }
func (l *closeErrListener) Addr() net.Addr            { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1} }

func mustUDPConn(t *testing.T) *net.UDPConn {
	t.Helper()
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("resolve udp addr failed: %v", err)
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		t.Fatalf("listen udp failed: %v", err)
	}
	return conn
}

func TestServerStartNilServices(t *testing.T) {
	s := NewServer(nil)
	if err := s.Start(context.Background()); err == nil {
		t.Fatalf("expected start error when services registry is nil")
	}
}

func TestServerShutdownClosesResources(t *testing.T) {
	httpSrv := &http.Server{Addr: "127.0.0.1:0", Handler: http.NewServeMux()}
	udpListener := mustUDPConn(t)
	udpSessionConn := mustUDPConn(t)

	s := &Server{
		listeners:          map[string]*http.Server{"h": httpSrv},
		tcpListeners:       map[string]net.Listener{"t": &closeErrListener{}},
		udpListeners:       map[string]*net.UDPConn{"u": udpListener},
		udpListenerOptions: map[string]udpRuntimeOptions{"u": {}},
		udpSessions:        map[string]*udpSession{"s": {conn: udpSessionConn, lastUsed: time.Now()}},
		udpSessionStop:     make(chan struct{}),
		metricsEvents:      make(chan metricsEvent, 1),
		metricsStop:        make(chan struct{}),
	}
	s.udpSessionStarted.Store(true)
	s.metricsStarted.Store(true)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := s.Shutdown(ctx)
	if err == nil {
		t.Fatalf("expected shutdown to return first close error")
	}

	if len(s.listeners) != 0 || len(s.tcpListeners) != 0 || len(s.udpListeners) != 0 || len(s.udpSessions) != 0 {
		t.Fatalf("expected all runtime maps to be reset")
	}
}
func TestCopyResponseHeadersAndStatusCaptureWriter(t *testing.T) {
	src := http.Header{}
	src.Add("X-Test", "a")
	src.Add("Connection", "keep-alive")
	dst := http.Header{}

	copyResponseHeaders(dst, src)
	if got := dst.Get("X-Test"); got != "a" {
		t.Fatalf("expected copied header, got %q", got)
	}
	if got := dst.Get("Connection"); got != "" {
		t.Fatalf("expected hop-by-hop header removed, got %q", got)
	}

	rr := httptest.NewRecorder()
	cw := &statusCaptureWriter{ResponseWriter: rr}
	cw.WriteHeader(http.StatusCreated)
	if cw.statusCode != http.StatusCreated {
		t.Fatalf("expected captured status 201, got %d", cw.statusCode)
	}
}

func TestServerInternalHelpers(t *testing.T) {
	t.Run("get service watch revision", func(t *testing.T) {
		s := &Server{}
		if got := s.getServiceWatchRevision(); got != 1 {
			t.Fatalf("expected default revision 1, got %d", got)
		}
		s.serviceWatchRevision = 9
		if got := s.getServiceWatchRevision(); got != 9 {
			t.Fatalf("expected revision 9, got %d", got)
		}
	})

	t.Run("reconcile loop exits on canceled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		done := make(chan struct{})
		go func() {
			(&Server{}).reconcileLoop(ctx)
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(200 * time.Millisecond):
			t.Fatalf("expected reconcile loop to exit quickly")
		}
	})

	t.Run("watch loop exits on canceled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		done := make(chan struct{})
		go func() {
			(&Server{}).watchServicesLoop(ctx)
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(200 * time.Millisecond):
			t.Fatalf("expected watch loop to exit quickly")
		}
	})

	t.Run("udp session reaper removes expired sessions", func(t *testing.T) {
		expired := mustUDPConn(t)
		fresh := mustUDPConn(t)

		s := &Server{
			udpSessions: map[string]*udpSession{
				listenerMapKey("127.0.0.1", 19091) + "|c1|u1": {conn: expired, lastUsed: time.Now().Add(-2 * time.Second)},
				listenerMapKey("127.0.0.1", 19092) + "|c2|u2": {conn: fresh, lastUsed: time.Now()},
			},
			plan: Plan{
				udpBindings: map[string]UDPBinding{
					listenerMapKey("127.0.0.1", 19091): {Listener: ListenerKey{Addr: "127.0.0.1", Port: 19091}, ServiceName: "svc-a"},
					listenerMapKey("127.0.0.1", 19092): {Listener: ListenerKey{Addr: "127.0.0.1", Port: 19092}, ServiceName: "svc-b"},
				},
				servicePolicies: map[string]model.ServiceTrafficPolicy{
					"svc-a": {UDP: model.UDPPolicy{SessionTTLMs: 500}},
					"svc-b": {UDP: model.UDPPolicy{SessionTTLMs: 5000}},
				},
			},
		}

		s.reapUDPSessions()
		s.udpSessionMu.Lock()
		count := len(s.udpSessions)
		s.udpSessionMu.Unlock()
		if count != 1 {
			t.Fatalf("expected only one fresh udp session to remain, got %d", count)
		}
	})

	t.Run("accept tcp loop exits on closed listener", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen tcp failed: %v", err)
		}
		_ = ln.Close()

		done := make(chan struct{})
		go func() {
			(&Server{}).acceptTCPLoop(ListenerKey{Addr: "127.0.0.1", Port: 1}, ln)
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(200 * time.Millisecond):
			t.Fatalf("expected accept tcp loop to exit on closed listener")
		}
	})

	t.Run("metrics aggregator applies events", func(t *testing.T) {
		s := NewServer(nil)
		s.startMetricsAggregator()
		s.metricsEvents <- metricsEvent{weight: 2, retryAttempts: 3, relayHit: true, relayLatencyNs: 100}
		time.Sleep(30 * time.Millisecond)
		s.stopMetricsAggregator()

		stats := s.Stats()
		if stats.RetryAttemptsTotal == 0 || stats.RelayHitTotal == 0 || stats.RelayLatencyNs == 0 {
			t.Fatalf("expected metrics event to be applied, got %+v", stats)
		}
	})
}

func TestServerRequestBodyHelpersAndBudget(t *testing.T) {
	if applyRetryBudget(1, 1) != 1 {
		t.Fatalf("expected maxAttempts<=1 to keep 1")
	}
	if applyRetryBudget(4, 0) != 1 {
		t.Fatalf("expected budget<=0 to disable retries")
	}
	if applyRetryBudget(4, 1) != 4 {
		t.Fatalf("expected budget>=1 to keep max attempts")
	}

	req := httptest.NewRequest(http.MethodPost, "http://mesh.local/upload", io.NopCloser(bytes.NewBufferString("abc")))
	req.ContentLength = 3
	body, err := readRequestBodyBytes(req)
	if err != nil {
		t.Fatalf("read request body failed: %v", err)
	}
	if string(body) != "abc" {
		t.Fatalf("unexpected body: %q", string(body))
	}
	reRead, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("re-read reset body failed: %v", err)
	}
	if string(reRead) != "abc" {
		t.Fatalf("expected reset body to be readable, got %q", string(reRead))
	}

	nilReq := httptest.NewRequest(http.MethodGet, "http://mesh.local/", nil)
	nilReq.Body = nil
	nilBody, err := readRequestBodyBytes(nilReq)
	if err != nil || nilBody != nil {
		t.Fatalf("expected nil body for nil request body, got body=%v err=%v", nilBody, err)
	}

	tests := []struct {
		base string
		req  string
		want string
	}{
		{base: "", req: "", want: "/"},
		{base: "/api", req: "", want: "/api"},
		{base: "/api/", req: "/v1", want: "/api/v1"},
		{base: "/api", req: "v1", want: "/api/v1"},
		{base: "/api", req: "/v1", want: "/api/v1"},
	}
	for _, tc := range tests {
		if got := joinURLPath(tc.base, tc.req); got != tc.want {
			t.Fatalf("joinURLPath(%q,%q): want %q got %q", tc.base, tc.req, tc.want, got)
		}
	}

	s := NewServer(nil)
	s.SetMetricsSampleRate(0)
	if got := s.metricsSampleRate.Load(); got != 1 {
		t.Fatalf("expected zero sample rate to normalize to 1, got %d", got)
	}

	reqChunked := httptest.NewRequest(http.MethodPost, "http://mesh.local/chunk", io.NopCloser(bytes.NewBufferString("xyz")))
	reqChunked.ContentLength = -1
	reqChunked.TransferEncoding = []string{"chunked"}
	chunkedBody, err := maybeReadRequestBodyForRetry(reqChunked)
	if err != nil || string(chunkedBody) != "xyz" {
		t.Fatalf("expected chunked body read, got body=%q err=%v", string(chunkedBody), err)
	}

	cloned, err := cloneRequestWithBody(reqChunked, nil)
	if err != nil {
		t.Fatalf("clone request with nil body failed: %v", err)
	}
	if cloned.Body != nil || cloned.ContentLength != 0 {
		t.Fatalf("expected nil cloned body and zero length")
	}

	if applyRetryBudget(5, 0.01) != 2 {
		t.Fatalf("expected tiny budget to still allow one retry")
	}
}

func TestServerDestinationCachesAndForwardDirectError(t *testing.T) {
	s := NewServer(nil)

	if _, err := s.proxyForDestination(""); err == nil {
		t.Fatalf("expected empty destination error for proxy")
	}
	if _, err := s.proxyForDestination("bad"); err == nil {
		t.Fatalf("expected parse error for invalid proxy destination")
	}

	p1, err := s.proxyForDestination("127.0.0.1:8080")
	if err != nil {
		t.Fatalf("expected proxy creation success, got %v", err)
	}
	p2, err := s.proxyForDestination("127.0.0.1:8080")
	if err != nil {
		t.Fatalf("expected proxy cache hit success, got %v", err)
	}
	if p1 != p2 {
		t.Fatalf("expected cached proxy instance reuse")
	}

	if _, err := s.targetForDestination(""); err == nil {
		t.Fatalf("expected empty destination error for target")
	}
	if _, err := s.targetForDestination("bad"); err == nil {
		t.Fatalf("expected parse error for invalid target destination")
	}
	t1, err := s.targetForDestination("127.0.0.1:8081")
	if err != nil {
		t.Fatalf("expected target creation success, got %v", err)
	}
	t2, err := s.targetForDestination("127.0.0.1:8081")
	if err != nil {
		t.Fatalf("expected target cache hit success, got %v", err)
	}
	if t1 != t2 {
		t.Fatalf("expected cached target instance reuse")
	}

	r := httptest.NewRequest(http.MethodGet, "http://mesh.local/demo", nil)
	w := httptest.NewRecorder()
	_, err = s.forwardDirect(w, r, &url.URL{Scheme: "http", Host: "127.0.0.1:1"})
	if err == nil {
		t.Fatalf("expected forwardDirect to return upstream error")
	}
}

package traffic

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
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
}

// NewServer 创建流量面运行时实例并初始化监听器与代理缓存。
func NewServer(services *registry.Services) *Server {
	return &Server{
		services:  services,
		listeners: make(map[string]*http.Server),
		proxies:   make(map[string]*httputil.ReverseProxy),
	}
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
			return
		}

		resolved, err := plan.ResolveDestination(match.Destination)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}

		proxy, err := s.proxyForDestination(resolved)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}

		r.Header.Set("X-FluxMesh-Service", match.ServiceName)
		r.Header.Set("X-FluxMesh-Destination", match.Destination)
		r.Header.Set("X-FluxMesh-Upstream", resolved)
		proxy.ServeHTTP(w, r)
	})
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

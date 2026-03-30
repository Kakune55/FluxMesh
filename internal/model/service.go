package model

import (
	"fmt"
	"net"
	"strings"
)

type ServiceConfig struct {
	Name            string               `json:"name"`
	Namespace       string               `json:"namespace,omitempty"`
	Version         string               `json:"version,omitempty"`
	ResourceVersion int64                `json:"resource_version,omitempty"`
	UpdatedAt       string               `json:"updated_at,omitempty"`
	UpdatedBy       string               `json:"updated_by,omitempty"`
	Routes          []ServiceRoute       `json:"routes"`
	BackendGroups   []BackendGroup       `json:"backend_groups,omitempty"`
	TrafficPolicy   ServiceTrafficPolicy `json:"traffic_policy"`
}

type BackendGroup struct {
	Name    string          `json:"name"`
	Targets []BackendTarget `json:"targets"`
}

type BackendTarget struct {
	Addr   string            `json:"addr"`
	Weight int               `json:"weight,omitempty"`
	Tags   map[string]string `json:"tags,omitempty"`
}

type ServiceRoute struct {
	Hosts       []string `json:"hosts,omitempty"`
	PathPrefix  string   `json:"path_prefix"`
	Destination string   `json:"destination"`
	Weight      int      `json:"weight,omitempty"`
}

type ServiceTrafficPolicy struct {
	Proxy     ProxyPolicy    `json:"proxy,omitempty"`
	Protocols []string       `json:"protocols,omitempty"`
	Listener  ListenerPolicy `json:"listener"`
	UDP       UDPPolicy      `json:"udp,omitempty"`
	LB        LBPolicy       `json:"lb,omitempty"`
	Retry     RetryPolicy    `json:"retry,omitempty"`
	Relay     RelayPolicy    `json:"relay,omitempty"`
	Observability ObservabilityPolicy `json:"observability,omitempty"`
}

type ProxyPolicy struct {
	Layer string `json:"layer,omitempty"`
}

type ListenerPolicy struct {
	Addr string `json:"addr,omitempty"`
	Port int    `json:"port"`
}

type UDPPolicy struct {
	DialTimeoutMs  int `json:"dial_timeout_ms,omitempty"`
	ReadTimeoutMs  int `json:"read_timeout_ms,omitempty"`
	WriteTimeoutMs int `json:"write_timeout_ms,omitempty"`
	SessionTTLMs   int `json:"session_ttl_ms,omitempty"`
	MaxPacketSize  int `json:"max_packet_size,omitempty"`
}

type LBPolicy struct {
	Strategy string `json:"strategy,omitempty"`
}

type RetryPolicy struct {
	MaxAttempts int     `json:"max_attempts,omitempty"`
	BudgetRatio float64 `json:"budget_ratio,omitempty"`
}

type RelayPolicy struct {
	MaxHops int `json:"max_hops,omitempty"`
}

type ObservabilityPolicy struct {
	MetricsSampleRate int `json:"metrics_sample_rate,omitempty"`
}

// ApplyDefaults 填充服务配置的最小可运行默认值。
func (s *ServiceConfig) ApplyDefaults() {
	if strings.TrimSpace(s.TrafficPolicy.Listener.Addr) == "" {
		s.TrafficPolicy.Listener.Addr = "0.0.0.0"
	}

	if strings.TrimSpace(s.TrafficPolicy.Proxy.Layer) == "" {
		s.TrafficPolicy.Proxy.Layer = "l7-http"
	}

	if len(s.TrafficPolicy.Protocols) == 0 {
		layer := strings.ToLower(strings.TrimSpace(s.TrafficPolicy.Proxy.Layer))
		switch layer {
		case "l4-tcp":
			s.TrafficPolicy.Protocols = []string{"tcp"}
		case "l4-udp":
			s.TrafficPolicy.Protocols = []string{"udp"}
		default:
			s.TrafficPolicy.Protocols = []string{"http"}
		}
	}

	if s.TrafficPolicy.Observability.MetricsSampleRate <= 0 {
		s.TrafficPolicy.Observability.MetricsSampleRate = 1
	}

	if s.TrafficPolicy.UDP.DialTimeoutMs <= 0 {
		s.TrafficPolicy.UDP.DialTimeoutMs = 2000
	}
	if s.TrafficPolicy.UDP.ReadTimeoutMs <= 0 {
		s.TrafficPolicy.UDP.ReadTimeoutMs = 2000
	}
	if s.TrafficPolicy.UDP.WriteTimeoutMs <= 0 {
		s.TrafficPolicy.UDP.WriteTimeoutMs = 2000
	}
	if s.TrafficPolicy.UDP.SessionTTLMs <= 0 {
		s.TrafficPolicy.UDP.SessionTTLMs = 30000
	}
	if s.TrafficPolicy.UDP.MaxPacketSize <= 0 {
		s.TrafficPolicy.UDP.MaxPacketSize = 65535
	}

	for i := range s.Routes {
		if s.Routes[i].Weight == 0 {
			s.Routes[i].Weight = 100
		}
		if len(s.Routes[i].Hosts) == 0 {
			s.Routes[i].Hosts = []string{"*"}
		}
	}

	for i := range s.BackendGroups {
		for j := range s.BackendGroups[i].Targets {
			if s.BackendGroups[i].Targets[j].Weight == 0 {
				s.BackendGroups[i].Targets[j].Weight = 100
			}
		}
	}
}

// Validate 校验服务配置字段完整性和约束范围。
func (s ServiceConfig) Validate() error {
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("name is required")
	}

	if s.TrafficPolicy.Listener.Port <= 0 {
		return fmt.Errorf("traffic_policy.listener.port is required")
	}
	if s.TrafficPolicy.Listener.Port > 65535 {
		return fmt.Errorf("traffic_policy.listener.port must be between 1 and 65535")
	}

	if !isValidListenerAddr(s.TrafficPolicy.Listener.Addr) {
		return fmt.Errorf("traffic_policy.listener.addr must be 0.0.0.0, 127.0.0.1, or a bindable IPv4 address")
	}

	layer := strings.TrimSpace(s.TrafficPolicy.Proxy.Layer)
	if layer != "l7-http" && layer != "l4-tcp" && layer != "l4-udp" {
		return fmt.Errorf("traffic_policy.proxy.layer must be l7-http, l4-tcp, or l4-udp")
	}

	if !isValidLBStrategy(s.TrafficPolicy.LB.Strategy) {
		return fmt.Errorf("traffic_policy.lb.strategy must use [a-z0-9-], e.g. load-first, round-robin, random")
	}

	if s.TrafficPolicy.Observability.MetricsSampleRate < 1 || s.TrafficPolicy.Observability.MetricsSampleRate > 10000 {
		return fmt.Errorf("traffic_policy.observability.metrics_sample_rate must be between 1 and 10000")
	}

	if s.TrafficPolicy.UDP.DialTimeoutMs < 1 || s.TrafficPolicy.UDP.DialTimeoutMs > 60000 {
		return fmt.Errorf("traffic_policy.udp.dial_timeout_ms must be between 1 and 60000")
	}
	if s.TrafficPolicy.UDP.ReadTimeoutMs < 1 || s.TrafficPolicy.UDP.ReadTimeoutMs > 60000 {
		return fmt.Errorf("traffic_policy.udp.read_timeout_ms must be between 1 and 60000")
	}
	if s.TrafficPolicy.UDP.WriteTimeoutMs < 1 || s.TrafficPolicy.UDP.WriteTimeoutMs > 60000 {
		return fmt.Errorf("traffic_policy.udp.write_timeout_ms must be between 1 and 60000")
	}
	if s.TrafficPolicy.UDP.SessionTTLMs < 100 || s.TrafficPolicy.UDP.SessionTTLMs > 3600000 {
		return fmt.Errorf("traffic_policy.udp.session_ttl_ms must be between 100 and 3600000")
	}
	if s.TrafficPolicy.UDP.MaxPacketSize < 512 || s.TrafficPolicy.UDP.MaxPacketSize > 65535 {
		return fmt.Errorf("traffic_policy.udp.max_packet_size must be between 512 and 65535")
	}

	if len(s.TrafficPolicy.Protocols) == 0 {
		return fmt.Errorf("traffic_policy.protocols must include at least one protocol")
	}
	for i, protocol := range s.TrafficPolicy.Protocols {
		p := strings.ToLower(strings.TrimSpace(protocol))
		switch layer {
		case "l7-http":
			if p != "http" {
				return fmt.Errorf("traffic_policy.protocols[%d] only supports http when proxy.layer=l7-http", i)
			}
		case "l4-tcp":
			if p != "tcp" {
				return fmt.Errorf("traffic_policy.protocols[%d] only supports tcp when proxy.layer=l4-tcp", i)
			}
		case "l4-udp":
			if p != "udp" {
				return fmt.Errorf("traffic_policy.protocols[%d] only supports udp when proxy.layer=l4-udp", i)
			}
		default:
			return fmt.Errorf("traffic_policy.proxy.layer must be l7-http, l4-tcp, or l4-udp")
		}
	}

	if len(s.Routes) == 0 {
		return fmt.Errorf("at least one route is required")
	}

	backendNames := make(map[string]struct{}, len(s.BackendGroups))
	for i, group := range s.BackendGroups {
		name := strings.TrimSpace(group.Name)
		if name == "" {
			return fmt.Errorf("backend_groups[%d].name is required", i)
		}
		if _, exists := backendNames[name]; exists {
			return fmt.Errorf("backend_groups[%d].name duplicated: %s", i, name)
		}
		backendNames[name] = struct{}{}

		if len(group.Targets) == 0 {
			return fmt.Errorf("backend_groups[%d].targets requires at least one target", i)
		}
		for j, target := range group.Targets {
			if strings.TrimSpace(target.Addr) == "" {
				return fmt.Errorf("backend_groups[%d].targets[%d].addr is required", i, j)
			}
			if !isValidTargetAddr(target.Addr) {
				return fmt.Errorf("backend_groups[%d].targets[%d].addr must be host:port", i, j)
			}
			if target.Weight < 1 || target.Weight > 100 {
				return fmt.Errorf("backend_groups[%d].targets[%d].weight must be between 1 and 100", i, j)
			}
		}
	}

	for i, route := range s.Routes {
		if len(route.Hosts) == 0 {
			return fmt.Errorf("routes[%d].hosts is required after defaults", i)
		}
		for j, host := range route.Hosts {
			if strings.TrimSpace(host) == "" {
				return fmt.Errorf("routes[%d].hosts[%d] cannot be empty", i, j)
			}
		}
		if strings.TrimSpace(route.PathPrefix) == "" {
			return fmt.Errorf("routes[%d].path_prefix is required", i)
		}
		if !strings.HasPrefix(strings.TrimSpace(route.PathPrefix), "/") {
			return fmt.Errorf("routes[%d].path_prefix must start with '/'", i)
		}
		if strings.TrimSpace(route.Destination) == "" {
			return fmt.Errorf("routes[%d].destination is required", i)
		}
		if route.Weight < 1 || route.Weight > 100 {
			return fmt.Errorf("routes[%d].weight must be between 1 and 100", i)
		}
	}

	return nil
}

// isValidListenerAddr 校验监听地址是否为可绑定 IPv4。
func isValidListenerAddr(addr string) bool {
	trimmed := strings.TrimSpace(addr)
	if trimmed == "" {
		return false
	}

	ip := net.ParseIP(trimmed)
	if ip == nil {
		return false
	}

	v4 := ip.To4()
	if v4 == nil {
		return false
	}

	return true
}

// isValidTargetAddr 校验后端目标地址是否为合法 host:port。
func isValidTargetAddr(addr string) bool {
	host, portRaw, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return false
	}
	if strings.TrimSpace(host) == "" {
		return false
	}
	var port int
	if _, err := fmt.Sscan(portRaw, &port); err != nil {
		return false
	}
	return port >= 1 && port <= 65535
}

// isValidLBStrategy 校验服务级负载均衡策略是否在支持列表中。
func isValidLBStrategy(strategy string) bool {
	s := strings.ToLower(strings.TrimSpace(strategy))
	switch s {
	case "", "load-first", "latency-first", "round-robin", "rr", "random", "rand":
		return true
	}

	for _, ch := range s {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-' {
			continue
		}
		return false
	}
	return s != ""
}

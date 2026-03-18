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
	TrafficPolicy   ServiceTrafficPolicy `json:"traffic_policy"`
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
	LB        LBPolicy       `json:"lb,omitempty"`
	Retry     RetryPolicy    `json:"retry,omitempty"`
	Relay     RelayPolicy    `json:"relay,omitempty"`
}

type ProxyPolicy struct {
	Layer string `json:"layer,omitempty"`
}

type ListenerPolicy struct {
	Addr string `json:"addr,omitempty"`
	Port int    `json:"port"`
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

func (s *ServiceConfig) ApplyDefaults() {
	if strings.TrimSpace(s.TrafficPolicy.Listener.Addr) == "" {
		s.TrafficPolicy.Listener.Addr = "0.0.0.0"
	}

	if strings.TrimSpace(s.TrafficPolicy.Proxy.Layer) == "" {
		s.TrafficPolicy.Proxy.Layer = "l7-http"
	}

	if len(s.TrafficPolicy.Protocols) == 0 {
		s.TrafficPolicy.Protocols = []string{"http"}
	}

	for i := range s.Routes {
		if s.Routes[i].Weight == 0 {
			s.Routes[i].Weight = 100
		}
		if len(s.Routes[i].Hosts) == 0 {
			s.Routes[i].Hosts = []string{"*"}
		}
	}
}

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
	if layer != "l7-http" && layer != "l4-tcp" {
		return fmt.Errorf("traffic_policy.proxy.layer must be l7-http or l4-tcp")
	}

	if len(s.TrafficPolicy.Protocols) == 0 {
		return fmt.Errorf("traffic_policy.protocols must include at least one protocol")
	}
	for i, protocol := range s.TrafficPolicy.Protocols {
		if strings.TrimSpace(protocol) != "http" {
			return fmt.Errorf("traffic_policy.protocols[%d] only supports http in MVP", i)
		}
	}

	if len(s.Routes) == 0 {
		return fmt.Errorf("at least one route is required")
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

package traffic

import (
	"fmt"
	"math/rand"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"fluxmesh/internal/model"
)

type ListenerKey struct {
	Addr string `json:"addr"`
	Port int    `json:"port"`
}

type RouteBinding struct {
	ServiceName string   `json:"service_name"`
	Hosts       []string `json:"hosts"`
	PathPrefix  string   `json:"path_prefix"`
	Destination string   `json:"destination"`
	Weight      int      `json:"weight"`
}

type ListenerPlan struct {
	Listener ListenerKey    `json:"listener"`
	Routes   []RouteBinding `json:"routes"`
}

type Plan struct {
	listeners        map[string]ListenerPlan
	backendGroups    map[string]model.BackendGroup
	backendRelayAddrs map[string]map[string]struct{}
	backendStrategy  map[string]string
	serviceListeners map[string]ListenerKey
	servicePolicies  map[string]model.ServiceTrafficPolicy
	state            *planState
}

type MatchResult struct {
	Listener    ListenerKey `json:"listener"`
	ServiceName string      `json:"service_name"`
	Destination string      `json:"destination"`
	PathPrefix  string      `json:"path_prefix"`
}

// BuildPlan 将服务配置编译为监听视图、路由表和目标解析索引。
func BuildPlan(services []model.ServiceConfig) (Plan, error) {
	listeners := make(map[string]ListenerPlan)
	backendGroups := make(map[string]model.BackendGroup)
	backendRelayAddrs := make(map[string]map[string]struct{})
	backendStrategy := make(map[string]string)
	serviceListeners := make(map[string]ListenerKey)
	servicePolicies := make(map[string]model.ServiceTrafficPolicy)

	for i := range services {
		cfg := services[i]
		cfg.ApplyDefaults()
		if err := cfg.Validate(); err != nil {
			return Plan{}, fmt.Errorf("invalid service %q: %w", cfg.Name, err)
		}

		serviceListeners[cfg.Name] = ListenerKey{Addr: cfg.TrafficPolicy.Listener.Addr, Port: cfg.TrafficPolicy.Listener.Port}
		servicePolicies[cfg.Name] = cfg.TrafficPolicy
		for _, group := range cfg.BackendGroups {
			name := strings.TrimSpace(group.Name)
			if _, exists := backendGroups[name]; exists {
				return Plan{}, fmt.Errorf("duplicated backend group name: %s", name)
			}
			backendGroups[name] = group
			relaySet := make(map[string]struct{})
			for _, target := range group.Targets {
				if isRelayTarget(target) {
					relaySet[target.Addr] = struct{}{}
				}
			}
			backendRelayAddrs[name] = relaySet
			backendStrategy[name] = normalizeLBStrategy(cfg.TrafficPolicy.LB.Strategy)
		}

		key := listenerMapKey(cfg.TrafficPolicy.Listener.Addr, cfg.TrafficPolicy.Listener.Port)
		lp := listeners[key]
		if lp.Listener.Port == 0 {
			lp.Listener = ListenerKey{Addr: cfg.TrafficPolicy.Listener.Addr, Port: cfg.TrafficPolicy.Listener.Port}
		}

		for _, route := range cfg.Routes {
			lp.Routes = append(lp.Routes, RouteBinding{
				ServiceName: cfg.Name,
				Hosts:       append([]string(nil), route.Hosts...),
				PathPrefix:  route.PathPrefix,
				Destination: route.Destination,
				Weight:      route.Weight,
			})
		}
		listeners[key] = lp
	}

	for key, lp := range listeners {
		sort.SliceStable(lp.Routes, func(i, j int) bool {
			if len(lp.Routes[i].PathPrefix) != len(lp.Routes[j].PathPrefix) {
				return len(lp.Routes[i].PathPrefix) > len(lp.Routes[j].PathPrefix)
			}
			if lp.Routes[i].Weight != lp.Routes[j].Weight {
				return lp.Routes[i].Weight > lp.Routes[j].Weight
			}
			if lp.Routes[i].ServiceName != lp.Routes[j].ServiceName {
				return lp.Routes[i].ServiceName < lp.Routes[j].ServiceName
			}
			return lp.Routes[i].Destination < lp.Routes[j].Destination
		})
		listeners[key] = lp
	}

	return Plan{
		listeners:        listeners,
		backendGroups:    backendGroups,
		backendRelayAddrs: backendRelayAddrs,
		backendStrategy:  backendStrategy,
		serviceListeners: serviceListeners,
		servicePolicies:  servicePolicies,
		state:            newPlanState(),
	}, nil
}

// Listeners 返回按地址端口排序后的监听规划快照。
func (p Plan) Listeners() []ListenerPlan {
	items := make([]ListenerPlan, 0, len(p.listeners))
	for _, listener := range p.listeners {
		items = append(items, listener)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Listener.Addr != items[j].Listener.Addr {
			return items[i].Listener.Addr < items[j].Listener.Addr
		}
		return items[i].Listener.Port < items[j].Listener.Port
	})
	return items
}

// Match 在指定监听上下文内按 Host+Path 规则挑选最佳路由。
func (p Plan) Match(addr string, port int, host string, path string) (MatchResult, bool) {
	listener, ok := p.listeners[listenerMapKey(strings.TrimSpace(addr), port)]
	if !ok {
		return MatchResult{}, false
	}

	host = normalizeHost(host)
	bestScore := -1
	best := MatchResult{}

	for _, route := range listener.Routes {
		hostScore, hostMatched := hostMatchScore(route.Hosts, host)
		if !hostMatched {
			continue
		}
		if !strings.HasPrefix(path, route.PathPrefix) {
			continue
		}

		score := hostScore*100000 + len(route.PathPrefix)*100 + route.Weight
		if score > bestScore {
			bestScore = score
			best = MatchResult{
				Listener:    listener.Listener,
				ServiceName: route.ServiceName,
				Destination: route.Destination,
				PathPrefix:  route.PathPrefix,
			}
		}
	}

	if bestScore < 0 {
		return MatchResult{}, false
	}
	return best, true
}

// ResolveDestination 将 destination 解析为可直连上游地址。
func (p Plan) ResolveDestination(destination string) (string, error) {
	destination = strings.TrimSpace(destination)
	if destination == "" {
		return "", fmt.Errorf("route destination is empty")
	}

	if group, ok := p.backendGroups[destination]; ok {
		strategy := p.backendStrategy[destination]
		target, err := p.selectBackendTarget(destination, group, strategy)
		if err != nil {
			return "", err
		}
		return target, nil
	}

	if listener, ok := p.serviceListeners[destination]; ok {
		addr := listener.Addr
		if addr == "0.0.0.0" {
			addr = "127.0.0.1"
		}
		return net.JoinHostPort(addr, strconv.Itoa(listener.Port)), nil
	}

	if isDirectDestination(destination) {
		return destination, nil
	}

	return "", fmt.Errorf("destination %q cannot be resolved (expect backend_group, service name, or host:port)", destination)
}

// ResolveDestinationsForAttempts 返回给定尝试次数下的候选上游地址列表。
func (p Plan) ResolveDestinationsForAttempts(destination string, attempts int) ([]string, error) {
	if attempts <= 0 {
		return nil, fmt.Errorf("attempts must be greater than 0")
	}

	destination = strings.TrimSpace(destination)
	if destination == "" {
		return nil, fmt.Errorf("route destination is empty")
	}

	if group, ok := p.backendGroups[destination]; ok {
		strategy := p.backendStrategy[destination]
		return p.backendCandidatesForAttempts(destination, group, strategy, attempts)
	}

	resolved, err := p.ResolveDestination(destination)
	if err != nil {
		return nil, err
	}

	items := make([]string, 0, attempts)
	for i := 0; i < attempts; i++ {
		items = append(items, resolved)
	}
	return items, nil
}

func (p Plan) backendCandidatesForAttempts(groupName string, group model.BackendGroup, strategy string, attempts int) ([]string, error) {
	if len(group.Targets) == 0 {
		return nil, fmt.Errorf("backend group %q has no targets", group.Name)
	}

	ordered := orderedBackendTargets(groupName, group, strategy, p.state)
	if len(ordered) == 0 {
		return nil, fmt.Errorf("backend group %q has no ordered targets", group.Name)
	}

	items := make([]string, 0, attempts)
	for i := 0; i < attempts; i++ {
		items = append(items, ordered[i%len(ordered)])
	}
	return items, nil
}

func orderedBackendTargets(groupName string, group model.BackendGroup, strategy string, state *planState) []string {
	directTargets, relayTargets := splitDirectAndRelayTargets(group.Targets)

	directOrdered := orderTargetsByStrategy(groupName+"#direct", directTargets, strategy, state)
	relayOrdered := orderTargetsByStrategy(groupName+"#relay", relayTargets, strategy, state)

	ordered := make([]model.BackendTarget, 0, len(directOrdered)+len(relayOrdered))
	ordered = append(ordered, directOrdered...)
	ordered = append(ordered, relayOrdered...)

	result := make([]string, 0, len(ordered))
	for _, target := range ordered {
		result = append(result, target.Addr)
	}
	return result
}

func orderTargetsByStrategy(groupName string, targets []model.BackendTarget, strategy string, state *planState) []model.BackendTarget {
	if len(targets) == 0 {
		return nil
	}

	resolved := normalizeLBStrategy(strategy)
	ordered := make([]model.BackendTarget, len(targets))
	copy(ordered, targets)

	switch resolved {
	case "round-robin":
		start := nextRoundRobinIndex(groupName, len(ordered), state)
		rotated := make([]model.BackendTarget, 0, len(ordered))
		for i := 0; i < len(ordered); i++ {
			idx := (start + i) % len(ordered)
			rotated = append(rotated, ordered[idx])
		}
		return rotated
	case "random":
		shuffleBackendTargets(ordered, state)
	default:
		sort.SliceStable(ordered, func(i, j int) bool {
			if ordered[i].Weight != ordered[j].Weight {
				return ordered[i].Weight > ordered[j].Weight
			}
			return ordered[i].Addr < ordered[j].Addr
		})
	}
	return ordered
}


func splitDirectAndRelayTargets(targets []model.BackendTarget) ([]model.BackendTarget, []model.BackendTarget) {
	direct := make([]model.BackendTarget, 0, len(targets))
	relay := make([]model.BackendTarget, 0, len(targets))
	for _, target := range targets {
		if isRelayTarget(target) {
			relay = append(relay, target)
			continue
		}
		direct = append(direct, target)
	}
	return direct, relay
}

func isRelayTarget(target model.BackendTarget) bool {
	if len(target.Tags) == 0 {
		return false
	}
	for key, value := range target.Tags {
		if strings.EqualFold(strings.TrimSpace(key), "relay") {
			v := strings.ToLower(strings.TrimSpace(value))
			return v == "1" || v == "true" || v == "yes" || v == "on"
		}
	}
	return false
}

func shuffleBackendTargets(targets []model.BackendTarget, state *planState) {
	if len(targets) <= 1 {
		return
	}

	if state == nil {
		rand.Shuffle(len(targets), func(i, j int) {
			targets[i], targets[j] = targets[j], targets[i]
		})
		return
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	state.rng.Shuffle(len(targets), func(i, j int) {
		targets[i], targets[j] = targets[j], targets[i]
	})
}

// ServicePolicy 返回指定服务的流量策略。
func (p Plan) ServicePolicy(serviceName string) (model.ServiceTrafficPolicy, bool) {
	policy, ok := p.servicePolicies[strings.TrimSpace(serviceName)]
	if !ok {
		return model.ServiceTrafficPolicy{}, false
	}
	return policy, true
}

// IsRelayCandidate 判断解析出的目标是否属于 relay 候选。
func (p Plan) IsRelayCandidate(destination string, resolved string) bool {
	relaySet, ok := p.backendRelayAddrs[strings.TrimSpace(destination)]
	if !ok || len(relaySet) == 0 {
		return false
	}
	_, hit := relaySet[strings.TrimSpace(resolved)]
	return hit
}

// listenerMapKey 生成监听器在规划表中的唯一键。
func listenerMapKey(addr string, port int) string {
	return strings.TrimSpace(addr) + ":" + strconv.Itoa(port)
}

// normalizeHost 归一化请求 Host 并去掉端口后缀。
func normalizeHost(host string) string {
	h := strings.ToLower(strings.TrimSpace(host))
	if idx := strings.Index(h, ":"); idx >= 0 {
		return h[:idx]
	}
	return h
}

// hostMatchScore 计算 Host 命中优先级并判断是否命中。
func hostMatchScore(routeHosts []string, requestHost string) (int, bool) {
	best := -1
	for _, host := range routeHosts {
		trimmed := strings.ToLower(strings.TrimSpace(host))
		switch {
		case trimmed == requestHost:
			if best < 2 {
				best = 2
			}
		case trimmed == "*":
			if best < 1 {
				best = 1
			}
		}
	}
	if best < 0 {
		return 0, false
	}
	return best, true
}

// isDirectDestination 判断 destination 是否为可直接代理的地址格式。
func isDirectDestination(destination string) bool {
	if _, _, err := net.SplitHostPort(destination); err == nil {
		return true
	}
	if strings.Contains(destination, "://") {
		u, err := url.Parse(destination)
		if err != nil {
			return false
		}
		if (u.Scheme != "http" && u.Scheme != "https") || strings.TrimSpace(u.Host) == "" {
			return false
		}
		return true
	}
	return false
}

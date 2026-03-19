package traffic

import (
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"fluxmesh/internal/model"
)

type planState struct {
	mu         sync.Mutex
	rrCounters map[string]uint64
	rng        *rand.Rand
}

// Balancer 定义后端目标选择策略的可插拔接口。
type Balancer interface {
	Pick(groupName string, group model.BackendGroup, state *planState) (string, error)
}

var (
	balancerRegistryMu sync.RWMutex
	balancerRegistry   = map[string]Balancer{}
)

// newPlanState 创建算法状态容器（轮询计数与随机源）。
func newPlanState() *planState {
	return &planState{
		rrCounters: make(map[string]uint64),
		rng:        rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// RegisterBalancer 注册自定义负载均衡策略。
func RegisterBalancer(name string, balancer Balancer) error {
	strategy := normalizeLBStrategy(name)
	if strategy == "" {
		return fmt.Errorf("balancer name is required")
	}
	if balancer == nil {
		return fmt.Errorf("balancer implementation is required")
	}

	balancerRegistryMu.Lock()
	defer balancerRegistryMu.Unlock()
	if _, exists := balancerRegistry[strategy]; exists {
		return fmt.Errorf("balancer %q already registered", strategy)
	}
	balancerRegistry[strategy] = balancer
	return nil
}

// selectBackendTarget 按策略从后端组中选择上游目标。
func (p Plan) selectBackendTarget(groupName string, group model.BackendGroup, strategy string) (string, error) {
	if len(group.Targets) == 0 {
		return "", fmt.Errorf("backend group %q has no targets", group.Name)
	}

	resolved := normalizeLBStrategy(strategy)
	balancer := getRegisteredBalancer(resolved)
	if balancer == nil {
		return "", fmt.Errorf("balancer strategy %q is not registered", resolved)
	}

	return balancer.Pick(groupName, group, p.state)
}

// getRegisteredBalancer 获取已注册的负载均衡策略实现。
func getRegisteredBalancer(name string) Balancer {
	balancerRegistryMu.RLock()
	defer balancerRegistryMu.RUnlock()
	return balancerRegistry[name]
}

// nextRoundRobinIndex 返回指定后端组下一次轮询命中的下标。
func nextRoundRobinIndex(groupName string, targetCount int, state *planState) int {
	if state == nil || targetCount <= 1 {
		return 0
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	cur := state.rrCounters[groupName]
	idx := int(cur % uint64(targetCount))
	state.rrCounters[groupName] = cur + 1
	return idx
}

// weightedRandomTarget 按目标权重随机选择一个上游地址。
func weightedRandomTarget(group model.BackendGroup, state *planState) (string, error) {
	if len(group.Targets) == 1 {
		return group.Targets[0].Addr, nil
	}

	total := 0
	for _, target := range group.Targets {
		total += target.Weight
	}
	if total <= 0 {
		return "", fmt.Errorf("backend group %q has invalid total weight", group.Name)
	}

	pick := 0
	if state != nil {
		state.mu.Lock()
		pick = state.rng.Intn(total)
		state.mu.Unlock()
	} else {
		pick = rand.Intn(total)
	}

	acc := 0
	for _, target := range group.Targets {
		acc += target.Weight
		if pick < acc {
			return target.Addr, nil
		}
	}

	return group.Targets[len(group.Targets)-1].Addr, nil
}

// normalizeLBStrategy 归一化负载均衡策略名称并兜底默认策略。
func normalizeLBStrategy(strategy string) string {
	s := strings.ToLower(strings.TrimSpace(strategy))
	switch s {
	case "":
		return "load-first"
	case "load-first", "latency-first":
		return s
	case "round-robin", "rr":
		return "round-robin"
	case "random", "rand":
		return "random"
	default:
		return s
	}
}

type loadFirstBalancer struct{}

func (b *loadFirstBalancer) Pick(_ string, group model.BackendGroup, _ *planState) (string, error) {
	best := group.Targets[0]
	for _, target := range group.Targets[1:] {
		if target.Weight > best.Weight {
			best = target
			continue
		}
		if target.Weight == best.Weight && target.Addr < best.Addr {
			best = target
		}
	}
	return best.Addr, nil
}

type roundRobinBalancer struct{}

func (b *roundRobinBalancer) Pick(groupName string, group model.BackendGroup, state *planState) (string, error) {
	idx := nextRoundRobinIndex(groupName, len(group.Targets), state)
	return group.Targets[idx].Addr, nil
}

type randomBalancer struct{}

func (b *randomBalancer) Pick(_ string, group model.BackendGroup, state *planState) (string, error) {
	return weightedRandomTarget(group, state)
}

func init() {
	balancerRegistry["load-first"] = &loadFirstBalancer{}
	balancerRegistry["latency-first"] = &loadFirstBalancer{} // TODO: latency-first 功能待实现
	balancerRegistry["round-robin"] = &roundRobinBalancer{}
	balancerRegistry["random"] = &randomBalancer{}
}

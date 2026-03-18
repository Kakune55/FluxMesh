package app

import (
	"context"
	"fmt"
	"math"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"fluxmesh/internal/config"
	"fluxmesh/internal/etcd"
	httpapi "fluxmesh/internal/httpapi"
	"fluxmesh/internal/logx"
	"fluxmesh/internal/model"
	"fluxmesh/internal/netutil"
	"fluxmesh/internal/reconcile"
	"fluxmesh/internal/registry"
	"fluxmesh/internal/softkv"
	"fluxmesh/internal/sysmetrics"
	"fluxmesh/internal/traffic"

	clientv3 "go.etcd.io/etcd/client/v3"
)

type App struct {
	cfg          config.Config
	embedded     *etcd.EmbeddedServer
	client       *clientv3.Client
	nodes        *registry.Service
	services     *registry.Services
	softStore    *softkv.Store
	softBus      *softkv.Bus
	softWriter   *softkv.Writer
	http         *httpapi.Server
	traffic      *traffic.Server
	reconciler   *reconcile.MemberReconciler
	metrics      *sysmetrics.Collector
	selfNode     model.Node
	nodeMu       sync.RWMutex
	leaseID      clientv3.LeaseID
	keepAliveCh  <-chan *clientv3.LeaseKeepAliveResponse
	leaseMu      sync.RWMutex
	appCtx       context.Context
	cancel       context.CancelFunc
	backgroundWG sync.WaitGroup
}

func New(cfg config.Config) (*App, error) {
	if cfg.IP == "auto" {
		ip, err := netutil.DetectLANIPv4()
		if err != nil {
			return nil, err
		}
		cfg.IP = ip
	}

	if cfg.ClientAdvertiseURL == "" {
		cfg.ClientAdvertiseURL = strings.Replace(cfg.ClientListenURL, "0.0.0.0", cfg.IP, 1)
	}
	if cfg.PeerAdvertiseURL == "" {
		cfg.PeerAdvertiseURL = strings.Replace(cfg.PeerListenURL, "0.0.0.0", cfg.IP, 1)
	}

	store := softkv.NewStore()
	bus := softkv.NewBus(256)

	return &App{
		cfg:        cfg,
		reconciler: reconcile.NewMemberReconciler(),
		metrics:    sysmetrics.NewCollector(),
		softStore:  store,
		softBus:    bus,
		softWriter: softkv.NewWriter(store, bus),
	}, nil
}

func (a *App) Run(parent context.Context) error {
	ctx, cancel := context.WithCancel(parent)
	a.appCtx = ctx
	a.cancel = cancel

	// 后台执行 Pending-Reconcile 任务，兜底清理失败成员。
	a.backgroundWG.Add(1)
	go func() {
		defer a.backgroundWG.Done()
		a.reconciler.Run(ctx)
	}()

	if err := a.startEtcdAndClient(ctx); err != nil {
		cancel()
		a.backgroundWG.Wait()
		return err
	}

	a.nodes = registry.NewService(a.client)
	a.services = registry.NewServices(a.client)
	a.traffic = traffic.NewServer(a.services)
	// 首次注册节点元信息，并绑定租约确保失联自动过期。
	nodeStatus := model.NodeStatus{
		NodeRole:   string(a.cfg.Role),
		NodeStatus: "Ready",
	}

	node := model.Node{
		ID:         a.cfg.NodeID,
		IP:         a.cfg.IP,
		Version:    a.cfg.Version,
		NodeStatus: nodeStatus,
	}
	a.selfNode = node

	if err := a.registerNodeLease(ctx); err != nil {
		return err
	}

	a.backgroundWG.Add(1)
	go func() {
		defer a.backgroundWG.Done()
		a.consumeKeepAlive(ctx)
	}()

	a.backgroundWG.Add(1)
	go func() {
		defer a.backgroundWG.Done()
		a.monitorNodeMetrics(ctx)
	}()

	a.backgroundWG.Add(1)
	go func() {
		defer a.backgroundWG.Done()
		a.gcSoftState(ctx)
	}()

	a.backgroundWG.Add(1)
	go func() {
		defer a.backgroundWG.Done()
		a.runSoftKVBus(ctx)
	}()

	if err := a.traffic.Start(ctx); err != nil {
		return err
	}

	a.http = httpapi.NewServer(a.cfg.AdminAddr, a.nodes, a.services, a.softStore, a.cfg.Version)
	return a.http.Start()
}

func (a *App) Shutdown(ctx context.Context) error {
	if a.cancel != nil {
		a.cancel()
	}

	var shutdownErr error
	if a.http != nil {
		if err := a.http.Shutdown(ctx); err != nil {
			shutdownErr = err
		}
	}

	if a.traffic != nil {
		if err := a.traffic.Shutdown(ctx); err != nil && shutdownErr == nil {
			shutdownErr = err
		}
	}

	if a.nodes != nil {
		revokeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		if err := a.nodes.Revoke(revokeCtx, a.currentLeaseID()); err != nil {
			shutdownErr = err
		}
		cancel()
	}

	if a.client != nil {
		if err := a.client.Close(); err != nil {
			shutdownErr = err
		}
	}

	if a.embedded != nil {
		a.embedded.Close()
	}

	a.backgroundWG.Wait()
	return shutdownErr
}

func (a *App) startEtcdAndClient(ctx context.Context) error {
	if a.cfg.Role == config.RoleAgent {
		// Agent 仅连接已有控制面，不启动本地 etcd。
		cli, err := newClient(a.cfg.SeedEndpoints)
		if err != nil {
			return err
		}
		a.client = cli
		return nil
	}

	initialCluster := fmt.Sprintf("%s=%s", a.cfg.NodeID, a.cfg.PeerAdvertiseURL)
	var joinResult etcd.JoinResult
	var joined bool

	if a.cfg.ClusterState == config.ClusterStateExisting {
		// existing 模式先 MemberAdd，再按返回拓扑生成 initial-cluster。
		result, err := etcd.JoinExistingCluster(ctx, a.cfg.SeedEndpoints, a.cfg.NodeID, a.cfg.PeerAdvertiseURL)
		if err != nil {
			return err
		}
		joinResult = result
		initialCluster = result.InitialCluster
		joined = true
	}

	embedded, err := etcd.StartEmbeddedServer(a.cfg, initialCluster)
	if err != nil {
		if joined {
			if rollbackErr := etcd.RollbackMember(ctx, joinResult.SeedEndpoints, joinResult.MemberID); rollbackErr != nil {
				logx.Warn("成员回滚失败，加入待协调队列", "member_id", joinResult.MemberID, "err", rollbackErr)
				a.reconciler.Add(joinResult.MemberID, joinResult.SeedEndpoints)
			}
		}
		return err
	}
	a.embedded = embedded

	cli, err := newClient([]string{a.cfg.ClientAdvertiseURL})
	if err != nil {
		if joined {
			if rollbackErr := etcd.RollbackMember(ctx, joinResult.SeedEndpoints, joinResult.MemberID); rollbackErr != nil {
				logx.Warn("成员回滚失败，加入待协调队列", "member_id", joinResult.MemberID, "err", rollbackErr)
				a.reconciler.Add(joinResult.MemberID, joinResult.SeedEndpoints)
			}
		}
		a.embedded.Close()
		return err
	}
	a.client = cli
	return nil
}

func (a *App) consumeKeepAlive(ctx context.Context) {
	for {
		keepAliveCh := a.currentKeepAliveCh()
		select {
		case <-ctx.Done():
			return
		case resp, ok := <-keepAliveCh:
			if !ok {
				logx.Warn("租约保活通道已关闭")
				if err := a.recoverLease(ctx); err != nil {
					logx.Error("租约重建失败", "err", err)
					return
				}
				continue
			}
			if resp != nil {
				logx.Debug("收到租约保活响应", "lease_id", int64(resp.ID), "ttl", resp.TTL)
			}
		}
	}
}

func (a *App) registerNodeLease(ctx context.Context) error {
	leaseID, keepAliveCh, err := a.nodes.RegisterWithLease(ctx, a.appCtx, a.currentNode(), a.cfg.LeaseTTLSeconds)
	if err != nil {
		return err
	}

	a.leaseMu.Lock()
	a.leaseID = leaseID
	a.keepAliveCh = keepAliveCh
	a.leaseMu.Unlock()
	return nil
}

func (a *App) recoverLease(ctx context.Context) error {
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		attemptCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := a.registerNodeLease(attemptCtx)
		cancel()
		if err == nil {
			logx.Info("租约已重建，节点重新注册成功", "node_id", a.selfNode.ID)
			return nil
		}

		logx.Warn("租约重建失败，准备重试", "err", err, "next_in", backoff.String())
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}

		if backoff < 8*time.Second {
			backoff *= 2
		}
	}
}

func (a *App) currentLeaseID() clientv3.LeaseID {
	a.leaseMu.RLock()
	defer a.leaseMu.RUnlock()
	return a.leaseID
}

func (a *App) currentKeepAliveCh() <-chan *clientv3.LeaseKeepAliveResponse {
	a.leaseMu.RLock()
	defer a.leaseMu.RUnlock()
	return a.keepAliveCh
}

func (a *App) monitorNodeMetrics(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.updateNodeMetrics(ctx)
		}
	}
}

func (a *App) updateNodeMetrics(ctx context.Context) {
	snapshot, err := a.metrics.Collect()
	if err != nil {
		logx.Warn("采集系统指标失败", "err", err)
		return
	}

	node := a.currentNode()
	snapshot.CPUUsage = round2(snapshot.CPUUsage)
	snapshot.MemoryUsage = round2(snapshot.MemoryUsage)
	snapshot.SystemLoad1m = round2(snapshot.SystemLoad1m)
	snapshot.SystemLoad5m = round2(snapshot.SystemLoad5m)
	snapshot.SystemLoad15m = round2(snapshot.SystemLoad15m)

	if a.softStore != nil {
		if a.softWriter == nil {
			a.softWriter = softkv.NewWriter(a.softStore, a.softBus)
		}

		result, err := a.softWriter.Write(ctx, "metrics/nodes/"+node.ID, snapshot, 30*time.Second, node.ID)
		if err != nil {
			logx.Warn("写入软状态指标失败", "node_id", node.ID, "err", err)
		} else if result.PublishErr != nil {
			logx.Debug("发布 softkv 事件失败", "node_id", node.ID, "err", result.PublishErr)
		}
	}

	logx.Debug("节点指标已更新",
		"node_id", node.ID,
		"cpu_usage", snapshot.CPUUsage,
		"memory_usage", snapshot.MemoryUsage,
		"system_load_1m", snapshot.SystemLoad1m,
	)
}

func (a *App) currentNode() model.Node {
	a.nodeMu.RLock()
	defer a.nodeMu.RUnlock()
	return a.selfNode
}

func (a *App) gcSoftState(ctx context.Context) {
	if a.softStore == nil {
		return
	}

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.softStore.DeleteExpired(ctx)
		}
	}
}

func (a *App) runSoftKVBus(ctx context.Context) {
	if a.softStore == nil || a.softBus == nil {
		return
	}

	peers := a.discoverGossipPeers(ctx)
	err := softkv.RunMemberlist(ctx, a.softStore, a.softBus.Subscribe(), softkv.MemberlistOptions{
		NodeID:        a.cfg.NodeID,
		AdvertiseAddr: a.cfg.IP,
		BindPort:      softkv.DefaultGossipPort,
		Join:          peers,
	})
	if err != nil {
		logx.Warn("softkv gossip 启动失败，回退 loopback", "err", err)
		softkv.RunLoopback(ctx, a.softStore, a.softBus.Subscribe())
	}
}

func (a *App) discoverGossipPeers(ctx context.Context) []string {
	if a.nodes == nil {
		return nil
	}

	listCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	nodes, err := a.nodes.ListNodes(listCtx)
	if err != nil {
		logx.Debug("读取节点列表用于 gossip join 失败", "err", err)
		return nil
	}

	peers := make([]string, 0, len(nodes))
	seen := make(map[string]struct{}, len(nodes))
	for _, node := range nodes {
		if node.ID == a.cfg.NodeID || strings.TrimSpace(node.IP) == "" {
			continue
		}
		addr := net.JoinHostPort(node.IP, strconv.Itoa(softkv.DefaultGossipPort))
		if _, ok := seen[addr]; ok {
			continue
		}
		seen[addr] = struct{}{}
		peers = append(peers, addr)
	}
	return peers
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

func newClient(endpoints []string) (*clientv3.Client, error) {
	if len(endpoints) == 0 {
		return nil, fmt.Errorf("empty etcd endpoints")
	}
	return clientv3.New(clientv3.Config{
		Endpoints:   endpoints,
		DialTimeout: 5 * time.Second,
	})
}

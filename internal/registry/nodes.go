package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"fluxmesh/internal/logx"
	"fluxmesh/internal/model"

	clientv3 "go.etcd.io/etcd/client/v3"
)

const nodesPrefix = "/mesh/nodes/"

type Service struct {
	cli    *clientv3.Client
	kv     clientv3.KV
	lease  clientv3.Lease
	watch  clientv3.Watcher
}

func NewService(cli *clientv3.Client) *Service {
	return &Service{cli: cli, kv: cli, lease: cli, watch: cli}
}

func (s *Service) RegisterWithLease(opCtx, keepAliveCtx context.Context, node model.Node, ttl int64) (clientv3.LeaseID, <-chan *clientv3.LeaseKeepAliveResponse, error) {
	// 申请租约并把节点注册键绑定到该租约，失联后自动过期删除。
	leaseResp, err := s.lease.Grant(opCtx, ttl)
	if err != nil {
		return 0, nil, err
	}

	key := nodeKey(node.ID)
	payload, err := json.Marshal(node)
	if err != nil {
		return 0, nil, err
	}

	_, err = s.kv.Put(opCtx, key, string(payload), clientv3.WithLease(leaseResp.ID))
	if err != nil {
		return 0, nil, err
	}

	keepAliveCh, err := s.lease.KeepAlive(keepAliveCtx, leaseResp.ID)
	if err != nil {
		return 0, nil, err
	}

	logx.Info("节点已完成租约注册", "node", node.ID, "lease_id", int64(leaseResp.ID), "ttl", ttl)
	return leaseResp.ID, keepAliveCh, nil
}

func (s *Service) Revoke(ctx context.Context, leaseID clientv3.LeaseID) error {
	if leaseID == 0 {
		return nil
	}
	_, err := s.lease.Revoke(ctx, leaseID)
	return err
}

func (s *Service) UpdateNodeWithLease(ctx context.Context, node model.Node, leaseID clientv3.LeaseID) error {
	payload, err := json.Marshal(node)
	if err != nil {
		return err
	}

	_, err = s.kv.Put(ctx, nodeKey(node.ID), string(payload), clientv3.WithLease(leaseID))
	return err
}

func (s *Service) ListNodes(ctx context.Context) ([]model.Node, error) {
	resp, err := s.kv.Get(ctx, nodesPrefix, clientv3.WithPrefix())
	if err != nil {
		return nil, err
	}

	nodes := make([]model.Node, 0, len(resp.Kvs))
	for _, kv := range resp.Kvs {
		var node model.Node
		if err := json.Unmarshal(kv.Value, &node); err != nil {
			continue
		}
		nodes = append(nodes, node)
	}

	return nodes, nil
}

func (s *Service) ListNodesWithEtcdRole(ctx context.Context) ([]model.Node, error) {
	nodes, err := s.ListNodes(ctx)
	if err != nil {
		return nil, err
	}

	if len(nodes) == 0 {
		return nodes, nil
	}

	endpoint := ""
	for _, e := range s.cli.Endpoints() {
		e = strings.TrimSpace(e)
		if e != "" {
			endpoint = e
			break
		}
	}
	if endpoint == "" {
		return markUnknownEtcdRoles(nodes), nil
	}

	statusResp, err := s.cli.Status(ctx, endpoint)
	if err != nil {
		logx.Warn("查询 etcd 选举状态失败", "endpoint", endpoint, "err", err)
		return markUnknownEtcdRoles(nodes), nil
	}

	membersResp, err := s.cli.MemberList(ctx)
	if err != nil {
		logx.Warn("查询 etcd 成员列表失败", "endpoint", endpoint, "err", err)
		return markUnknownEtcdRoles(nodes), nil
	}

	memberRoleByName := make(map[string]string, len(membersResp.Members))
	for _, member := range membersResp.Members {
		if member == nil {
			continue
		}
		if member.ID == statusResp.Leader {
			memberRoleByName[member.Name] = "leader"
			continue
		}
		memberRoleByName[member.Name] = "follower"
	}

	for i := range nodes {
		if nodes[i].NodeStatus.NodeRole != "server" {
			nodes[i].NodeStatus.EtcdRole = "agent"
			continue
		}
		if role, ok := memberRoleByName[nodes[i].ID]; ok {
			nodes[i].NodeStatus.EtcdRole = role
			continue
		}
		nodes[i].NodeStatus.EtcdRole = "unknown"
	}

	return nodes, nil
}

func (s *Service) GetNode(ctx context.Context, id string) (model.Node, error) {
	resp, err := s.kv.Get(ctx, nodeKey(id))
	if err != nil {
		return model.Node{}, err
	}
	if len(resp.Kvs) == 0 {
		return model.Node{}, fmt.Errorf("node not found")
	}

	var node model.Node
	if err := json.Unmarshal(resp.Kvs[0].Value, &node); err != nil {
		return model.Node{}, err
	}
	return node, nil
}

func nodeKey(id string) string {
	return nodesPrefix + strings.TrimSpace(id)
}

func markUnknownEtcdRoles(nodes []model.Node) []model.Node {
	for i := range nodes {
		if nodes[i].NodeStatus.NodeRole == "server" {
			nodes[i].NodeStatus.EtcdRole = "unknown"
			continue
		}
		nodes[i].NodeStatus.EtcdRole = "agent"
	}
	return nodes
}

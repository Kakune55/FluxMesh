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
	client clientv3.KV
	lease  clientv3.Lease
	watch  clientv3.Watcher
}

func NewService(cli *clientv3.Client) *Service {
	return &Service{client: cli, lease: cli, watch: cli}
}

func (s *Service) RegisterWithLease(ctx context.Context, node model.Node, ttl int64) (clientv3.LeaseID, <-chan *clientv3.LeaseKeepAliveResponse, error) {
	// 申请租约并把节点注册键绑定到该租约，失联后自动过期删除。
	leaseResp, err := s.lease.Grant(ctx, ttl)
	if err != nil {
		return 0, nil, err
	}

	key := nodeKey(node.ID)
	payload, err := json.Marshal(node)
	if err != nil {
		return 0, nil, err
	}

	_, err = s.client.Put(ctx, key, string(payload), clientv3.WithLease(leaseResp.ID))
	if err != nil {
		return 0, nil, err
	}

	keepAliveCh, err := s.lease.KeepAlive(ctx, leaseResp.ID)
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

	_, err = s.client.Put(ctx, nodeKey(node.ID), string(payload), clientv3.WithLease(leaseID))
	return err
}

func (s *Service) ListNodes(ctx context.Context) ([]model.Node, error) {
	resp, err := s.client.Get(ctx, nodesPrefix, clientv3.WithPrefix())
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

func (s *Service) GetNode(ctx context.Context, id string) (model.Node, error) {
	resp, err := s.client.Get(ctx, nodeKey(id))
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

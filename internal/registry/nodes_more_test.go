package registry_test

import (
	"testing"

	"fluxmesh/internal/model"
	"fluxmesh/internal/registry"
	"fluxmesh/internal/testutil/etcdtest"
)

func TestNodesListUpdateRoleStatusAndRevoke(t *testing.T) {
	emb := etcdtest.Start(t)
	nodes := registry.NewService(emb.Client)

	keepAliveCtx, cancel := t.Context(), func() {}
	_ = cancel

	server := model.Node{
		ID:      "test-node",
		IP:      "127.0.0.1",
		Version: "v1",
		NodeStatus: model.NodeStatus{
			NodeRole:   "server",
			NodeStatus: "Ready",
		},
	}
	agent := model.Node{
		ID:      "agent-1",
		IP:      "127.0.0.1",
		Version: "v1",
		NodeStatus: model.NodeStatus{
			NodeRole:   "agent",
			NodeStatus: "Ready",
		},
	}

	leaseServer, _, err := nodes.RegisterWithLease(t.Context(), keepAliveCtx, server, 30)
	if err != nil {
		t.Fatalf("register server failed: %v", err)
	}
	leaseAgent, _, err := nodes.RegisterWithLease(t.Context(), keepAliveCtx, agent, 30)
	if err != nil {
		t.Fatalf("register agent failed: %v", err)
	}

	list, err := nodes.ListNodes(t.Context())
	if err != nil {
		t.Fatalf("list nodes failed: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(list))
	}

	updated := server
	updated.NodeStatus.NodeStatus = "Draining"
	if err := nodes.UpdateNodeWithLease(t.Context(), updated, leaseServer); err != nil {
		t.Fatalf("update node with lease failed: %v", err)
	}
	got, err := nodes.GetNode(t.Context(), server.ID)
	if err != nil {
		t.Fatalf("get updated node failed: %v", err)
	}
	if got.NodeStatus.NodeStatus != "Draining" {
		t.Fatalf("expected node status Draining, got %s", got.NodeStatus.NodeStatus)
	}

	withRole, err := nodes.ListNodesWithEtcdRole(t.Context())
	if err != nil {
		t.Fatalf("list nodes with etcd role failed: %v", err)
	}
	if len(withRole) != 2 {
		t.Fatalf("expected 2 nodes with role info, got %d", len(withRole))
	}

	seenServer := false
	seenAgent := false
	for i := range withRole {
		if withRole[i].ID == server.ID {
			seenServer = true
			if withRole[i].NodeStatus.EtcdRole == "" {
				t.Fatalf("expected server etcd role to be set")
			}
		}
		if withRole[i].ID == agent.ID {
			seenAgent = true
			if withRole[i].NodeStatus.EtcdRole != "agent" {
				t.Fatalf("expected agent etcd role=agent, got %s", withRole[i].NodeStatus.EtcdRole)
			}
		}
	}
	if !seenServer || !seenAgent {
		t.Fatalf("expected to see both server and agent nodes")
	}

	status, err := nodes.ClusterStatus(t.Context())
	if err != nil {
		t.Fatalf("cluster status failed: %v", err)
	}
	if status.Endpoint == "" || len(status.Members) == 0 {
		t.Fatalf("unexpected cluster status payload: %+v", status)
	}

	if err := nodes.Revoke(t.Context(), 0); err != nil {
		t.Fatalf("revoke leaseID=0 should succeed, got %v", err)
	}
	if err := nodes.Revoke(t.Context(), leaseAgent); err != nil {
		t.Fatalf("revoke agent lease failed: %v", err)
	}
}

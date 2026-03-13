package registry_test

import (
	"context"
	"errors"
	"testing"

	"fluxmesh/internal/model"
	"fluxmesh/internal/registry"
	"fluxmesh/internal/testutil/etcdtest"
)

func TestNodeEvictSemantics(t *testing.T) {
	emb := etcdtest.Start(t)
	nodes := registry.NewService(emb.Client)
	opCtx := context.Background()
	keepAliveCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	t.Run("evict agent removes node key", func(t *testing.T) {
		agent := model.Node{
			ID:      "agent-1",
			IP:      "127.0.0.1",
			Version: "v0.1.0",
			NodeStatus: model.NodeStatus{
				NodeRole:   "agent",
				NodeStatus: "Ready",
			},
		}
		if _, _, err := nodes.RegisterWithLease(opCtx, keepAliveCtx, agent, 30); err != nil {
			t.Fatalf("register agent failed: %v", err)
		}

		result, err := nodes.EvictNode(opCtx, agent.ID, false)
		if err != nil {
			t.Fatalf("evict agent failed: %v", err)
		}
		if !result.NodeDeleted {
			t.Fatalf("expected node to be deleted")
		}
		if result.MemberRemoved {
			t.Fatalf("agent eviction should not remove raft member")
		}

		_, err = nodes.GetNode(opCtx, agent.ID)
		if !errors.Is(err, registry.ErrNodeNotFound) {
			t.Fatalf("expected node not found, got %v", err)
		}
	})

	t.Run("evict server without raft member only removes node key", func(t *testing.T) {
		server := model.Node{
			ID:      "ghost-server",
			IP:      "127.0.0.1",
			Version: "v0.1.0",
			NodeStatus: model.NodeStatus{
				NodeRole:   "server",
				NodeStatus: "Ready",
			},
		}
		if _, _, err := nodes.RegisterWithLease(opCtx, keepAliveCtx, server, 30); err != nil {
			t.Fatalf("register server failed: %v", err)
		}

		result, err := nodes.EvictNode(opCtx, server.ID, false)
		if err != nil {
			t.Fatalf("evict server failed: %v", err)
		}
		if !result.NodeDeleted {
			t.Fatalf("expected node to be deleted")
		}
		if result.MemberRemoved {
			t.Fatalf("unexpected member remove for non-member server")
		}
	})

	t.Run("evict leader requires force", func(t *testing.T) {
		leaderNode := model.Node{
			ID:      "test-node",
			IP:      "127.0.0.1",
			Version: "v0.1.0",
			NodeStatus: model.NodeStatus{
				NodeRole:   "server",
				NodeStatus: "Ready",
			},
		}
		if _, _, err := nodes.RegisterWithLease(opCtx, keepAliveCtx, leaderNode, 30); err != nil {
			t.Fatalf("register leader node failed: %v", err)
		}

		_, err := nodes.EvictNode(opCtx, leaderNode.ID, false)
		if !errors.Is(err, registry.ErrLeaderEvictRequiresForce) {
			t.Fatalf("expected leader force error, got %v", err)
		}

		if _, err := nodes.GetNode(opCtx, leaderNode.ID); err != nil {
			t.Fatalf("leader node should remain after rejected evict: %v", err)
		}
	})
}

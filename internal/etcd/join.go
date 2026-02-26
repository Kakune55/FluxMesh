package etcd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"fluxmesh/internal/logx"

	"go.etcd.io/etcd/api/v3/etcdserverpb"
	clientv3 "go.etcd.io/etcd/client/v3"
)

type JoinResult struct {
	InitialCluster string
	MemberID       uint64
	SeedEndpoints  []string
}

func JoinExistingCluster(ctx context.Context, seedEndpoints []string, nodeName, peerAdvertiseURL string) (JoinResult, error) {
	// existing 模式：通过 MemberAdd 预注册自身，再返回最新拓扑。
	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   seedEndpoints,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		return JoinResult{}, err
	}
	defer cli.Close()

	requestCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	addResp, err := cli.MemberAdd(requestCtx, []string{peerAdvertiseURL})
	if err != nil {
		return JoinResult{}, err
	}

	initialCluster := buildInitialCluster(addResp.Members, nodeName, peerAdvertiseURL)
	if initialCluster == "" {
		return JoinResult{}, fmt.Errorf("failed to build initial cluster from member list")
	}

	logx.Info("节点已加入集群成员列表", "member_id", addResp.Member.ID)
	return JoinResult{
		InitialCluster: initialCluster,
		MemberID:       addResp.Member.ID,
		SeedEndpoints:  append([]string(nil), seedEndpoints...),
	}, nil
}

func RollbackMember(ctx context.Context, seedEndpoints []string, memberID uint64) error {
	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   seedEndpoints,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		return err
	}
	defer cli.Close()

	requestCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	_, err = cli.MemberRemove(requestCtx, memberID)
	if err == nil {
		logx.Info("成员回滚成功", "member_id", memberID)
	}
	return err
}

func buildInitialCluster(members []*etcdserverpb.Member, nodeName, peerAdvertiseURL string) string {
	entries := make([]string, 0, len(members))
	for _, member := range members {
		if len(member.PeerURLs) == 0 {
			continue
		}

		name := member.Name
		if name == "" && containsURL(member.PeerURLs, peerAdvertiseURL) {
			name = nodeName
		}
		if name == "" {
			continue
		}

		entries = append(entries, fmt.Sprintf("%s=%s", name, member.PeerURLs[0]))
	}

	return strings.Join(entries, ",")
}

func containsURL(urls []string, target string) bool {
	for _, item := range urls {
		if item == target {
			return true
		}
	}
	return false
}

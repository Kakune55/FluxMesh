package softkv

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"slices"
	"strconv"
	"strings"

	"fluxmesh/internal/logx"

	"github.com/hashicorp/memberlist"
)

const DefaultGossipPort = 7946

type MemberlistOptions struct {
	NodeID        string
	BindAddr      string
	AdvertiseAddr string
	BindPort      int
	Join          []string
}

type gossipMessage struct {
	Sender string `json:"sender"`
	Event  Event  `json:"event"`
}

type gossipDelegate struct {
	store  *Store
	nodeID string
	queue  *memberlist.TransmitLimitedQueue
}

func (d *gossipDelegate) NodeMeta(int) []byte {
	return nil
}

func (d *gossipDelegate) NotifyMsg(raw []byte) {
	if d.store == nil || len(raw) == 0 {
		return
	}

	msg, err := decodeGossipMessage(raw)
	if err != nil {
		return
	}

	if msg.Sender == d.nodeID {
		return
	}
	if msg.Event.Type != EventPut {
		return
	}

	d.store.Merge(context.Background(), msg.Event.Entry)
}

func (d *gossipDelegate) GetBroadcasts(overhead, limit int) [][]byte {
	if d.queue == nil {
		return nil
	}
	return d.queue.GetBroadcasts(overhead, limit)
}

func (d *gossipDelegate) LocalState(bool) []byte {
	return nil
}

func (d *gossipDelegate) MergeRemoteState([]byte, bool) {
}

type gossipBroadcast struct {
	msg []byte
}

func (b *gossipBroadcast) Invalidates(memberlist.Broadcast) bool {
	return false
}

func (b *gossipBroadcast) Message() []byte {
	return b.msg
}

func (b *gossipBroadcast) Finished() {
}

func RunMemberlist(ctx context.Context, store *Store, events <-chan Event, opts MemberlistOptions) error {
	if store == nil {
		return errors.New("nil store")
	}
	if events == nil {
		return errors.New("nil events")
	}
	if strings.TrimSpace(opts.NodeID) == "" {
		return errors.New("empty node id")
	}

	cfg := memberlist.DefaultLANConfig()
	cfg.Name = opts.NodeID
	cfg.BindAddr = strings.TrimSpace(opts.BindAddr)
	if cfg.BindAddr == "" {
		cfg.BindAddr = "0.0.0.0"
	}
	if opts.BindPort <= 0 {
		opts.BindPort = DefaultGossipPort
	}
	cfg.BindPort = opts.BindPort
	cfg.LogOutput = io.Discard

	if addr := strings.TrimSpace(opts.AdvertiseAddr); addr != "" {
		resolved, resolveErr := resolveAdvertiseAddr(addr)
		if resolveErr != nil {
			logx.Warn("softkv gossip 无法解析 advertise 地址，改用自动探测", "addr", addr, "err", resolveErr)
		} else {
			cfg.AdvertiseAddr = resolved
			cfg.AdvertisePort = opts.BindPort
		}
	}

	delegate := &gossipDelegate{store: store, nodeID: opts.NodeID}
	cfg.Delegate = delegate

	ml, err := memberlist.Create(cfg)
	if err != nil {
		return err
	}
	defer ml.Shutdown()

	queue := &memberlist.TransmitLimitedQueue{
		NumNodes: func() int {
			return ml.NumMembers()
		},
		RetransmitMult: 3,
	}
	delegate.queue = queue

	joinTargets := normalizeJoinTargets(opts.Join, opts.BindPort)
	if len(joinTargets) > 0 {
		if _, err := ml.Join(joinTargets); err != nil {
			logx.Warn("softkv gossip join 失败，先以单节点模式运行", "err", err, "targets", strings.Join(joinTargets, ","))
		}
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case event := <-events:
			if event.Type != EventPut {
				continue
			}

			raw, err := encodeGossipMessage(opts.NodeID, event)
			if err != nil {
				logx.Debug("softkv gossip 编码失败", "err", err)
				continue
			}
			queue.QueueBroadcast(&gossipBroadcast{msg: raw})
		}
	}
}

func encodeGossipMessage(nodeID string, event Event) ([]byte, error) {
	msg := gossipMessage{Sender: nodeID, Event: event}
	return json.Marshal(msg)
}

func decodeGossipMessage(raw []byte) (gossipMessage, error) {
	var msg gossipMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		return gossipMessage{}, err
	}
	if strings.TrimSpace(msg.Sender) == "" {
		return gossipMessage{}, errors.New("empty sender")
	}
	return msg, nil
}

func normalizeJoinTargets(addrs []string, defaultPort int) []string {
	if defaultPort <= 0 {
		defaultPort = DefaultGossipPort
	}

	result := make([]string, 0, len(addrs))
	for _, raw := range addrs {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}

		host, port, err := net.SplitHostPort(raw)
		if err == nil {
			if strings.TrimSpace(host) == "" || strings.TrimSpace(port) == "" {
				continue
			}
			result = append(result, net.JoinHostPort(host, port))
			continue
		}

		result = append(result, net.JoinHostPort(raw, strconv.Itoa(defaultPort)))
	}

	slices.Sort(result)
	return slices.Compact(result)
}

func resolveAdvertiseAddr(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}

	if ip := net.ParseIP(raw); ip != nil {
		return ip.String(), nil
	}

	ips, err := net.LookupIP(raw)
	if err != nil {
		return "", err
	}
	for _, ip := range ips {
		if v4 := ip.To4(); v4 != nil {
			return v4.String(), nil
		}
	}
	if len(ips) > 0 {
		return ips[0].String(), nil
	}

	return "", errors.New("no ip found for advertise host")
}
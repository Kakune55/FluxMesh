package registry

import (
	"context"
	"encoding/json"
	"strings"

	"fluxmesh/internal/model"

	clientv3 "go.etcd.io/etcd/client/v3"
)

const servicesPrefix = "/mesh/services/"

type Services struct {
	kv clientv3.KV
}

func NewServices(cli *clientv3.Client) *Services {
	return &Services{kv: cli}
}

func (s *Services) Put(ctx context.Context, cfg model.ServiceConfig) error {
	payload, err := json.Marshal(cfg)
	if err != nil {
		return err
	}

	_, err = s.kv.Put(ctx, serviceKey(cfg.Name), string(payload))
	return err
}

func (s *Services) List(ctx context.Context) ([]model.ServiceConfig, error) {
	resp, err := s.kv.Get(ctx, servicesPrefix, clientv3.WithPrefix())
	if err != nil {
		return nil, err
	}

	items := make([]model.ServiceConfig, 0, len(resp.Kvs))
	for _, kv := range resp.Kvs {
		var cfg model.ServiceConfig
		if err := json.Unmarshal(kv.Value, &cfg); err != nil {
			continue
		}
		items = append(items, cfg)
	}

	return items, nil
}

func serviceKey(name string) string {
	return servicesPrefix + strings.TrimSpace(name)
}

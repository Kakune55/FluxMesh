package registry

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"fluxmesh/internal/model"

	clientv3 "go.etcd.io/etcd/client/v3"
)

const servicesPrefix = "/mesh/services/"

var ErrServiceNotFound = errors.New("service not found")

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

func (s *Services) Get(ctx context.Context, name string) (model.ServiceConfig, error) {
	resp, err := s.kv.Get(ctx, serviceKey(name))
	if err != nil {
		return model.ServiceConfig{}, err
	}
	if len(resp.Kvs) == 0 {
		return model.ServiceConfig{}, ErrServiceNotFound
	}

	var cfg model.ServiceConfig
	if err := json.Unmarshal(resp.Kvs[0].Value, &cfg); err != nil {
		return model.ServiceConfig{}, err
	}
	return cfg, nil
}

func serviceKey(name string) string {
	return servicesPrefix + strings.TrimSpace(name)
}

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
var ErrServiceConflict = errors.New("service resource version conflict")

type Services struct {
	kv clientv3.KV
}

func NewServices(cli *clientv3.Client) *Services {
	return &Services{kv: cli}
}

func (s *Services) Put(ctx context.Context, cfg model.ServiceConfig) error {
	cfg.ResourceVersion = 0
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
		cfg.ResourceVersion = kv.ModRevision
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
	cfg.ResourceVersion = resp.Kvs[0].ModRevision
	return cfg, nil
}

func (s *Services) UpdateWithRevision(ctx context.Context, name string, cfg model.ServiceConfig, expectedRevision int64) (model.ServiceConfig, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return model.ServiceConfig{}, ErrServiceNotFound
	}

	cfg.Name = name
	cfg.ResourceVersion = 0
	payload, err := json.Marshal(cfg)
	if err != nil {
		return model.ServiceConfig{}, err
	}

	key := serviceKey(name)
	txnResp, err := s.kv.Txn(ctx).
		If(clientv3.Compare(clientv3.ModRevision(key), "=", expectedRevision)).
		Then(clientv3.OpPut(key, string(payload))).
		Commit()
	if err != nil {
		return model.ServiceConfig{}, err
	}
	if !txnResp.Succeeded {
		return model.ServiceConfig{}, ErrServiceConflict
	}

	return s.Get(ctx, name)
}

func (s *Services) Delete(ctx context.Context, name string) error {
	resp, err := s.kv.Delete(ctx, serviceKey(name))
	if err != nil {
		return err
	}
	if resp.Deleted == 0 {
		return ErrServiceNotFound
	}
	return nil
}

func serviceKey(name string) string {
	return servicesPrefix + strings.TrimSpace(name)
}

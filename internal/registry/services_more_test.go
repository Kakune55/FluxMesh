package registry_test

import (
	"errors"
	"testing"

	"fluxmesh/internal/model"
	"fluxmesh/internal/registry"
	"fluxmesh/internal/testutil/etcdtest"
)

func TestServicesListAndDeleteNotFound(t *testing.T) {
	emb := etcdtest.Start(t)
	svc := registry.NewServices(emb.Client)

	seed := []model.ServiceConfig{
		{
			Name:      "orders-svc",
			Namespace: "prod",
			Version:   "v1",
			TrafficPolicy: model.ServiceTrafficPolicy{
				Listener: model.ListenerPolicy{Port: 18080},
			},
			Routes: []model.ServiceRoute{{PathPrefix: "/", Destination: "orders-v1", Weight: 100}},
		},
		{
			Name:      "payment-svc",
			Namespace: "prod",
			Version:   "v1",
			TrafficPolicy: model.ServiceTrafficPolicy{
				Listener: model.ListenerPolicy{Port: 18081},
			},
			Routes: []model.ServiceRoute{{PathPrefix: "/", Destination: "payment-v1", Weight: 100}},
		},
	}

	for i := range seed {
		if err := svc.Put(t.Context(), seed[i]); err != nil {
			t.Fatalf("put %s failed: %v", seed[i].Name, err)
		}
	}

	items, err := svc.List(t.Context())
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 services, got %d", len(items))
	}

	if err := svc.Delete(t.Context(), "missing-svc"); !errors.Is(err, registry.ErrServiceNotFound) {
		t.Fatalf("expected ErrServiceNotFound, got %v", err)
	}
}

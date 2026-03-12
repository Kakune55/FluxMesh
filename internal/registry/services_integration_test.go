package registry_test

import (
	"context"
	"errors"
	"testing"

	"fluxmesh/internal/model"
	"fluxmesh/internal/registry"
	"fluxmesh/internal/testutil/etcdtest"
)

func TestServicesCASFlow(t *testing.T) {
	emb := etcdtest.Start(t)
	svc := registry.NewServices(emb.Client)
	ctx := context.Background()

	initial := model.ServiceConfig{
		Name:      "payment-svc",
		Namespace: "prod",
		Version:   "v1",
		Routes: []model.ServiceRoute{
			{PathPrefix: "/", Destination: "payment-v1", Weight: 100},
		},
	}

	if err := svc.Put(ctx, initial); err != nil {
		t.Fatalf("put failed: %v", err)
	}

	got, err := svc.Get(ctx, initial.Name)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if got.ResourceVersion <= 0 {
		t.Fatalf("expected positive resource version, got %d", got.ResourceVersion)
	}

	updatedCfg := got
	updatedCfg.Version = "v2"
	updatedCfg.Routes = []model.ServiceRoute{{PathPrefix: "/", Destination: "payment-v2", Weight: 100}}
	updated, err := svc.UpdateWithRevision(ctx, got.Name, updatedCfg, got.ResourceVersion)
	if err != nil {
		t.Fatalf("update with revision failed: %v", err)
	}
	if updated.Version != "v2" {
		t.Fatalf("expected version v2, got %s", updated.Version)
	}

	_, err = svc.UpdateWithRevision(ctx, got.Name, updatedCfg, got.ResourceVersion)
	if !errors.Is(err, registry.ErrServiceConflict) {
		t.Fatalf("expected conflict error, got %v", err)
	}

	if err := svc.Delete(ctx, got.Name); err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	_, err = svc.Get(ctx, got.Name)
	if !errors.Is(err, registry.ErrServiceNotFound) {
		t.Fatalf("expected not found after delete, got %v", err)
	}
}

/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	infrastructurev1alpha1 "github.com/tuunit/cluster-api-provider-stackit/api/v1alpha1"
	internalstackit "github.com/tuunit/cluster-api-provider-stackit/internal/stackit"

	iaas "github.com/stackitcloud/stackit-sdk-go/services/iaas/v2api"
)

var stackitNotFoundError = internalstackit.ErrNotFound

type stackitCreateNetworkInput = internalstackit.CreateNetworkInput
type stackitCreateServerInput = internalstackit.CreateServerInput

type fakeStackitFactory struct {
	client internalstackit.Client
	err    error
}

func (f fakeStackitFactory) NewClient() (internalstackit.Client, error) {
	return f.client, f.err
}

type fakeStackitClient struct {
	listNetworksByLabelFn func(ctx context.Context, projectID, region, key, value string) ([]iaas.Network, error)
	createNetworkFn       func(ctx context.Context, input stackitCreateNetworkInput) (*iaas.Network, error)
	deleteNetworkFn       func(ctx context.Context, projectID, region, networkID string) error
	getNetworkFn          func(ctx context.Context, projectID, region, networkID string) (*iaas.Network, error)
	getServerFn           func(ctx context.Context, projectID, region, serverID string) (*iaas.Server, error)
	listServersByLabelFn  func(ctx context.Context, projectID, region, key, value string) ([]iaas.Server, error)
	createServerFn        func(ctx context.Context, input stackitCreateServerInput) (*iaas.Server, error)
	deleteServerFn        func(ctx context.Context, projectID, region, serverID string) error
}

func (f *fakeStackitClient) GetNetwork(ctx context.Context, projectID, region, networkID string) (*iaas.Network, error) {
	if f.getNetworkFn == nil {
		return nil, nil
	}
	return f.getNetworkFn(ctx, projectID, region, networkID)
}

func (f *fakeStackitClient) ListNetworksByLabel(ctx context.Context, projectID, region, key, value string) ([]iaas.Network, error) {
	if f.listNetworksByLabelFn == nil {
		return nil, nil
	}
	return f.listNetworksByLabelFn(ctx, projectID, region, key, value)
}

func (f *fakeStackitClient) CreateNetwork(ctx context.Context, input stackitCreateNetworkInput) (*iaas.Network, error) {
	if f.createNetworkFn == nil {
		return nil, nil
	}
	return f.createNetworkFn(ctx, input)
}

func (f *fakeStackitClient) DeleteNetwork(ctx context.Context, projectID, region, networkID string) error {
	if f.deleteNetworkFn == nil {
		return nil
	}
	return f.deleteNetworkFn(ctx, projectID, region, networkID)
}

func (f *fakeStackitClient) GetServer(ctx context.Context, projectID, region, serverID string) (*iaas.Server, error) {
	return f.getServerFn(ctx, projectID, region, serverID)
}

func (f *fakeStackitClient) ListServersByLabel(ctx context.Context, projectID, region, key, value string) ([]iaas.Server, error) {
	if f.listServersByLabelFn == nil {
		return nil, nil
	}
	return f.listServersByLabelFn(ctx, projectID, region, key, value)
}

func (f *fakeStackitClient) CreateServer(ctx context.Context, input stackitCreateServerInput) (*iaas.Server, error) {
	return f.createServerFn(ctx, input)
}

func (f *fakeStackitClient) DeleteServer(ctx context.Context, projectID, region, serverID string) error {
	if f.deleteServerFn == nil {
		return nil
	}
	return f.deleteServerFn(ctx, projectID, region, serverID)
}

func newControllerTestClient(objects ...client.Object) client.Client {
	scheme := runtime.NewScheme()
	_ = clusterv1.AddToScheme(scheme)
	_ = infrastructurev1alpha1.AddToScheme(scheme)

	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(
			&infrastructurev1alpha1.StackitCluster{},
			&infrastructurev1alpha1.StackitMachine{},
		).
		WithObjects(objects...).
		Build()
}

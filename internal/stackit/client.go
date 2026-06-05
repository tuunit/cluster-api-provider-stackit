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

package stackit

import (
	"context"
	stderrors "errors"
	"fmt"
	"net/http"

	"github.com/stackitcloud/stackit-sdk-go/core/oapierror"
	iaas "github.com/stackitcloud/stackit-sdk-go/services/iaas/v2api"
)

const (
	// MachineUIDLabel is set on STACKIT servers created for StackitMachine objects.
	MachineUIDLabel = "capi-machine-uid"

	// ClusterUIDLabel is set on STACKIT networks created for StackitCluster objects.
	ClusterUIDLabel = "capi-cluster-uid"
)

var (
	// ErrNotFound indicates that the requested STACKIT resource does not exist.
	ErrNotFound = stderrors.New("stackit resource not found")
)

// CreateServerInput contains the information required to create a STACKIT server.
type CreateServerInput struct {
	ProjectID         string
	Region            string
	NetworkID         string
	Name              string
	MachineType       string
	AvailabilityZone  string
	ImageID           string
	BootVolumeSizeGiB int64
	MachineUID        string
}

// CreateNetworkInput contains the information required to create a STACKIT network.
type CreateNetworkInput struct {
	ProjectID    string
	Region       string
	Name         string
	PrefixLength int64
	ClusterUID   string
}

// Client is the STACKIT API surface used by the controllers.
type Client interface {
	GetNetwork(ctx context.Context, projectID, region, networkID string) (*iaas.Network, error)
	ListNetworksByLabel(ctx context.Context, projectID, region, key, value string) ([]iaas.Network, error)
	CreateNetwork(ctx context.Context, input CreateNetworkInput) (*iaas.Network, error)
	DeleteNetwork(ctx context.Context, projectID, region, networkID string) error
	GetServer(ctx context.Context, projectID, region, serverID string) (*iaas.Server, error)
	ListServersByLabel(ctx context.Context, projectID, region, key, value string) ([]iaas.Server, error)
	CreateServer(ctx context.Context, input CreateServerInput) (*iaas.Server, error)
	DeleteServer(ctx context.Context, projectID, region, serverID string) error
}

// ClientFactory creates STACKIT clients.
type ClientFactory interface {
	NewClient() (Client, error)
}

// SDKClientFactory creates real STACKIT SDK-backed clients.
type SDKClientFactory struct{}

// NewClient returns a real STACKIT SDK-backed client.
func (SDKClientFactory) NewClient() (Client, error) {
	apiClient, err := iaas.NewAPIClient()
	if err != nil {
		return nil, err
	}

	return &sdkClient{api: apiClient.DefaultAPI}, nil
}

type sdkClient struct {
	api iaas.DefaultAPI
}

// GetNetwork returns a STACKIT network by ID.
func (c *sdkClient) GetNetwork(ctx context.Context, projectID, region, networkID string) (*iaas.Network, error) {
	network, err := c.api.GetNetwork(ctx, projectID, region, networkID).Execute()
	return network, normalizeError(err)
}

// ListNetworksByLabel returns STACKIT networks matching a single label.
func (c *sdkClient) ListNetworksByLabel(ctx context.Context, projectID, region, key, value string) ([]iaas.Network, error) {
	response, err := c.api.ListNetworks(ctx, projectID, region).LabelSelector(fmt.Sprintf("%s=%s", key, value)).Execute()
	if err != nil {
		return nil, normalizeError(err)
	}
	if response == nil {
		return nil, nil
	}
	return response.Items, nil
}

// CreateNetwork creates a STACKIT network using a prefix-length IPv4 definition.
func (c *sdkClient) CreateNetwork(ctx context.Context, input CreateNetworkInput) (*iaas.Network, error) {
	prefixLength := input.PrefixLength
	payload := iaas.CreateNetworkPayload{
		Name: input.Name,
		Ipv4: &iaas.CreateNetworkIPv4{
			CreateNetworkIPv4WithPrefixLength: &iaas.CreateNetworkIPv4WithPrefixLength{
				PrefixLength: prefixLength,
			},
		},
		Labels: map[string]any{
			ClusterUIDLabel: input.ClusterUID,
		},
	}

	network, err := c.api.CreateNetwork(ctx, input.ProjectID, input.Region).CreateNetworkPayload(payload).Execute()
	return network, normalizeError(err)
}

// DeleteNetwork deletes a STACKIT network by ID.
func (c *sdkClient) DeleteNetwork(ctx context.Context, projectID, region, networkID string) error {
	return normalizeError(c.api.DeleteNetwork(ctx, projectID, region, networkID).Execute())
}

// GetServer returns a STACKIT server by ID with NIC details.
func (c *sdkClient) GetServer(ctx context.Context, projectID, region, serverID string) (*iaas.Server, error) {
	server, err := c.api.GetServer(ctx, projectID, region, serverID).Details(true).Execute()
	return server, normalizeError(err)
}

// ListServersByLabel returns STACKIT servers matching a single label.
func (c *sdkClient) ListServersByLabel(ctx context.Context, projectID, region, key, value string) ([]iaas.Server, error) {
	response, err := c.api.ListServers(ctx, projectID, region).Details(true).LabelSelector(fmt.Sprintf("%s=%s", key, value)).Execute()
	if err != nil {
		return nil, normalizeError(err)
	}
	if response == nil {
		return nil, nil
	}
	return response.Items, nil
}

// CreateServer creates a STACKIT server using an image-backed boot volume and a single network.
func (c *sdkClient) CreateServer(ctx context.Context, input CreateServerInput) (*iaas.Server, error) {
	networkID := input.NetworkID
	availabilityZone := input.AvailabilityZone
	bootVolumeSizeGiB := input.BootVolumeSizeGiB

	payload := iaas.CreateServerPayload{
		Name:        input.Name,
		MachineType: input.MachineType,
		BootVolume: &iaas.BootVolume{
			Size: &bootVolumeSizeGiB,
			Source: &iaas.BootVolumeSource{
				Id:   input.ImageID,
				Type: "image",
			},
		},
		Labels: map[string]any{
			MachineUIDLabel: input.MachineUID,
		},
		Networking: iaas.CreateServerPayloadAllOfNetworking{
			CreateServerNetworking: &iaas.CreateServerNetworking{
				NetworkId: &networkID,
			},
		},
	}

	if availabilityZone != "" {
		payload.AvailabilityZone = &availabilityZone
	}

	server, err := c.api.CreateServer(ctx, input.ProjectID, input.Region).CreateServerPayload(payload).Execute()
	return server, normalizeError(err)
}

// DeleteServer deletes a STACKIT server by ID.
func (c *sdkClient) DeleteServer(ctx context.Context, projectID, region, serverID string) error {
	return normalizeError(c.api.DeleteServer(ctx, projectID, region, serverID).Execute())
}

func normalizeError(err error) error {
	if err == nil {
		return nil
	}

	var openAPIError *oapierror.GenericOpenAPIError
	if stderrors.As(err, &openAPIError) && openAPIError.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}

	return err
}

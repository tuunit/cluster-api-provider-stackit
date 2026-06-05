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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

// StackitClusterSpec defines the desired state of StackitCluster.
type StackitClusterSpec struct {
	// projectID identifies the STACKIT project that owns the cluster infrastructure.
	// +kubebuilder:validation:MinLength=1
	ProjectID string `json:"projectID"`

	// region identifies the STACKIT region in which cluster infrastructure should be reconciled.
	// +kubebuilder:validation:MinLength=1
	Region string `json:"region"`

	// networkID references an existing STACKIT network that should host the cluster.
	// If omitted, the provider creates and manages a dedicated STACKIT network for the cluster.
	// +optional
	// +kubebuilder:validation:MinLength=1
	NetworkID string `json:"networkID,omitempty"`

	// controlPlaneEndpoint represents the endpoint used to communicate with the control plane.
	// +optional
	ControlPlaneEndpoint clusterv1.APIEndpoint `json:"controlPlaneEndpoint,omitempty,omitzero"`
}

// StackitClusterInitializationStatus provides observations of the StackitCluster initialization process.
// +kubebuilder:validation:MinProperties=1
type StackitClusterInitializationStatus struct {
	// provisioned is true when the cluster infrastructure is fully provisioned.
	// +optional
	Provisioned *bool `json:"provisioned,omitempty"`
}

// StackitClusterStatus defines the observed state of StackitCluster.
type StackitClusterStatus struct {
	// networkID is the STACKIT network currently used by the cluster.
	// +optional
	NetworkID string `json:"networkID,omitempty"`

	// managedNetwork indicates if the current network was created and is managed by this provider.
	// +optional
	ManagedNetwork bool `json:"managedNetwork,omitempty"`

	// initialization provides observations of the StackitCluster initialization process.
	// +optional
	Initialization StackitClusterInitializationStatus `json:"initialization,omitempty,omitzero"`

	// conditions represent the current state of the StackitCluster resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=stackitclusters,scope=Namespaced,categories=cluster-api
// +kubebuilder:storageversion
// +kubebuilder:subresource:status

// StackitCluster is the Schema for the stackitclusters API.
type StackitCluster struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of StackitCluster
	// +required
	Spec StackitClusterSpec `json:"spec"`

	// status defines the observed state of StackitCluster
	// +optional
	Status StackitClusterStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// StackitClusterList contains a list of StackitCluster.
type StackitClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []StackitCluster `json:"items"`
}

// GetConditions returns the list of conditions for a StackitCluster.
func (s *StackitCluster) GetConditions() []metav1.Condition {
	return s.Status.Conditions
}

// SetConditions sets the conditions for a StackitCluster.
func (s *StackitCluster) SetConditions(conditions []metav1.Condition) {
	s.Status.Conditions = conditions
}

func init() {
	SchemeBuilder.Register(&StackitCluster{}, &StackitClusterList{})
}

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

// StackitMachineSpec defines the desired state of StackitMachine.
type StackitMachineSpec struct {
	// providerID must match the provider ID as seen on the corresponding Kubernetes Node object.
	// +optional
	// +kubebuilder:validation:MinLength=1
	ProviderID string `json:"providerID,omitempty"`

	// instanceID references the STACKIT instance backing this machine when one already exists.
	// +optional
	// +kubebuilder:validation:MinLength=1
	InstanceID string `json:"instanceID,omitempty"`

	// imageID identifies the STACKIT image used as the boot volume source for the machine.
	// +kubebuilder:validation:MinLength=1
	ImageID string `json:"imageID"`

	// machineType identifies the STACKIT instance flavor to use for the machine.
	// +kubebuilder:validation:MinLength=1
	MachineType string `json:"machineType"`

	// availabilityZone identifies the STACKIT availability zone for the machine.
	// +kubebuilder:validation:MinLength=1
	AvailabilityZone string `json:"availabilityZone"`

	// bootVolumeSizeGiB is the size of the machine boot volume in GiB.
	// +kubebuilder:validation:Minimum=1
	BootVolumeSizeGiB int64 `json:"bootVolumeSizeGiB"`
}

// StackitMachineInitializationStatus provides observations of the StackitMachine initialization process.
// +kubebuilder:validation:MinProperties=1
type StackitMachineInitializationStatus struct {
	// provisioned is true when the machine infrastructure is fully provisioned.
	// +optional
	Provisioned *bool `json:"provisioned,omitempty"`
}

// StackitMachineStatus defines the observed state of StackitMachine.
type StackitMachineStatus struct {
	// projectID is the STACKIT project used for the last successful reconciliation.
	// +optional
	ProjectID string `json:"projectID,omitempty"`

	// region is the STACKIT region used for the last successful reconciliation.
	// +optional
	Region string `json:"region,omitempty"`

	// instanceID is the STACKIT server ID managed for this machine.
	// +optional
	InstanceID string `json:"instanceID,omitempty"`

	// initialization provides observations of the StackitMachine initialization process.
	// +optional
	Initialization StackitMachineInitializationStatus `json:"initialization,omitempty,omitzero"`

	// failureDomain is the failure domain where this machine has been placed.
	// +optional
	FailureDomain string `json:"failureDomain,omitempty"`

	// addresses contains the associated addresses for the machine.
	// +optional
	Addresses []clusterv1.MachineAddress `json:"addresses,omitempty"`

	// conditions represent the current state of the StackitMachine resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=stackitmachines,scope=Namespaced,categories=cluster-api
// +kubebuilder:storageversion
// +kubebuilder:subresource:status

// StackitMachine is the Schema for the stackitmachines API.
type StackitMachine struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of StackitMachine
	// +required
	Spec StackitMachineSpec `json:"spec"`

	// status defines the observed state of StackitMachine
	// +optional
	Status StackitMachineStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// StackitMachineList contains a list of StackitMachine.
type StackitMachineList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []StackitMachine `json:"items"`
}

// GetConditions returns the list of conditions for a StackitMachine.
func (s *StackitMachine) GetConditions() []metav1.Condition {
	return s.Status.Conditions
}

// SetConditions sets the conditions for a StackitMachine.
func (s *StackitMachine) SetConditions(conditions []metav1.Condition) {
	s.Status.Conditions = conditions
}

func init() {
	SchemeBuilder.Register(&StackitMachine{}, &StackitMachineList{})
}

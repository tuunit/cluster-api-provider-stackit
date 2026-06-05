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

// StackitMachineTemplateSpec defines the desired state of StackitMachineTemplate.
type StackitMachineTemplateSpec struct {
	Template StackitMachineTemplateResource `json:"template"`
}

// StackitMachineTemplateResource describes the data needed to create a StackitMachine from a template.
type StackitMachineTemplateResource struct {
	// metadata is the standard object's metadata.
	// +optional
	ObjectMeta clusterv1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of the StackitMachine created from this template.
	Spec StackitMachineSpec `json:"spec"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=stackitmachinetemplates,scope=Namespaced,categories=cluster-api
// +kubebuilder:storageversion

// StackitMachineTemplate is the Schema for the stackitmachinetemplates API.
type StackitMachineTemplate struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of StackitMachineTemplate
	// +required
	Spec StackitMachineTemplateSpec `json:"spec"`
}

// +kubebuilder:object:root=true

// StackitMachineTemplateList contains a list of StackitMachineTemplate.
type StackitMachineTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []StackitMachineTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&StackitMachineTemplate{}, &StackitMachineTemplateList{})
}

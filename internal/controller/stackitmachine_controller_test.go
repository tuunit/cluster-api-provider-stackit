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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	infrastructurev1alpha1 "github.com/tuunit/cluster-api-provider-stackit/api/v1alpha1"

	iaas "github.com/stackitcloud/stackit-sdk-go/services/iaas/v2api"
)

var _ = Describe("StackitMachine Controller", func() {
	Context("When reconciling a resource", func() {
		ctx := context.Background()
		namespace := "default"

		newOwnerCluster := func(name string, infraName string) *clusterv1.Cluster {
			return &clusterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
				},
				Spec: clusterv1.ClusterSpec{
					InfrastructureRef: clusterv1.ContractVersionedObjectReference{
						APIGroup: infrastructurev1alpha1.GroupVersion.Group,
						Kind:     "StackitCluster",
						Name:     infraName,
					},
				},
			}
		}

		newOwnerMachine := func(name string, clusterName string) *clusterv1.Machine {
			return &clusterv1.Machine{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
				},
				Spec: clusterv1.MachineSpec{
					ClusterName: clusterName,
				},
			}
		}

		newStackitCluster := func(name string) *infrastructurev1alpha1.StackitCluster {
			return &infrastructurev1alpha1.StackitCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
				},
				Spec: infrastructurev1alpha1.StackitClusterSpec{
					ProjectID: "00000000-0000-0000-0000-000000000001",
					Region:    "eu01",
				},
				Status: infrastructurev1alpha1.StackitClusterStatus{
					NetworkID: "00000000-0000-0000-0000-000000000002",
				},
			}
		}

		newStackitMachine := func(name string, owner *clusterv1.Machine) *infrastructurev1alpha1.StackitMachine {
			stackitMachine := &infrastructurev1alpha1.StackitMachine{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
					UID:       types.UID("machine-uid-00000001"),
				},
				Spec: infrastructurev1alpha1.StackitMachineSpec{
					ImageID:           "00000000-0000-0000-0000-000000000010",
					MachineType:       "g1.1",
					AvailabilityZone:  "eu01-1",
					BootVolumeSizeGiB: 64,
				},
			}
			stackitMachine.OwnerReferences = []metav1.OwnerReference{
				*metav1.NewControllerRef(owner, clusterv1.GroupVersion.WithKind("Machine")),
			}
			return stackitMachine
		}

		It("creates a STACKIT server and requeues while it is provisioning", func() {
			stackitCluster := newStackitCluster("stackit-cluster")
			ownerCluster := newOwnerCluster("owner-cluster", stackitCluster.Name)
			ownerMachine := newOwnerMachine("owner-machine", ownerCluster.Name)
			stackitMachine := newStackitMachine("stackit-machine", ownerMachine)

			fakeStackitClient := &fakeStackitClient{
				listServersByLabelFn: func(_ context.Context, _, _, _, _ string) ([]iaas.Server, error) {
					return nil, nil
				},
				createServerFn: func(_ context.Context, input stackitCreateServerInput) (*iaas.Server, error) {
					Expect(input.NetworkID).To(Equal(stackitCluster.Status.NetworkID))
					Expect(input.ImageID).To(Equal(stackitMachine.Spec.ImageID))
					Expect(input.BootVolumeSizeGiB).To(Equal(stackitMachine.Spec.BootVolumeSizeGiB))

					serverID := "00000000-0000-0000-0000-000000000099"
					status := "CREATING"
					return &iaas.Server{
						Id:               &serverID,
						Status:           &status,
						AvailabilityZone: &stackitMachine.Spec.AvailabilityZone,
					}, nil
				},
			}

			k8sClient := newControllerTestClient(ownerCluster, ownerMachine, stackitCluster, stackitMachine)
			controllerReconciler := &StackitMachineReconciler{
				Client:         k8sClient,
				Scheme:         k8sClient.Scheme(),
				StackitFactory: fakeStackitFactory{client: fakeStackitClient},
			}

			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: client.ObjectKeyFromObject(stackitMachine),
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(15 * time.Second))

			reconciled := &infrastructurev1alpha1.StackitMachine{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(stackitMachine), reconciled)).To(Succeed())
			Expect(reconciled.Status.InstanceID).To(Equal("00000000-0000-0000-0000-000000000099"))
			Expect(reconciled.Spec.ProviderID).To(Equal(stackitProviderID(reconciled.Status.InstanceID)))
			Expect(reconciled.Finalizers).To(ContainElement(stackitMachineFinalizer))

			readyCondition := meta.FindStatusCondition(reconciled.Status.Conditions, clusterv1.ReadyCondition)
			Expect(readyCondition).NotTo(BeNil())
			Expect(readyCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCondition.Reason).To(Equal(reasonServerProvisioning))
		})

		It("marks the machine ready from an existing STACKIT server", func() {
			stackitCluster := newStackitCluster("stackit-cluster")
			ownerCluster := newOwnerCluster("owner-cluster", stackitCluster.Name)
			ownerMachine := newOwnerMachine("owner-machine", ownerCluster.Name)
			stackitMachine := newStackitMachine("stackit-machine", ownerMachine)
			stackitMachine.Status.InstanceID = "00000000-0000-0000-0000-000000000099"

			serverID := stackitMachine.Status.InstanceID
			serverStatus := "ACTIVE"
			internalIP := "10.0.0.15"
			publicIP := "203.0.113.10"

			fakeStackitClient := &fakeStackitClient{
				getServerFn: func(_ context.Context, _, _, gotServerID string) (*iaas.Server, error) {
					Expect(gotServerID).To(Equal(serverID))
					return &iaas.Server{
						Id:               &serverID,
						Name:             "stackit-machine",
						Status:           &serverStatus,
						AvailabilityZone: &stackitMachine.Spec.AvailabilityZone,
						Nics: []iaas.ServerNetwork{
							{
								Mac:         "02:00:00:00:00:01",
								NetworkId:   stackitCluster.Status.NetworkID,
								NetworkName: "network",
								NicId:       "00000000-0000-0000-0000-000000000055",
								NicSecurity: true,
								Ipv4:        &internalIP,
								PublicIp:    &publicIP,
							},
						},
					}, nil
				},
			}

			k8sClient := newControllerTestClient(ownerCluster, ownerMachine, stackitCluster, stackitMachine)
			controllerReconciler := &StackitMachineReconciler{
				Client:         k8sClient,
				Scheme:         k8sClient.Scheme(),
				StackitFactory: fakeStackitFactory{client: fakeStackitClient},
			}

			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: stackitMachine.Name, Namespace: stackitMachine.Namespace},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeZero())

			reconciled := &infrastructurev1alpha1.StackitMachine{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(stackitMachine), reconciled)).To(Succeed())
			Expect(reconciled.Spec.ProviderID).To(Equal(stackitProviderID(serverID)))
			Expect(reconciled.Status.FailureDomain).To(Equal(stackitMachine.Spec.AvailabilityZone))
			Expect(reconciled.Status.Addresses).To(ContainElements(
				clusterv1.MachineAddress{Type: clusterv1.MachineHostName, Address: ownerMachine.Name},
				clusterv1.MachineAddress{Type: clusterv1.MachineInternalIP, Address: internalIP},
				clusterv1.MachineAddress{Type: clusterv1.MachineExternalIP, Address: publicIP},
			))

			readyCondition := meta.FindStatusCondition(reconciled.Status.Conditions, clusterv1.ReadyCondition)
			Expect(readyCondition).NotTo(BeNil())
			Expect(readyCondition.Status).To(Equal(metav1.ConditionTrue))
			Expect(readyCondition.Reason).To(Equal(reasonServerReady))
		})
	})
})

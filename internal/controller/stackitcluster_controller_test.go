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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	infrastructurev1alpha1 "github.com/tuunit/cluster-api-provider-stackit/api/v1alpha1"

	iaas "github.com/stackitcloud/stackit-sdk-go/services/iaas/v2api"
)

var _ = Describe("StackitCluster Controller", func() {
	Context("When reconciling a resource", func() {
		ctx := context.Background()
		namespace := "default"

		newOwnerCluster := func(name string) *clusterv1.Cluster {
			return &clusterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
				},
			}
		}

		newStackitCluster := func(name string, owner *clusterv1.Cluster) *infrastructurev1alpha1.StackitCluster {
			stackitCluster := &infrastructurev1alpha1.StackitCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
					UID:       types.UID(name + "-uid"),
				},
				Spec: infrastructurev1alpha1.StackitClusterSpec{
					ProjectID: "00000000-0000-0000-0000-000000000001",
					Region:    "eu01",
					NetworkID: "00000000-0000-0000-0000-000000000002",
					ControlPlaneEndpoint: clusterv1.APIEndpoint{
						Host: "api.example.invalid",
						Port: 6443,
					},
				},
			}
			stackitCluster.OwnerReferences = []metav1.OwnerReference{
				*metav1.NewControllerRef(owner, clusterv1.GroupVersion.WithKind("Cluster")),
			}
			return stackitCluster
		}

		It("marks the cluster ready when the STACKIT network is ready and the endpoint exists", func() {
			ownerCluster := newOwnerCluster("owner-cluster")
			stackitCluster := newStackitCluster("ready-cluster", ownerCluster)

			fakeStackitClient := &fakeStackitClient{
				getNetworkFn: func(_ context.Context, _, _, _ string) (*iaas.Network, error) {
					return &iaas.Network{
						Id:     stackitCluster.Spec.NetworkID,
						Name:   "cluster-network",
						Status: "CREATED",
					}, nil
				},
			}

			k8sClient := newControllerTestClient(ownerCluster, stackitCluster)
			controllerReconciler := &StackitClusterReconciler{
				Client:         k8sClient,
				Scheme:         k8sClient.Scheme(),
				StackitFactory: fakeStackitFactory{client: fakeStackitClient},
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: client.ObjectKeyFromObject(stackitCluster),
			})
			Expect(err).NotTo(HaveOccurred())

			reconciled := &infrastructurev1alpha1.StackitCluster{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(stackitCluster), reconciled)).To(Succeed())
			Expect(ptr.Deref(reconciled.Status.Initialization.Provisioned, false)).To(BeTrue())
			Expect(reconciled.Status.NetworkID).To(Equal(stackitCluster.Spec.NetworkID))
			Expect(reconciled.Status.ManagedNetwork).To(BeFalse())

			readyCondition := meta.FindStatusCondition(reconciled.Status.Conditions, clusterv1.ReadyCondition)
			Expect(readyCondition).NotTo(BeNil())
			Expect(readyCondition.Status).To(Equal(metav1.ConditionTrue))
			Expect(readyCondition.Reason).To(Equal(reasonControlPlaneEndpointConfigured))
		})

		It("creates and records a managed STACKIT network when spec.networkID is empty", func() {
			ownerCluster := newOwnerCluster("owner-cluster")
			stackitCluster := newStackitCluster("managed-network-cluster", ownerCluster)
			stackitCluster.Spec.NetworkID = ""

			createdNetworkID := "00000000-0000-0000-0000-000000000123"
			fakeStackitClient := &fakeStackitClient{
				listNetworksByLabelFn: func(_ context.Context, _, _, key, value string) ([]iaas.Network, error) {
					Expect(key).To(Equal("capi-cluster-uid"))
					Expect(value).To(Equal(string(stackitCluster.UID)))
					return nil, nil
				},
				createNetworkFn: func(_ context.Context, input stackitCreateNetworkInput) (*iaas.Network, error) {
					Expect(input.Name).To(Equal(stackitCluster.Name + "-network"))
					Expect(input.PrefixLength).To(Equal(int64(24)))
					Expect(input.ClusterUID).To(Equal(string(stackitCluster.UID)))

					return &iaas.Network{
						Id:     createdNetworkID,
						Name:   input.Name,
						Status: "CREATED",
					}, nil
				},
			}

			k8sClient := newControllerTestClient(ownerCluster, stackitCluster)
			controllerReconciler := &StackitClusterReconciler{
				Client:         k8sClient,
				Scheme:         k8sClient.Scheme(),
				StackitFactory: fakeStackitFactory{client: fakeStackitClient},
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: client.ObjectKeyFromObject(stackitCluster),
			})
			Expect(err).NotTo(HaveOccurred())

			reconciled := &infrastructurev1alpha1.StackitCluster{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(stackitCluster), reconciled)).To(Succeed())
			Expect(reconciled.Status.NetworkID).To(Equal(createdNetworkID))
			Expect(reconciled.Status.ManagedNetwork).To(BeTrue())
			Expect(reconciled.Finalizers).To(ContainElement(stackitClusterFinalizer))

			readyCondition := meta.FindStatusCondition(reconciled.Status.Conditions, clusterv1.ReadyCondition)
			Expect(readyCondition).NotTo(BeNil())
			Expect(readyCondition.Status).To(Equal(metav1.ConditionTrue))
			Expect(readyCondition.Reason).To(Equal(reasonControlPlaneEndpointConfigured))
		})

		It("marks the cluster not ready when the STACKIT network is missing", func() {
			ownerCluster := newOwnerCluster("owner-cluster")
			stackitCluster := newStackitCluster("missing-network-cluster", ownerCluster)

			fakeStackitClient := &fakeStackitClient{
				getNetworkFn: func(_ context.Context, _, _, _ string) (*iaas.Network, error) {
					return nil, stackitNotFoundError
				},
			}

			k8sClient := newControllerTestClient(ownerCluster, stackitCluster)
			controllerReconciler := &StackitClusterReconciler{
				Client:         k8sClient,
				Scheme:         k8sClient.Scheme(),
				StackitFactory: fakeStackitFactory{client: fakeStackitClient},
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: stackitCluster.Name, Namespace: stackitCluster.Namespace},
			})
			Expect(err).NotTo(HaveOccurred())

			reconciled := &infrastructurev1alpha1.StackitCluster{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(stackitCluster), reconciled)).To(Succeed())
			Expect(ptr.Deref(reconciled.Status.Initialization.Provisioned, true)).To(BeFalse())

			readyCondition := meta.FindStatusCondition(reconciled.Status.Conditions, clusterv1.ReadyCondition)
			Expect(readyCondition).NotTo(BeNil())
			Expect(readyCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCondition.Reason).To(Equal(reasonNetworkNotFound))
		})
	})
})

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
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/utils/ptr"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/annotations"
	"sigs.k8s.io/cluster-api/util/patch"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"

	infrastructurev1alpha1 "github.com/tuunit/cluster-api-provider-stackit/api/v1alpha1"
	"github.com/tuunit/cluster-api-provider-stackit/internal/stackit"

	iaas "github.com/stackitcloud/stackit-sdk-go/services/iaas/v2api"
)

// StackitClusterReconciler reconciles a StackitCluster object
type StackitClusterReconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	StackitFactory stackit.ClientFactory
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=stackitclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=stackitclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=stackitclusters/finalizers,verbs=update

// Reconcile ensures the StackitCluster status reflects the infrastructure state observed in STACKIT.
func (r *StackitClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	log := ctrl.LoggerFrom(ctx)

	stackitCluster := &infrastructurev1alpha1.StackitCluster{}
	if err := r.Get(ctx, req.NamespacedName, stackitCluster); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if annotations.IsExternallyManaged(stackitCluster) {
		log.V(4).Info("Skipping externally managed StackitCluster", "stackitCluster", req.NamespacedName)
		return ctrl.Result{}, nil
	}

	patchHelper, err := patch.NewHelper(stackitCluster, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}

	defer func() {
		if err := patchHelper.Patch(ctx, stackitCluster, patch.WithOwnedConditions{Conditions: []string{
			clusterv1.ReadyCondition,
			clusterv1.PausedCondition,
		}}); err != nil {
			reterr = errors.NewAggregate([]error{reterr, err})
		}
	}()

	cluster, err := util.GetOwnerCluster(ctx, r.Client, stackitCluster.ObjectMeta)
	if err != nil {
		return ctrl.Result{}, err
	}
	if cluster == nil {
		log.V(4).Info("Waiting for owning Cluster reference", "stackitCluster", req.NamespacedName)
		return ctrl.Result{}, nil
	}

	clusterPaused := ptr.Deref(cluster.Spec.Paused, false)
	objectPaused := annotations.HasPaused(stackitCluster)
	setPausedCondition(stackitCluster, clusterPaused, objectPaused)
	if !stackitCluster.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, stackitCluster)
	}

	stackitClient, err := r.stackitClient()
	if err != nil {
		setReadyCondition(
			stackitCluster,
			metav1.ConditionFalse,
			reasonStackitAPIError,
			fmt.Sprintf("Failed to initialize STACKIT client: %v", err),
		)
		return ctrl.Result{}, err
	}

	if clusterPaused || objectPaused {
		return ctrl.Result{}, nil
	}

	controllerutil.AddFinalizer(stackitCluster, stackitClusterFinalizer)
	stackitCluster.Status.Initialization.Provisioned = ptr.To(false)

	network, networkID, managedNetwork, err := r.reconcileNetwork(ctx, stackitClient, stackitCluster)
	switch err {
	case nil:
	case stackit.ErrNotFound:
		setReadyCondition(
			stackitCluster,
			metav1.ConditionFalse,
			reasonNetworkNotFound,
			fmt.Sprintf("STACKIT network %q was not found", firstNonEmpty(stackitCluster.Spec.NetworkID, stackitCluster.Status.NetworkID)),
		)
		return ctrl.Result{}, nil
	default:
		setReadyCondition(
			stackitCluster,
			metav1.ConditionFalse,
			reasonStackitAPIError,
			fmt.Sprintf("Failed to reconcile STACKIT network: %v", err),
		)
		return ctrl.Result{}, err
	}

	stackitCluster.Status.NetworkID = networkID
	stackitCluster.Status.ManagedNetwork = managedNetwork

	if !networkIsReady(network) {
		reason := reasonNetworkNotReady
		message := fmt.Sprintf("STACKIT network %q is in status %q", networkID, network.GetStatus())
		if managedNetwork {
			reason = reasonCreatingManagedNetwork
			message = fmt.Sprintf("Provider-managed STACKIT network %q is in status %q", networkID, network.GetStatus())
		}
		setReadyCondition(
			stackitCluster,
			metav1.ConditionFalse,
			reason,
			message,
		)
		return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
	}

	provisioned := stackitCluster.Spec.ControlPlaneEndpoint.Host != "" && stackitCluster.Spec.ControlPlaneEndpoint.Port != 0
	stackitCluster.Status.Initialization.Provisioned = ptr.To(provisioned)
	if provisioned {
		setReadyCondition(
			stackitCluster,
			metav1.ConditionTrue,
			reasonControlPlaneEndpointConfigured,
			fmt.Sprintf("STACKIT network %q is ready and spec.controlPlaneEndpoint is configured", networkID),
		)
		return ctrl.Result{}, nil
	}

	setReadyCondition(
		stackitCluster,
		metav1.ConditionFalse,
		reasonWaitingForControlPlaneEndpoint,
		fmt.Sprintf("STACKIT network %q is ready, waiting for spec.controlPlaneEndpoint to be populated", networkID),
	)
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *StackitClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	stackitClusterGVK := infrastructurev1alpha1.GroupVersion.WithKind("StackitCluster")

	return ctrl.NewControllerManagedBy(mgr).
		For(&infrastructurev1alpha1.StackitCluster{}).
		Watches(
			&clusterv1.Cluster{},
			handler.EnqueueRequestsFromMapFunc(
				util.ClusterToInfrastructureMapFunc(context.Background(), stackitClusterGVK, mgr.GetClient(), &infrastructurev1alpha1.StackitCluster{}),
			),
		).
		Named("stackitcluster").
		Complete(r)
}

func (r *StackitClusterReconciler) stackitClient() (stackit.Client, error) {
	factory := r.StackitFactory
	if factory == nil {
		factory = stackit.SDKClientFactory{}
	}
	return factory.NewClient()
}

func (r *StackitClusterReconciler) reconcileNetwork(ctx context.Context, stackitClient stackit.Client, stackitCluster *infrastructurev1alpha1.StackitCluster) (*iaas.Network, string, bool, error) {
	if stackitCluster.Spec.NetworkID != "" {
		network, err := stackitClient.GetNetwork(ctx, stackitCluster.Spec.ProjectID, stackitCluster.Spec.Region, stackitCluster.Spec.NetworkID)
		if err != nil {
			return nil, "", false, err
		}
		return network, stackitCluster.Spec.NetworkID, false, nil
	}

	if stackitCluster.Status.NetworkID != "" && stackitCluster.Status.ManagedNetwork {
		network, err := stackitClient.GetNetwork(ctx, stackitCluster.Spec.ProjectID, stackitCluster.Spec.Region, stackitCluster.Status.NetworkID)
		if err == nil {
			return network, stackitCluster.Status.NetworkID, true, nil
		}
		if err != stackit.ErrNotFound {
			return nil, "", false, err
		}
	}

	networks, err := stackitClient.ListNetworksByLabel(ctx, stackitCluster.Spec.ProjectID, stackitCluster.Spec.Region, stackit.ClusterUIDLabel, string(stackitCluster.UID))
	if err != nil {
		return nil, "", false, err
	}
	if len(networks) > 0 {
		network, err := stackitClient.GetNetwork(ctx, stackitCluster.Spec.ProjectID, stackitCluster.Spec.Region, networks[0].GetId())
		if err != nil {
			return nil, "", false, err
		}
		return network, networks[0].GetId(), true, nil
	}

	network, err := stackitClient.CreateNetwork(ctx, stackit.CreateNetworkInput{
		ProjectID:    stackitCluster.Spec.ProjectID,
		Region:       stackitCluster.Spec.Region,
		Name:         stackitClusterManagedNetworkName(stackitCluster),
		PrefixLength: 24,
		ClusterUID:   string(stackitCluster.UID),
	})
	if err != nil {
		return nil, "", false, err
	}
	return network, network.GetId(), true, nil
}

func (r *StackitClusterReconciler) reconcileDelete(ctx context.Context, stackitCluster *infrastructurev1alpha1.StackitCluster) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(stackitCluster, stackitClusterFinalizer) {
		return ctrl.Result{}, nil
	}

	if !stackitCluster.Status.ManagedNetwork || stackitCluster.Status.NetworkID == "" {
		controllerutil.RemoveFinalizer(stackitCluster, stackitClusterFinalizer)
		return ctrl.Result{}, nil
	}

	stackitClient, err := r.stackitClient()
	if err != nil {
		return ctrl.Result{}, err
	}

	network, err := stackitClient.GetNetwork(ctx, stackitCluster.Spec.ProjectID, stackitCluster.Spec.Region, stackitCluster.Status.NetworkID)
	switch err {
	case nil:
	case stackit.ErrNotFound:
		controllerutil.RemoveFinalizer(stackitCluster, stackitClusterFinalizer)
		return ctrl.Result{}, nil
	default:
		return ctrl.Result{}, err
	}

	if !strings.EqualFold(network.GetStatus(), "DELETING") && !strings.EqualFold(network.GetStatus(), "DELETED") {
		if err := stackitClient.DeleteNetwork(ctx, stackitCluster.Spec.ProjectID, stackitCluster.Spec.Region, stackitCluster.Status.NetworkID); err != nil && err != stackit.ErrNotFound {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
}

func stackitClusterManagedNetworkName(stackitCluster *infrastructurev1alpha1.StackitCluster) string {
	return fmt.Sprintf("%s-network", stackitCluster.Name)
}

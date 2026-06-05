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
	"slices"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
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

// StackitMachineReconciler reconciles a StackitMachine object
type StackitMachineReconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	StackitFactory stackit.ClientFactory
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=stackitmachines,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=stackitmachines/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=stackitmachines/finalizers,verbs=update
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines;clusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=stackitclusters,verbs=get;list;watch

// Reconcile ensures the StackitMachine status reflects the server state observed in STACKIT.
func (r *StackitMachineReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	log := ctrl.LoggerFrom(ctx)

	stackitMachine := &infrastructurev1alpha1.StackitMachine{}
	if err := r.Get(ctx, req.NamespacedName, stackitMachine); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if annotations.IsExternallyManaged(stackitMachine) {
		log.V(4).Info("Skipping externally managed StackitMachine", "stackitMachine", req.NamespacedName)
		return ctrl.Result{}, nil
	}

	patchHelper, err := patch.NewHelper(stackitMachine, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}

	defer func() {
		if err := patchHelper.Patch(ctx, stackitMachine, patch.WithOwnedConditions{Conditions: []string{
			clusterv1.ReadyCondition,
			clusterv1.PausedCondition,
		}}); err != nil {
			reterr = errors.NewAggregate([]error{reterr, err})
		}
	}()

	machine, err := util.GetOwnerMachine(ctx, r.Client, stackitMachine.ObjectMeta)
	if err != nil {
		return ctrl.Result{}, err
	}
	if machine == nil {
		log.V(4).Info("Waiting for owning Machine reference", "stackitMachine", req.NamespacedName)
		return ctrl.Result{}, nil
	}

	var cluster *clusterv1.Cluster
	if machine.Spec.ClusterName != "" {
		cluster, err = util.GetClusterByName(ctx, r.Client, machine.Namespace, machine.Spec.ClusterName)
		if err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
	}

	clusterPaused := cluster != nil && ptr.Deref(cluster.Spec.Paused, false)
	objectPaused := annotations.HasPaused(stackitMachine)
	setPausedCondition(stackitMachine, clusterPaused, objectPaused)
	if clusterPaused || objectPaused {
		return ctrl.Result{}, nil
	}

	if !stackitMachine.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, stackitMachine)
	}

	controllerutil.AddFinalizer(stackitMachine, stackitMachineFinalizer)

	stackitCluster, err := r.getStackitCluster(ctx, cluster)
	if err != nil {
		if apierrors.IsNotFound(err) {
			stackitMachine.Status.Initialization.Provisioned = ptr.To(false)
			setReadyCondition(
				stackitMachine,
				metav1.ConditionFalse,
				reasonWaitingForClusterInfrastructure,
				"Waiting for the referenced StackitCluster to exist",
			)
			return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
		}
		return ctrl.Result{}, err
	}

	stackitMachine.Status.ProjectID = stackitCluster.Spec.ProjectID
	stackitMachine.Status.Region = stackitCluster.Spec.Region
	stackitMachine.Status.Initialization.Provisioned = ptr.To(false)

	networkID := firstNonEmpty(stackitCluster.Status.NetworkID, stackitCluster.Spec.NetworkID)
	if networkID == "" {
		setReadyCondition(
			stackitMachine,
			metav1.ConditionFalse,
			reasonWaitingForNetworkReference,
			"Waiting for StackitCluster to resolve a STACKIT network",
		)
		return ctrl.Result{}, nil
	}

	stackitClient, err := r.stackitClient()
	if err != nil {
		setReadyCondition(
			stackitMachine,
			metav1.ConditionFalse,
			reasonStackitAPIError,
			fmt.Sprintf("Failed to initialize STACKIT client: %v", err),
		)
		return ctrl.Result{}, err
	}

	serverID, server, err := r.ensureServer(ctx, stackitClient, stackitCluster, networkID, stackitMachine)
	if err != nil {
		if err == stackit.ErrNotFound {
			setReadyCondition(
				stackitMachine,
				metav1.ConditionFalse,
				reasonServerNotFound,
				fmt.Sprintf("STACKIT server %q was not found", firstNonEmpty(stackitMachine.Status.InstanceID, stackitMachine.Spec.InstanceID)),
			)
			return ctrl.Result{}, nil
		}

		setReadyCondition(
			stackitMachine,
			metav1.ConditionFalse,
			reasonStackitAPIError,
			fmt.Sprintf("Failed to reconcile STACKIT server: %v", err),
		)
		return ctrl.Result{}, err
	}

	stackitMachine.Status.InstanceID = serverID
	stackitMachine.Spec.ProviderID = stackitProviderID(serverID)
	stackitMachine.Status.FailureDomain = firstNonEmpty(server.GetAvailabilityZone(), stackitMachine.Spec.AvailabilityZone)
	stackitMachine.Status.Addresses = machineAddressesFromServer(machine.Name, server)

	if serverIsProvisioned(server) {
		stackitMachine.Status.Initialization.Provisioned = ptr.To(true)
		setReadyCondition(
			stackitMachine,
			metav1.ConditionTrue,
			reasonServerReady,
			fmt.Sprintf("STACKIT server %q is in status %q", serverID, server.GetStatus()),
		)
		return ctrl.Result{}, nil
	}

	reason := reasonServerProvisioning
	message := fmt.Sprintf("STACKIT server %q is in status %q", serverID, server.GetStatus())
	if strings.EqualFold(server.GetStatus(), "ERROR") {
		reason = reasonServerError
		if server.GetErrorMessage() != "" {
			message = server.GetErrorMessage()
		}
	}

	setReadyCondition(
		stackitMachine,
		metav1.ConditionFalse,
		reason,
		message,
	)
	return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *StackitMachineReconciler) SetupWithManager(mgr ctrl.Manager) error {
	stackitMachineGVK := infrastructurev1alpha1.GroupVersion.WithKind("StackitMachine")

	return ctrl.NewControllerManagedBy(mgr).
		For(&infrastructurev1alpha1.StackitMachine{}).
		Watches(
			&clusterv1.Machine{},
			handler.EnqueueRequestsFromMapFunc(util.MachineToInfrastructureMapFunc(stackitMachineGVK)),
		).
		Named("stackitmachine").
		Complete(r)
}

func (r *StackitMachineReconciler) stackitClient() (stackit.Client, error) {
	factory := r.StackitFactory
	if factory == nil {
		factory = stackit.SDKClientFactory{}
	}
	return factory.NewClient()
}

func (r *StackitMachineReconciler) getStackitCluster(ctx context.Context, cluster *clusterv1.Cluster) (*infrastructurev1alpha1.StackitCluster, error) {
	if cluster == nil || !cluster.Spec.InfrastructureRef.IsDefined() {
		return nil, apierrors.NewNotFound(infrastructurev1alpha1.GroupVersion.WithResource("stackitclusters").GroupResource(), "")
	}

	if cluster.Spec.InfrastructureRef.GroupKind() != infrastructurev1alpha1.GroupVersion.WithKind("StackitCluster").GroupKind() {
		return nil, apierrors.NewNotFound(infrastructurev1alpha1.GroupVersion.WithResource("stackitclusters").GroupResource(), cluster.Spec.InfrastructureRef.Name)
	}

	stackitCluster := &infrastructurev1alpha1.StackitCluster{}
	key := client.ObjectKey{
		Namespace: cluster.Namespace,
		Name:      cluster.Spec.InfrastructureRef.Name,
	}

	if err := r.Get(ctx, key, stackitCluster); err != nil {
		return nil, err
	}
	return stackitCluster, nil
}

func (r *StackitMachineReconciler) ensureServer(ctx context.Context, stackitClient stackit.Client, stackitCluster *infrastructurev1alpha1.StackitCluster, networkID string, stackitMachine *infrastructurev1alpha1.StackitMachine) (string, *iaas.Server, error) {
	serverID := firstNonEmpty(stackitMachine.Spec.InstanceID, stackitMachine.Status.InstanceID)
	if serverID != "" {
		server, err := stackitClient.GetServer(ctx, stackitCluster.Spec.ProjectID, stackitCluster.Spec.Region, serverID)
		return serverID, server, err
	}

	servers, err := stackitClient.ListServersByLabel(ctx, stackitCluster.Spec.ProjectID, stackitCluster.Spec.Region, stackit.MachineUIDLabel, string(stackitMachine.UID))
	if err != nil {
		return "", nil, err
	}
	if len(servers) > 0 {
		serverID = servers[0].GetId()
		server, err := stackitClient.GetServer(ctx, stackitCluster.Spec.ProjectID, stackitCluster.Spec.Region, serverID)
		return serverID, server, err
	}

	server, err := stackitClient.CreateServer(ctx, stackit.CreateServerInput{
		ProjectID:         stackitCluster.Spec.ProjectID,
		Region:            stackitCluster.Spec.Region,
		NetworkID:         networkID,
		Name:              stackitMachineServerName(stackitMachine),
		MachineType:       stackitMachine.Spec.MachineType,
		AvailabilityZone:  stackitMachine.Spec.AvailabilityZone,
		ImageID:           stackitMachine.Spec.ImageID,
		BootVolumeSizeGiB: stackitMachine.Spec.BootVolumeSizeGiB,
		MachineUID:        string(stackitMachine.UID),
	})
	if err != nil {
		return "", nil, err
	}
	return server.GetId(), server, nil
}

func (r *StackitMachineReconciler) reconcileDelete(ctx context.Context, stackitMachine *infrastructurev1alpha1.StackitMachine) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(stackitMachine, stackitMachineFinalizer) {
		return ctrl.Result{}, nil
	}

	serverID := firstNonEmpty(stackitMachine.Status.InstanceID, stackitMachine.Spec.InstanceID)
	if serverID == "" {
		controllerutil.RemoveFinalizer(stackitMachine, stackitMachineFinalizer)
		return ctrl.Result{}, nil
	}
	if stackitMachine.Status.ProjectID == "" || stackitMachine.Status.Region == "" {
		return ctrl.Result{}, fmt.Errorf("cannot delete STACKIT server %q without status.projectID and status.region", serverID)
	}

	stackitClient, err := r.stackitClient()
	if err != nil {
		return ctrl.Result{}, err
	}

	server, err := stackitClient.GetServer(ctx, stackitMachine.Status.ProjectID, stackitMachine.Status.Region, serverID)
	switch err {
	case nil:
	case stackit.ErrNotFound:
		controllerutil.RemoveFinalizer(stackitMachine, stackitMachineFinalizer)
		return ctrl.Result{}, nil
	default:
		return ctrl.Result{}, err
	}

	if !strings.EqualFold(server.GetStatus(), "DELETING") && !strings.EqualFold(server.GetStatus(), "DELETED") {
		if err := stackitClient.DeleteServer(ctx, stackitMachine.Status.ProjectID, stackitMachine.Status.Region, serverID); err != nil && err != stackit.ErrNotFound {
			return ctrl.Result{}, err
		}
	}

	setReadyCondition(
		stackitMachine,
		metav1.ConditionFalse,
		reasonServerProvisioning,
		fmt.Sprintf("Waiting for STACKIT server %q to be deleted", serverID),
	)
	return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
}

func stackitMachineServerName(stackitMachine *infrastructurev1alpha1.StackitMachine) string {
	suffix := string(stackitMachine.UID)
	if len(suffix) > 8 {
		suffix = suffix[:8]
	}
	if suffix == "" {
		return stackitMachine.Name
	}

	name := fmt.Sprintf("%s-%s", stackitMachine.Name, suffix)
	if len(name) > 63 {
		name = name[:63]
		name = strings.TrimSuffix(name, "-")
	}
	return name
}

func machineAddressesFromServer(hostname string, server *iaas.Server) []clusterv1.MachineAddress {
	addresses := make([]clusterv1.MachineAddress, 0, 1+len(server.Nics)*2)
	seen := map[string]struct{}{}

	add := func(addressType clusterv1.MachineAddressType, address string) {
		address = strings.TrimSpace(address)
		if address == "" {
			return
		}
		key := string(addressType) + "/" + address
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		addresses = append(addresses, clusterv1.MachineAddress{Type: addressType, Address: address})
	}

	add(clusterv1.MachineHostName, firstNonEmpty(hostname, server.GetName()))
	for _, nic := range server.Nics {
		add(clusterv1.MachineInternalIP, nic.GetIpv4())
		add(clusterv1.MachineExternalIP, nic.GetPublicIp())
	}

	slices.SortFunc(addresses, func(a, b clusterv1.MachineAddress) int {
		if a.Type == b.Type {
			return strings.Compare(a.Address, b.Address)
		}
		return strings.Compare(string(a.Type), string(b.Type))
	})

	return addresses
}

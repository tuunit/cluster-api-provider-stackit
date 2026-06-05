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
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util/conditions"

	iaas "github.com/stackitcloud/stackit-sdk-go/services/iaas/v2api"
)

const (
	reasonControlPlaneEndpointConfigured  = "ControlPlaneEndpointConfigured"
	reasonWaitingForControlPlaneEndpoint  = "WaitingForControlPlaneEndpoint"
	reasonWaitingForNetworkReference      = "WaitingForNetworkReference"
	reasonCreatingManagedNetwork          = "CreatingManagedNetwork"
	reasonNetworkNotFound                 = "NetworkNotFound"
	reasonNetworkNotReady                 = "NetworkNotReady"
	reasonStackitAPIError                 = "StackitAPIError"
	reasonWaitingForClusterInfrastructure = "WaitingForClusterInfrastructure"
	reasonServerProvisioning              = "ServerProvisioning"
	reasonServerReady                     = "ServerReady"
	reasonServerError                     = "ServerError"
	reasonServerNotFound                  = "ServerNotFound"

	stackitMachineFinalizer = "stackitmachine.infrastructure.cluster.x-k8s.io"
	stackitClusterFinalizer = "stackitcluster.infrastructure.cluster.x-k8s.io"
)

type conditionSetter interface {
	conditions.Setter
	metav1.Object
}

func setPausedCondition(obj conditionSetter, clusterPaused, objectPaused bool) {
	if clusterPaused || objectPaused {
		messages := make([]string, 0, 2)
		if clusterPaused {
			messages = append(messages, "Cluster spec.paused is set to true")
		}
		if objectPaused {
			messages = append(messages, "Resource has the cluster.x-k8s.io/paused annotation")
		}

		conditions.Set(obj, metav1.Condition{
			Type:    clusterv1.PausedCondition,
			Status:  metav1.ConditionTrue,
			Reason:  clusterv1.PausedReason,
			Message: strings.Join(messages, ", "),
		})
		return
	}

	conditions.Set(obj, metav1.Condition{
		Type:   clusterv1.PausedCondition,
		Status: metav1.ConditionFalse,
		Reason: clusterv1.NotPausedReason,
	})
}

func setReadyCondition(obj conditionSetter, status metav1.ConditionStatus, reason, message string) {
	conditions.Set(obj, metav1.Condition{
		Type:    clusterv1.ReadyCondition,
		Status:  status,
		Reason:  reason,
		Message: message,
	})
}

func networkIsReady(network *iaas.Network) bool {
	if network == nil {
		return false
	}

	switch strings.ToUpper(network.GetStatus()) {
	case "CREATED", "UPDATED":
		return true
	default:
		return false
	}
}

func serverIsProvisioned(server *iaas.Server) bool {
	if server == nil {
		return false
	}

	switch strings.ToUpper(server.GetStatus()) {
	case "ACTIVE", "INACTIVE", "DEALLOCATED", "PAUSED":
		return true
	default:
		return false
	}
}

func stackitProviderID(serverID string) string {
	return fmt.Sprintf("stackit://%s", serverID)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

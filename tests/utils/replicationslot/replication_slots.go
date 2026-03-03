/*
Copyright © contributors to CloudNativePG, established as
CloudNativePG a Series of LF Projects, LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/

// Package replicationslot provides functions to manage the replication slot of a
// cnpg cluster
package replicationslot

import (
	"context"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/exec"
)

// PrintReplicationSlots prints replications slots with their restart_lsn
func PrintReplicationSlots(
	ctx context.Context,
	crudClient client.Client,
	kubeInterface kubernetes.Interface,
	restConfig *rest.Config,
	namespace, clusterName, dbName string,
) string {
	podList, err := clusterutils.ListPods(ctx, crudClient, namespace, clusterName)
	if err != nil {
		return fmt.Sprintf("Couldn't retrieve the cluster's podlist: %v\n", err)
	}
	var output strings.Builder
	for i, pod := range podList.Items {
		slots, err := GetReplicationSlotsOnPod(
			ctx, crudClient, kubeInterface, restConfig,
			namespace, pod.GetName(), dbName,
		)
		if err != nil {
			return fmt.Sprintf("Couldn't retrieve slots for pod %v: %v\n", pod.GetName(), err)
		}
		if len(slots) == 0 {
			return fmt.Sprintf("No Replication slots have been found on %v pod %v\n",
				pod.Labels[utils.ClusterInstanceRoleLabelName],
				pod.GetName())
		}
		m := make(map[string]string)
		for _, slot := range slots {
			query := fmt.Sprintf("SELECT restart_lsn FROM pg_catalog.pg_replication_slots WHERE slot_name = '%v'", slot)
			restartLsn, _, err := exec.QueryInInstancePod(
				ctx, crudClient, kubeInterface, restConfig,
				exec.PodLocator{
					Namespace: podList.Items[i].Namespace,
					PodName:   podList.Items[i].Name,
				},
				exec.DatabaseName(dbName),
				query)
			if err != nil {
				fmt.Fprintf(&output, "Couldn't retrieve restart_lsn for slot %v: %v\n", slot, err)
			}
			m[slot] = strings.TrimSpace(restartLsn)
		}
		fmt.Fprintf(&output, "Replication slots on %v pod %v: %v\n",
			pod.Labels[utils.ClusterInstanceRoleLabelName], pod.GetName(), m)
	}
	return output.String()
}

// AreSameLsn returns true if all the LSN values inside a given list are the same
func AreSameLsn(lsnList []string) bool {
	if len(lsnList) == 0 {
		return true
	}
	for _, lsn := range lsnList {
		if lsn != lsnList[0] {
			return false
		}
	}
	return true
}

// GetExpectedHAReplicationSlotsOnPod returns a slice of replication slot names which should be present
// in a given pod
func GetExpectedHAReplicationSlotsOnPod(
	ctx context.Context,
	crudClient client.Client,
	namespace, clusterName, podName string,
) ([]string, error) {
	podList, err := clusterutils.ListPods(ctx, crudClient, namespace, clusterName)
	if err != nil {
		return nil, err
	}

	cluster, err := clusterutils.Get(ctx, crudClient, namespace, clusterName)
	if err != nil {
		return nil, err
	}

	var slots []string
	for _, pod := range podList.Items {
		if pod.Name != podName && !specs.IsPodPrimary(pod) {
			replicationSlotName := cluster.GetSlotNameFromInstanceName(pod.Name)
			slots = append(slots, replicationSlotName)
		}
	}
	sort.Strings(slots)
	return slots, err
}

// GetReplicationSlotsOnPod returns a slice containing the names of the current replication slots present in
// a given pod
func GetReplicationSlotsOnPod(
	ctx context.Context,
	crudClient client.Client,
	kubeInterface kubernetes.Interface,
	restConfig *rest.Config,
	namespace, podName, dbName string,
) ([]string, error) {
	namespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      podName,
	}
	targetPod := &corev1.Pod{}
	err := crudClient.Get(ctx, namespacedName, targetPod)
	if err != nil {
		return nil, err
	}

	query := "SELECT slot_name FROM pg_catalog.pg_replication_slots WHERE temporary = 'f' AND slot_type = 'physical'"
	stdout, _, err := exec.QueryInInstancePod(
		ctx, crudClient, kubeInterface, restConfig,
		exec.PodLocator{
			Namespace: targetPod.Namespace,
			PodName:   targetPod.Name,
		},
		exec.DatabaseName(dbName),
		query)
	if err != nil {
		return nil, err
	}
	var slots []string
	// To avoid list with space entry when stdout value is empty
	// then just skip split and return empty list.
	if stdout != "" {
		slots = strings.Split(strings.TrimSpace(stdout), "\n")
		sort.Strings(slots)
	}
	return slots, nil
}

// GetReplicationSlotLsnsOnPod returns a slice containing the current restart_lsn values of each
// replication slot present in a given pod
func GetReplicationSlotLsnsOnPod(
	ctx context.Context,
	crudClient client.Client,
	kubeInterface kubernetes.Interface,
	restConfig *rest.Config,
	namespace, clusterName, dbName string,
	pod corev1.Pod,
) ([]string, error) {
	slots, err := GetExpectedHAReplicationSlotsOnPod(ctx, crudClient, namespace, clusterName, pod.GetName())
	if err != nil {
		return nil, err
	}

	lsnList := make([]string, 0, len(slots))
	for _, slot := range slots {
		query := fmt.Sprintf("SELECT restart_lsn FROM pg_catalog.pg_replication_slots WHERE slot_name = '%v'",
			slot)
		restartLsn, _, err := exec.QueryInInstancePod(
			ctx, crudClient, kubeInterface, restConfig,
			exec.PodLocator{
				Namespace: pod.Namespace,
				PodName:   pod.Name,
			},
			exec.DatabaseName(dbName),
			query)
		if err != nil {
			return nil, err
		}
		lsnList = append(lsnList, strings.TrimSpace(restartLsn))
	}
	return lsnList, err
}

// ToggleHAReplicationSlots sets the HA Replication Slot feature on/off depending on `enable`
func ToggleHAReplicationSlots(
	ctx context.Context,
	crudClient client.Client,
	namespace, clusterName string,
	enable bool,
) error {
	cluster, err := clusterutils.Get(ctx, crudClient, namespace, clusterName)
	if err != nil {
		return err
	}
	clusterToggle := cluster.DeepCopy()
	if clusterToggle.Spec.ReplicationSlots == nil {
		clusterToggle.Spec.ReplicationSlots = &apiv1.ReplicationSlotsConfiguration{}
	}

	if clusterToggle.Spec.ReplicationSlots.HighAvailability == nil {
		clusterToggle.Spec.ReplicationSlots.HighAvailability = &apiv1.ReplicationSlotsHAConfiguration{}
	}

	clusterToggle.Spec.ReplicationSlots.HighAvailability.Enabled = ptr.To(enable)
	err = crudClient.Patch(ctx, clusterToggle, client.MergeFrom(cluster))
	if err != nil {
		return err
	}
	return nil
}

// ToggleSynchronizeReplicationSlots sets the Synchronize Replication Slot feature on/off depending on `enable`
func ToggleSynchronizeReplicationSlots(
	ctx context.Context,
	crudClient client.Client,
	namespace, clusterName string,
	enable bool,
) error {
	cluster, err := clusterutils.Get(ctx, crudClient, namespace, clusterName)
	if err != nil {
		return err
	}
	clusterToggle := cluster.DeepCopy()
	if clusterToggle.Spec.ReplicationSlots == nil {
		clusterToggle.Spec.ReplicationSlots = &apiv1.ReplicationSlotsConfiguration{}
	}

	if clusterToggle.Spec.ReplicationSlots.SynchronizeReplicas == nil {
		clusterToggle.Spec.ReplicationSlots.SynchronizeReplicas = &apiv1.SynchronizeReplicasConfiguration{}
	}

	clusterToggle.Spec.ReplicationSlots.SynchronizeReplicas.Enabled = ptr.To(enable)
	err = crudClient.Patch(ctx, clusterToggle, client.MergeFrom(cluster))
	if err != nil {
		return err
	}
	return nil
}

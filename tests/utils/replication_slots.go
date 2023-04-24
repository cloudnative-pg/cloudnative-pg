/*
Copyright The CloudNativePG Contributors

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

package utils

import (
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
)

// PrintReplicationSlots prints replications slots with their restart_lsn
func PrintReplicationSlots(
	namespace,
	clusterName string,
	env *TestingEnvironment,
) string {
	podList, err := env.GetClusterPodList(namespace, clusterName)
	if err != nil {
		return fmt.Sprintf("Couldn't retrieve the cluster's podlist: %v\n", err)
	}
	var output strings.Builder
	for i, pod := range podList.Items {
		slots, err := GetReplicationSlotsOnPod(namespace, pod.GetName(), env)
		if err != nil {
			return fmt.Sprintf("Couldn't retrieve slots for pod %v: %v\n", pod.GetName(), err)
		}
		if len(slots) == 0 {
			return fmt.Sprintf("No Replication slots have been found on %v pod %v\n",
				pod.Labels["role"],
				pod.GetName())
		}
		m := make(map[string]string)
		for _, slot := range slots {
			restartLsn, _, err := RunQueryFromPod(
				&podList.Items[i], PGLocalSocketDir,
				"app",
				"postgres",
				"''",
				fmt.Sprintf("SELECT restart_lsn FROM pg_replication_slots WHERE slot_name = '%v'", slot),
				env)
			if err != nil {
				output.WriteString(fmt.Sprintf("Couldn't retrieve restart_lsn for slot %v: %v\n", slot, err))
			}
			m[slot] = strings.TrimSpace(restartLsn)
		}
		output.WriteString(fmt.Sprintf("Replication slots on %v pod %v: %v\n", pod.Labels["role"], pod.GetName(), m))
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

// GetExpectedReplicationSlotsOnPod returns a slice of replication slot names which should be present
// in a given pod
func GetExpectedReplicationSlotsOnPod(
	namespace, clusterName, podName string,
	env *TestingEnvironment,
) ([]string, error) {
	podList, err := env.GetClusterPodList(namespace, clusterName)
	if err != nil {
		return nil, err
	}

	cluster, err := env.GetCluster(namespace, clusterName)
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
func GetReplicationSlotsOnPod(namespace, podName string, env *TestingEnvironment) ([]string, error) {
	namespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      podName,
	}
	targetPod := &corev1.Pod{}
	err := env.Client.Get(env.Ctx, namespacedName, targetPod)
	if err != nil {
		return nil, err
	}

	stdout, _, err := RunQueryFromPod(targetPod, PGLocalSocketDir,
		"app", "postgres", "''",
		"SELECT slot_name FROM pg_replication_slots  WHERE temporary = 'f' AND slot_type = 'physical'", env)
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
	namespace, clusterName string,
	pod corev1.Pod,
	env *TestingEnvironment,
) ([]string, error) {
	slots, err := GetExpectedReplicationSlotsOnPod(namespace, clusterName, pod.GetName(), env)
	if err != nil {
		return nil, err
	}

	lsnList := make([]string, 0, len(slots))
	for _, slot := range slots {
		query := fmt.Sprintf("SELECT restart_lsn FROM pg_replication_slots WHERE slot_name = '%v'",
			slot)
		restartLsn, _, err := RunQueryFromPod(&pod, PGLocalSocketDir,
			"app", "postgres", "''", query, env)
		if err != nil {
			return nil, err
		}
		lsnList = append(lsnList, strings.TrimSpace(restartLsn))
	}
	return lsnList, err
}

// ToggleReplicationSlots sets the HA Replication Slot feature on/off depending on `enable`
func ToggleReplicationSlots(namespace, clusterName string, enable bool, env *TestingEnvironment) error {
	cluster, err := env.GetCluster(namespace, clusterName)
	if err != nil {
		return err
	}
	clusterToggle := cluster.DeepCopy()
	clusterToggle.Spec.ReplicationSlots.HighAvailability.Enabled = pointer.Bool(enable)
	err = env.Client.Patch(env.Ctx, clusterToggle, ctrlclient.MergeFrom(cluster))
	if err != nil {
		return err
	}
	return nil
}

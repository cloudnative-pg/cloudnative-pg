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

	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
)

// CompareLsn returns true if all the LSN values inside a given list are the same
func CompareLsn(lsnList []string) bool {
	for _, lsn := range lsnList {
		if lsn != lsnList[0] {
			return false
		}
	}
	return true
}

// GetExpectedRepSlotsOnPod returns a slice of replication slot names which should be present
// in a given pod
func GetExpectedRepSlotsOnPod(namespace, clusterName, podName string, env *TestingEnvironment) ([]string, error) {
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
			repSlotName := cluster.GetSlotNameFromInstanceName(pod.Name)
			slots = append(slots, repSlotName)
		}
	}
	sort.Strings(slots)
	return slots, err
}

// GetRepSlotsOnPod returns a slice containing the names of the current replication slots present in
// a given pod
func GetRepSlotsOnPod(namespace, podName string, env *TestingEnvironment) ([]string, error) {
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
		"SELECT slot_name FROM pg_replication_slots", env)
	if err != nil {
		return nil, err
	}

	var slots []string
	for _, slot := range strings.Split(strings.TrimSpace(stdout), "\n") {
		slots = append(slots, slot)
	}
	sort.Strings(slots)
	return slots, err
}

// GetRepSlotsLsnOnPod returns a slice containing the current restart_lsn values of each
// replication slot present in a given pod
func GetRepSlotsLsnOnPod(namespace, clusterName, podName string, env *TestingEnvironment) ([]string, error) {
	namespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      podName,
	}
	targetPod := &corev1.Pod{}
	err := env.Client.Get(env.Ctx, namespacedName, targetPod)
	if err != nil {
		return nil, err
	}

	slots, err := GetExpectedRepSlotsOnPod(namespace, clusterName, podName, env)
	if err != nil {
		return nil, err
	}

	lsnList := make([]string, 0, len(slots))
	for _, slot := range slots {
		query := fmt.Sprintf("SELECT restart_lsn FROM pg_replication_slots WHERE slot_name = '%v'",
			slot)
		restartLsn, _, err := RunQueryFromPod(targetPod, PGLocalSocketDir,
			"app", "postgres", "''", query, env)
		if err != nil {
			return nil, err
		}
		lsnList = append(lsnList, strings.TrimSpace(restartLsn))
	}
	return lsnList, err
}

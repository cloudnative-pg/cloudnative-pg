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

package postgres

import (
	corev1 "k8s.io/api/core/v1"
)

// SilentInstance is an instance whose /pg/status probe failed. Only the
// fields that are populated regardless of the probe outcome (see AddPod)
// are available here, so a caller cannot accidentally read an unobserved
// field as if it were real data.
type SilentInstance struct {
	// Pod is the instance's Pod, as known independently of the probe.
	Pod *corev1.Pod

	// Node is the name of the node the Pod is running on.
	Node string

	// Error is the error returned by the failed probe.
	Error error
}

// InstanceSet partitions a PostgresqlStatusList by what is actually known
// about each instance, so that decision code cannot read an observed field
// (e.g. IsWalReceiverActive, IsPrimary, TimeLineID) off an instance whose
// status probe failed.
type InstanceSet struct {
	// Reporting are the instances whose /pg/status probe succeeded. Every
	// field on these items is a real observation.
	Reporting PostgresqlStatusList

	// ReadyButSilent are instances whose probe failed while kubelet still
	// reports the Pod as Ready: the operator cannot tell whether PostgreSQL
	// stopped or the pod merely became unreachable, so these must be treated
	// as unknown, not as "down".
	ReadyButSilent []SilentInstance

	// NotReady are instances whose probe failed and kubelet no longer
	// reports the Pod as Ready: kubelet's readiness verdict is trusted over
	// the failing probe, so these can be treated as gone.
	NotReady []SilentInstance
}

// Partition splits the status list into the instances we actually heard
// from (Reporting) and the instances whose probe failed, further divided by
// whether kubelet still considers the Pod Ready (ReadyButSilent) or not
// (NotReady).
func (list PostgresqlStatusList) Partition() InstanceSet {
	set := InstanceSet{
		Reporting: PostgresqlStatusList{
			IsReplicaCluster: list.IsReplicaCluster,
			CurrentPrimary:   list.CurrentPrimary,
		},
	}

	for idx := range list.Items {
		item := &list.Items[idx]

		if item.Error == nil {
			set.Reporting.Items = append(set.Reporting.Items, *item)
			continue
		}

		silent := SilentInstance{
			Pod:   item.Pod,
			Node:  item.Node,
			Error: item.Error,
		}
		if item.IsPodReady {
			set.ReadyButSilent = append(set.ReadyButSilent, silent)
		} else {
			set.NotReady = append(set.NotReady, silent)
		}
	}

	return set
}

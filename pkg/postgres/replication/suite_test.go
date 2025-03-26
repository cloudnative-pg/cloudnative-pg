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

package replication

import (
	"fmt"
	"testing"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestReplication(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "PostgreSQL replication configuration test suite")
}

func createFakeCluster(name string) *apiv1.Cluster {
	primaryPod := fmt.Sprintf("%s-1", name)
	cluster := &apiv1.Cluster{}
	cluster.Name = name
	cluster.Default()
	cluster.Spec.Instances = 3
	cluster.Spec.MaxSyncReplicas = 2
	cluster.Spec.MinSyncReplicas = 1
	cluster.Status = apiv1.ClusterStatus{
		CurrentPrimary: primaryPod,
		InstancesStatus: map[apiv1.PodStatus][]string{
			apiv1.PodHealthy: {primaryPod, fmt.Sprintf("%s-2", name), fmt.Sprintf("%s-3", name)},
			apiv1.PodFailed:  {},
		},
	}
	return cluster
}

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

package v1

import (
	"fmt"
	"testing"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestApi(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "v1 API tests")
}

func createFakeCluster(name string) *Cluster {
	primaryPod := fmt.Sprintf("%s-1", name)
	cluster := &Cluster{}
	cluster.Default()
	cluster.Spec.Instances = 3
	cluster.Spec.MaxSyncReplicas = 2
	cluster.Spec.MinSyncReplicas = 1
	cluster.Status = ClusterStatus{
		CurrentPrimary: primaryPod,
		InstancesStatus: map[utils.PodStatus][]string{
			utils.PodHealthy: {primaryPod, fmt.Sprintf("%s-2", name), fmt.Sprintf("%s-3", name)},
			utils.PodFailed:  {},
		},
	}
	return cluster
}

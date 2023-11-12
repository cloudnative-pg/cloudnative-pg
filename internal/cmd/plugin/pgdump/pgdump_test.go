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

package pgdump

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("pgdump launcher", func() {
	podList := []corev1.Pod{
		fakePod("cluster-example-1", specs.ClusterRoleLabelReplica),
		fakePod("cluster-example-2", specs.ClusterRoleLabelPrimary),
		fakePod("cluster-example-3", specs.ClusterRoleLabelReplica),
	}

	It("selects the correct Pod when looking for a primary", func() {
		cmd := pgdumpCommand{
			pgdumpCommandOptions: pgdumpCommandOptions{
				replica: false,
			},
			podList: podList,
		}
		Expect(cmd.getPodName()).To(Equal("cluster-example-2"))
	})

	It("selects the correct Pod when looking for a replica", func() {
		cmd := pgdumpCommand{
			pgdumpCommandOptions: pgdumpCommandOptions{
				replica: true,
			},
			podList: podList,
		}
		Expect(cmd.getPodName()).To(Equal("cluster-example-1"))
	})

	It("raises an error when a Pod cannot be found", func() {
		fakePodList := []corev1.Pod{
			fakePod("cluster-example-1", "guitar"),
			fakePod("cluster-example-2", "piano"),
			fakePod("cluster-example-3", "oboe"),
		}

		cmd := pgdumpCommand{
			pgdumpCommandOptions: pgdumpCommandOptions{
				replica: false,
			},
			podList: fakePodList,
		}

		_, err := cmd.getPodName()
		Expect(err).To(HaveOccurred())
		Expect(err.(*ErrMissingPod)).ToNot(BeNil())
	})

	It("correctly composes a kubectl exec command line", func() {
		cmd := pgdumpCommand{
			pgdumpCommandOptions: pgdumpCommandOptions{
				replica:     true,
				allocateTTY: true,
				passStdin:   true,
				namespace:   "default",
			},
			podList: podList,
		}
		Expect(cmd.getKubectlInvocation()).To(ConsistOf(
			"kubectl",
			"exec",
			"-t",
			"-i",
			"-n",
			"default",
			"-c",
			"postgres",
			"cluster-example-1",
			"--",
			"pgdump",
		))
	})

	It("correctly composes a kubectl exec command line with pgdump args", func() {
		cmd := pgdumpCommand{
			pgdumpCommandOptions: pgdumpCommandOptions{
				replica:   true,
				namespace: "default",
				args: []string{
					"-n",
					"schema1",
				},
			},
			podList: podList,
		}
		Expect(cmd.getKubectlInvocation()).To(ConsistOf(
			"kubectl",
			"exec",
			"-n",
			"default",
			"-c",
			"postgres",
			"cluster-example-1",
			"--",
			"pgdump",
			"-n",
			"schema1",
		))
	})
})

func fakePod(name, role string) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Labels: map[string]string{
				utils.ClusterRoleLabelName: role,
			},
		},
	}
}

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

package sysbench

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("NewCmd", func() {
	It("should create a cobra.Command with correct defaults", func() {
		cmd := NewCmd()

		Expect(cmd.Use).To(Equal("sysbench <cluster-name> [-- sysbench_command_args...]"))
		Expect(cmd.Short).To(Equal("Creates a sysbench job"))
		Expect(cmd.Example).To(Equal(jobExample))

		// Check each flag exists and has the right default
		jobNameFlag := cmd.Flag("job-name")
		Expect(jobNameFlag).ToNot(BeNil())
		Expect(jobNameFlag.DefValue).To(Equal(""))

		dbNameFlag := cmd.Flag("db-name")
		Expect(dbNameFlag).ToNot(BeNil())
		Expect(dbNameFlag.DefValue).To(Equal("app"))

		imageFlag := cmd.Flag("sysbench-image")
		Expect(imageFlag).ToNot(BeNil())
		Expect(imageFlag.DefValue).To(Equal(defaultSysbenchImage))

		dryRunFlag := cmd.Flag("dry-run")
		Expect(dryRunFlag).ToNot(BeNil())
		Expect(dryRunFlag.DefValue).To(Equal("false"))

		nodeSelectorFlag := cmd.Flag("node-selector")
		Expect(nodeSelectorFlag).ToNot(BeNil())
		Expect(nodeSelectorFlag.DefValue).To(Equal("[]"))
	})
})

var _ = Describe("buildJob", func() {
	It("should use the sysbench image, not the cluster image", func() {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "default",
			},
		}

		cmd := &sysbenchRun{
			clusterName:   "test-cluster",
			dbName:        "app",
			sysbenchImage: "perconalab/sysbench:1.1",
		}

		job := cmd.buildJob(cluster)

		Expect(job.Spec.Template.Spec.Containers[0].Image).To(Equal("perconalab/sysbench:1.1"))
	})

	It("should propagate ImagePullSecrets from the Cluster", func() {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "default",
			},
			Spec: apiv1.ClusterSpec{
				ImagePullSecrets: []apiv1.LocalObjectReference{
					{Name: "my-secret"},
				},
			},
		}

		cmd := &sysbenchRun{
			clusterName:   "test-cluster",
			dbName:        "app",
			sysbenchImage: "perconalab/sysbench:1.1",
		}

		job := cmd.buildJob(cluster)

		Expect(job.Spec.Template.Spec.ImagePullSecrets).To(ConsistOf(
			corev1.LocalObjectReference{Name: "my-secret"},
		))
	})
})

var _ = Describe("buildArgs", func() {
	It("should prepend connection args before user args", func() {
		cmd := &sysbenchRun{
			clusterName:         "my-cluster",
			dbName:              "testdb",
			sysbenchCommandArgs: []string{"oltp_read_write", "run"},
		}

		args := cmd.buildArgs()

		Expect(args[0]).To(Equal("--db-driver=pgsql"))
		Expect(args).To(ContainElement("--pgsql-db=testdb"))
		Expect(args).To(ContainElement("oltp_read_write"))
		Expect(args).To(ContainElement("run"))
	})
})

var _ = Describe("getJobName", func() {
	It("should return custom name when set", func() {
		cmd := &sysbenchRun{
			clusterName: "my-cluster",
			jobName:     "my-custom-job",
		}
		Expect(cmd.getJobName()).To(Equal("my-custom-job"))
	})

	It("should generate a name when not set", func() {
		cmd := &sysbenchRun{
			clusterName: "my-cluster",
		}
		Expect(cmd.getJobName()).To(HavePrefix("my-cluster-sysbench-"))
	})
})

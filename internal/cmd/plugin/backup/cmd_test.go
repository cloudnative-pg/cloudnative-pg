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

package backup

import (
	"io"
	"os"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
	"github.com/cloudnative-pg/cloudnative-pg/internal/scheme"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func captureStdout(f func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	f()

	_ = w.Close()
	os.Stdout = old

	out, _ := io.ReadAll(r)
	return string(out)
}

var _ = Describe("NewCmd", func() {
	It("should register the dry-run flag with a false default", func() {
		cmd := NewCmd()

		dryRunFlag := cmd.Flag("dry-run")
		Expect(dryRunFlag).ToNot(BeNil())
		Expect(dryRunFlag.DefValue).To(Equal("false"))
	})
})

var _ = Describe("createBackup", func() {
	const (
		clusterName = "cluster-example"
		namespace   = "test-ns"
		backupName  = "cluster-example-backup"
	)

	BeforeEach(func() {
		plugin.Namespace = namespace
	})

	It("should not create the Backup resource when dry-run is set", func(ctx SpecContext) {
		plugin.Client = fake.NewClientBuilder().
			WithScheme(scheme.BuildWithAllKnownScheme()).
			Build()

		var err error
		stdout := captureStdout(func() {
			err = createBackup(ctx, backupCommandOptions{
				backupName:  backupName,
				clusterName: clusterName,
				dryRun:      true,
			})
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(stdout).To(ContainSubstring("kind: Backup"))
		Expect(stdout).To(ContainSubstring("name: " + backupName))

		var created apiv1.Backup
		err = plugin.Client.Get(ctx,
			types.NamespacedName{Name: backupName, Namespace: namespace},
			&created,
		)
		Expect(err).To(HaveOccurred())
	})

	It("should create the Backup resource when dry-run is not set", func(ctx SpecContext) {
		plugin.Client = fake.NewClientBuilder().
			WithScheme(scheme.BuildWithAllKnownScheme()).
			Build()

		Expect(createBackup(ctx, backupCommandOptions{
			backupName:  backupName,
			clusterName: clusterName,
		})).To(Succeed())

		var created apiv1.Backup
		Expect(plugin.Client.Get(ctx,
			types.NamespacedName{Name: backupName, Namespace: namespace},
			&created,
		)).To(Succeed())
		Expect(created.Spec.Cluster.Name).To(Equal(clusterName))
	})
})

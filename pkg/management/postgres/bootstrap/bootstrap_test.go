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

package bootstrap

import (
	"context"
	"os"
	"path/filepath"

	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The restoresnapshot mode must never wipe a data directory: a snapshot-seeded
// PGDATA cannot be re-obtained. The other modes must wipe it before redoing the
// bootstrap. We drive Execute with a data directory carrying a sentinel file and
// a client that has no Cluster, so every mode fails, and we check whether the
// sentinel survived the failed attempt.
var _ = Describe("per-mode target directory cleanup", func() {
	var pgData string
	var sentinel string

	BeforeEach(func() {
		pgData = GinkgoT().TempDir()
		sentinel = filepath.Join(pgData, "sentinel")
		Expect(os.WriteFile(sentinel, []byte("data"), 0o600)).To(Succeed())
	})

	newClient := func() *fake.ClientBuilder {
		return fake.NewClientBuilder().WithScheme(scheme.BuildWithAllKnownScheme())
	}

	It("does not wipe the data directory in restoresnapshot mode", func() {
		info := postgres.InitInfo{PgData: pgData, ClusterName: "cluster", Namespace: "default"}
		err := Execute(
			context.Background(),
			newClient().Build(),
			nil,
			info,
			Instruction{Mode: ModeRestoreSnapshot, Immediate: true},
		)
		Expect(err).To(HaveOccurred())

		_, statErr := os.Stat(sentinel)
		Expect(statErr).ToNot(HaveOccurred(), "restoresnapshot must not remove the seeded data directory")
	})

	It("wipes the data directory in restore mode", func() {
		info := postgres.InitInfo{PgData: pgData, ClusterName: "cluster", Namespace: "default"}
		err := Execute(
			context.Background(),
			newClient().Build(),
			nil,
			info,
			Instruction{Mode: ModeRestore},
		)
		Expect(err).To(HaveOccurred())

		_, statErr := os.Stat(sentinel)
		Expect(os.IsNotExist(statErr)).To(BeTrue(), "restore must clear the pre-existing data directory")
	})

	It("rejects an unknown bootstrap mode", func() {
		err := Execute(
			context.Background(),
			newClient().Build(),
			nil,
			postgres.InitInfo{PgData: pgData},
			Instruction{Mode: "nonsense"},
		)
		Expect(err).To(MatchError(ContainSubstring("unknown bootstrap mode")))
	})
})

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

package controller

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("refreshConfigurationFiles", func() {
	It("reports the files it changed before failing", func(ctx SpecContext) {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "cluster-example"},
		}

		// Without PG_VERSION the PostgreSQL configuration cannot be generated,
		// and that happens after pg_hba.conf and pg_ident.conf are written
		pgInstance := postgres.NewInstance()
		pgInstance.PgData = GinkgoT().TempDir()
		reconciler := &InstanceReconciler{instance: pgInstance}

		reloadNeeded, err := reconciler.refreshConfigurationFiles(ctx, cluster)

		Expect(err).To(HaveOccurred())
		Expect(reloadNeeded).To(BeTrue())
	})
})

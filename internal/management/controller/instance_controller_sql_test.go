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
	"database/sql"
	"fmt"

	"github.com/DATA-DOG/go-sqlmock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	postgresSpec "github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("reconcileExtensions search_path", func() {
	const existenceQuery = "SELECT COUNT(*) > 0 FROM pg_catalog.pg_extension WHERE extname = $1"

	var (
		dbMock sqlmock.Sqlmock
		db     *sql.DB
		err    error
	)

	BeforeEach(func() {
		db, dbMock, err = sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		Expect(dbMock.ExpectationsWereMet()).To(Succeed())
		_ = db.Close()
	})

	It("brackets CREATE EXTENSION with the standard \"$user\", public search_path", func(ctx SpecContext) {
		// Pick a managed extension that the operator creates explicitly
		// (not shared-preload only) so the CREATE EXTENSION branch runs.
		var target postgresSpec.ManagedExtension
		for _, ext := range postgresSpec.ManagedExtensions {
			if !ext.SkipCreateExtension && len(ext.Namespaces) > 0 {
				target = ext
				break
			}
		}
		Expect(target.Name).ToNot(BeEmpty(), "expected at least one creatable managed extension")

		// Enable only the target extension via its configuration namespace.
		userSettings := map[string]string{target.Namespaces[0] + ".max": "1000"}

		dbMock.ExpectBegin()
		for _, ext := range postgresSpec.ManagedExtensions {
			// Report every extension as not yet installed.
			dbMock.ExpectQuery(existenceQuery).WithArgs(ext.Name).
				WillReturnRows(sqlmock.NewRows([]string{"?column?"}).AddRow(false))

			if ext.Name == target.Name {
				// The fix: search_path is set to the standard resolution
				// immediately before CREATE EXTENSION so the relocatable
				// extension is not created against the pinned pg_catalog.
				dbMock.ExpectExec(`SET LOCAL search_path TO "$user", public`).
					WillReturnResult(sqlmock.NewResult(0, 0))
				dbMock.ExpectExec(fmt.Sprintf("CREATE EXTENSION %s", ext.Name)).
					WillReturnResult(sqlmock.NewResult(0, 0))
			}
		}
		dbMock.ExpectCommit()

		r := &InstanceReconciler{}
		Expect(r.reconcileExtensions(ctx, db, userSettings)).To(Succeed())
	})

	It("does not touch search_path when no extension needs to be created", func(ctx SpecContext) {
		// No user settings -> no managed extension is in use. Each extension is
		// only probed for existence; no SET LOCAL / CREATE EXTENSION is issued.
		dbMock.ExpectBegin()
		for _, ext := range postgresSpec.ManagedExtensions {
			dbMock.ExpectQuery(existenceQuery).WithArgs(ext.Name).
				WillReturnRows(sqlmock.NewRows([]string{"?column?"}).AddRow(false))
		}
		dbMock.ExpectCommit()

		r := &InstanceReconciler{}
		Expect(r.reconcileExtensions(ctx, db, map[string]string{})).To(Succeed())
	})
})

var _ = Describe("reconcileSuperuserPassword", func() {
	const (
		namespace  = "default"
		secretName = "superuser-secret"
	)

	var (
		dbMock sqlmock.Sqlmock
		db     *sql.DB
		err    error

		reconciler *InstanceReconciler
		cluster    *apiv1.Cluster
		secretRV   string
	)

	BeforeEach(func(ctx SpecContext) {
		db, dbMock, err = sqlmock.New()
		Expect(err).ToNot(HaveOccurred())

		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: namespace},
			Data: map[string][]byte{
				corev1.BasicAuthUsernameKey: []byte("postgres"),
				corev1.BasicAuthPasswordKey: []byte("supersecret"),
			},
		}
		fakeClient := fake.NewClientBuilder().
			WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
			WithObjects(secret).
			Build()

		// Read back the resource version assigned by the fake client so we can
		// pre-seed the in-memory cache as if the password had already been applied.
		applied := &corev1.Secret{}
		Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(secret), applied)).To(Succeed())
		secretRV = applied.ResourceVersion
		Expect(secretRV).NotTo(BeEmpty())

		reconciler = &InstanceReconciler{
			client: fakeClient,
			instance: postgres.NewInstance().
				WithNamespace(namespace).
				WithPodName("cluster-example-1").
				WithClusterName("cluster-example"),
			secretVersions: map[string]string{
				// the superuser password was already applied during a previous reconcile
				secretName: secretRV,
			},
		}

		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster-example", Namespace: namespace},
			Spec: apiv1.ClusterSpec{
				SuperuserSecret: &apiv1.LocalObjectReference{Name: secretName},
			},
		}
	})

	AfterEach(func() {
		Expect(dbMock.ExpectationsWereMet()).To(Succeed())
	})

	It("re-applies the superuser password when access is re-enabled after being disabled", func(ctx SpecContext) {
		// 1) Disable superuser access: the password is dropped in PostgreSQL.
		disabled := false
		cluster.Spec.EnableSuperuserAccess = &disabled

		dbMock.ExpectQuery("SELECT rolpassword IS NOT NULL").
			WillReturnRows(sqlmock.NewRows([]string{"has_password"}).AddRow(true))
		dbMock.ExpectBegin()
		dbMock.ExpectExec("ALTER ROLE postgres WITH PASSWORD NULL").
			WillReturnResult(sqlmock.NewResult(0, 1))
		dbMock.ExpectCommit()

		Expect(reconciler.reconcileSuperuserPassword(ctx, cluster, db)).To(Succeed())

		// 2) Re-enable superuser access. The secret is unchanged (same resource
		//    version), so without invalidating the cache the password would never
		//    be re-applied (this is the bug from #9721). We therefore expect the
		//    ALTER ROLE ... WITH PASSWORD statement to run again.
		enabled := true
		cluster.Spec.EnableSuperuserAccess = &enabled

		dbMock.ExpectBegin()
		dbMock.ExpectExec("SET LOCAL log_statement").WillReturnResult(sqlmock.NewResult(0, 0))
		dbMock.ExpectExec("SET LOCAL log_min_error_statement").WillReturnResult(sqlmock.NewResult(0, 0))
		dbMock.ExpectExec("ALTER ROLE").WillReturnResult(sqlmock.NewResult(0, 1))
		dbMock.ExpectCommit()

		Expect(reconciler.reconcileSuperuserPassword(ctx, cluster, db)).To(Succeed())
	})
})

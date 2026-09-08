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
	"context"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("DatabaseSecret operator-side controller", func() {
	ctx := context.Background()

	buildReconciler := func(objs ...client.Object) (*DatabaseSecretReconciler, client.Client) {
		scheme := schemeBuilder.BuildWithAllKnownScheme()
		cli := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(objs...).
			Build()
		return &DatabaseSecretReconciler{Client: cli, Scheme: scheme}, cli
	}

	newDatabase := func(name, owner string) *apiv1.Database {
		return &apiv1.Database{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec: apiv1.DatabaseSpec{
				ClusterRef: corev1.LocalObjectReference{Name: "cluster-example"},
				Name:       name + "-db",
				Owner:      owner,
			},
		}
	}

	newRole := func(name, secretName string) *apiv1.DatabaseRole {
		role := &apiv1.DatabaseRole{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec: apiv1.DatabaseRoleSpec{
				RoleConfiguration: apiv1.RoleConfiguration{Name: name},
				ClusterRef:        corev1.LocalObjectReference{Name: "cluster-example"},
			},
		}
		if secretName != "" {
			role.Spec.PasswordSecret = &apiv1.LocalObjectReference{Name: secretName}
		}
		return role
	}

	newPasswordSecret := func(name, password string) *corev1.Secret {
		return &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Data:       map[string][]byte{corev1.BasicAuthPasswordKey: []byte(password)},
		}
	}

	requestFor := func(db *apiv1.Database) ctrl.Request {
		return ctrl.Request{NamespacedName: types.NamespacedName{Namespace: db.Namespace, Name: db.Name}}
	}

	getConnectionSecret := func(cli client.Client, db *apiv1.Database) (*corev1.Secret, error) {
		secret := &corev1.Secret{}
		err := cli.Get(ctx, types.NamespacedName{
			Namespace: db.Namespace,
			Name:      db.GetConnectionSecretName(),
		}, secret)
		return secret, err
	}

	It("Case A: generates a connection Secret with credentials when the role and password exist", func() {
		db := newDatabase("app", "app-owner")
		role := newRole("app-owner", "owner-secret")
		passwordSecret := newPasswordSecret("owner-secret", "supersecret")
		r, cli := buildReconciler(db, role, passwordSecret)

		_, err := r.Reconcile(ctx, requestFor(db))
		Expect(err).NotTo(HaveOccurred())

		secret, err := getConnectionSecret(cli, db)
		Expect(err).NotTo(HaveOccurred())

		Expect(secret.Data).To(HaveKeyWithValue("username", []byte("app-owner")))
		Expect(secret.Data).To(HaveKeyWithValue("dbname", []byte("app-db")))
		Expect(secret.Data).To(HaveKeyWithValue("host", []byte("cluster-example-rw")))
		Expect(secret.Data).To(HaveKeyWithValue("password", []byte("supersecret")))
		Expect(string(secret.Data["uri"])).To(ContainSubstring("supersecret"))
		Expect(string(secret.Data["uri"])).To(ContainSubstring("app-owner"))

		// The Secret must be owned by the Database for garbage collection.
		Expect(metav1.IsControlledBy(secret, db)).To(BeTrue())
		Expect(secret.Labels).To(HaveKeyWithValue(utils.UserTypeLabelName, string(utils.UserTypeApp)))
	})

	It("Case B: generates a Secret without password when the password Secret is missing", func() {
		db := newDatabase("app", "app-owner")
		role := newRole("app-owner", "owner-secret")
		// password secret intentionally not created
		r, cli := buildReconciler(db, role)

		_, err := r.Reconcile(ctx, requestFor(db))
		Expect(err).NotTo(HaveOccurred())

		secret, err := getConnectionSecret(cli, db)
		Expect(err).NotTo(HaveOccurred())

		Expect(secret.Data).To(HaveKeyWithValue("username", []byte("app-owner")))
		Expect(secret.Data).To(HaveKeyWithValue("dbname", []byte("app-db")))
		Expect(secret.Data).To(HaveKeyWithValue("host", []byte("cluster-example-rw")))

		// password-dependent fields must be omitted
		Expect(secret.Data).NotTo(HaveKey("password"))
		Expect(secret.Data).NotTo(HaveKey("uri"))
		Expect(secret.Data).NotTo(HaveKey("jdbc-uri"))
		Expect(secret.Data).NotTo(HaveKey("pgpass"))
	})

	It("Case C: still generates a Secret when the owner role cannot be resolved", func() {
		db := newDatabase("app", "missing-owner")
		// no DatabaseRole matching the owner
		r, cli := buildReconciler(db)

		_, err := r.Reconcile(ctx, requestFor(db))
		Expect(err).NotTo(HaveOccurred())

		secret, err := getConnectionSecret(cli, db)
		Expect(err).NotTo(HaveOccurred())

		Expect(secret.Data).To(HaveKeyWithValue("username", []byte("missing-owner")))
		Expect(secret.Data).To(HaveKeyWithValue("dbname", []byte("app-db")))
		Expect(secret.Data).NotTo(HaveKey("password"))
	})

	It("ignores a same-named Secret that is not owned by the Database", func() {
		db := newDatabase("app", "app-owner")
		foreignSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      db.GetConnectionSecretName(),
				Namespace: db.Namespace,
			},
			Data: map[string][]byte{"untouched": []byte("true")},
		}
		r, cli := buildReconciler(db, foreignSecret)

		_, err := r.Reconcile(ctx, requestFor(db))
		Expect(err).NotTo(HaveOccurred())

		secret, err := getConnectionSecret(cli, db)
		Expect(err).NotTo(HaveOccurred())
		Expect(secret.Data).To(HaveKeyWithValue("untouched", []byte("true")))
		Expect(secret.Data).NotTo(HaveKey("username"))
	})

	It("updates the owned Secret when the password rotates", func() {
		db := newDatabase("app", "app-owner")
		role := newRole("app-owner", "owner-secret")
		passwordSecret := newPasswordSecret("owner-secret", "old-password")
		r, cli := buildReconciler(db, role, passwordSecret)

		_, err := r.Reconcile(ctx, requestFor(db))
		Expect(err).NotTo(HaveOccurred())

		stored := &corev1.Secret{}
		Expect(cli.Get(ctx, types.NamespacedName{Namespace: "default", Name: "owner-secret"}, stored)).To(Succeed())
		stored.Data[corev1.BasicAuthPasswordKey] = []byte("new-password")
		Expect(cli.Update(ctx, stored)).To(Succeed())

		_, err = r.Reconcile(ctx, requestFor(db))
		Expect(err).NotTo(HaveOccurred())

		secret, err := getConnectionSecret(cli, db)
		Expect(err).NotTo(HaveOccurred())
		Expect(secret.Data).To(HaveKeyWithValue("password", []byte("new-password")))
		Expect(string(secret.Data["uri"])).To(ContainSubstring("new-password"))
	})

	It("returns no error and creates no Secret when the Database does not exist", func() {
		r, cli := buildReconciler()

		_, err := r.Reconcile(ctx, ctrl.Request{
			NamespacedName: types.NamespacedName{Namespace: "default", Name: "ghost"},
		})
		Expect(err).NotTo(HaveOccurred())

		secret := &corev1.Secret{}
		err = cli.Get(ctx, types.NamespacedName{Namespace: "default", Name: "ghost-conn"}, secret)
		Expect(apierrs.IsNotFound(err)).To(BeTrue())
	})

	It("maps a password Secret change to the databases owned by the referencing role", func() {
		db := newDatabase("app", "app-owner")
		role := newRole("app-owner", "owner-secret")
		passwordSecret := newPasswordSecret("owner-secret", "p")
		r, _ := buildReconciler(db, role, passwordSecret)

		requests := r.mapPasswordSecretToDatabases()(ctx, passwordSecret)
		Expect(requests).To(HaveLen(1))
		Expect(requests[0].Name).To(Equal("app"))
	})

	It("maps a DatabaseRole change to the databases it owns", func() {
		db := newDatabase("app", "app-owner")
		role := newRole("app-owner", "")
		r, _ := buildReconciler(db, role)

		requests := r.mapRoleToDatabases()(ctx, role)
		Expect(requests).To(HaveLen(1))
		Expect(requests[0].Name).To(Equal("app"))
	})

	It("does not map an unrelated DatabaseRole to a database", func() {
		db := newDatabase("app", "app-owner")
		role := newRole("other-owner", "")
		r, _ := buildReconciler(db, role)

		requests := r.mapRoleToDatabases()(ctx, role)
		Expect(requests).To(BeEmpty())
	})

	It("builds an FQDN URI using the database namespace", func() {
		db := newDatabase("app", "app-owner")
		role := newRole("app-owner", "owner-secret")
		passwordSecret := newPasswordSecret("owner-secret", "pw")
		r, cli := buildReconciler(db, role, passwordSecret)

		_, err := r.Reconcile(ctx, requestFor(db))
		Expect(err).NotTo(HaveOccurred())

		secret, err := getConnectionSecret(cli, db)
		Expect(err).NotTo(HaveOccurred())
		Expect(strings.Contains(string(secret.Data["fqdn-uri"]), "cluster-example-rw.default")).To(BeTrue())
	})
})

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
	"errors"

	rbacv1 "k8s.io/api/rbac/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	k8client "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("cross-namespace RBAC controller", func() {
	const (
		clusterNamespace = "postgres"
		failingTenant    = "terminating"
		healthyTenant    = "healthy"
	)

	var (
		ctx        context.Context
		fakeClient k8client.Client
		recorder   *record.FakeRecorder
		reconciler crossNamespaceRBACReconciler
		cluster    *apiv1.Cluster
		rbacName   string
	)

	newRole := func(namespace, name string) *apiv1.DatabaseRole {
		return &apiv1.DatabaseRole{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			Spec: apiv1.DatabaseRoleSpec{
				RoleConfiguration: apiv1.RoleConfiguration{
					Name:           name,
					PasswordSecret: &apiv1.LocalObjectReference{Name: name + "-secret"},
				},
				ClusterRef: apiv1.ClusterObjectReference{Name: cluster.Name, Namespace: cluster.Namespace},
			},
		}
	}

	build := func(funcs interceptor.Funcs, objects ...k8client.Object) {
		fakeClient = newClusterIndexedFakeClientBuilder(schemeBuilder.BuildWithAllKnownScheme()).
			WithObjects(objects...).
			WithInterceptorFuncs(funcs).
			Build()
		recorder = record.NewFakeRecorder(1000)
		reconciler = crossNamespaceRBACReconciler{ClusterReconciler: &ClusterReconciler{
			Client:   fakeClient,
			Recorder: recorder,
			Scheme:   schemeBuilder.BuildWithAllKnownScheme(),
		}}
	}

	// events drains the events recorded so far
	events := func() []string {
		var result []string
		for {
			select {
			case event := <-recorder.Events:
				result = append(result, event)
			default:
				return result
			}
		}
	}

	request := func() ctrl.Request {
		return ctrl.Request{NamespacedName: k8client.ObjectKeyFromObject(cluster)}
	}

	exists := func(obj k8client.Object, namespace string) bool {
		err := fakeClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: rbacName}, obj)
		if apierrs.IsNotFound(err) {
			return false
		}
		Expect(err).NotTo(HaveOccurred())
		return true
	}

	BeforeEach(func() {
		ctx = context.Background()
		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "shared", Namespace: clusterNamespace},
			Spec:       apiv1.ClusterSpec{Instances: 1, EnableCrossNamespaceResources: true},
		}
		rbacName = specs.GetCrossNamespaceRoleName(*cluster)
	})

	It("keeps reconciling the other tenant namespaces when one of them fails", func() {
		terminating := apierrs.NewForbidden(
			schema.GroupResource{Group: rbacv1.GroupName, Resource: "roles"}, rbacName,
			errors.New("namespace is being terminated"))
		build(interceptor.Funcs{
			Create: func(
				ctx context.Context, cli k8client.WithWatch, obj k8client.Object, opts ...k8client.CreateOption,
			) error {
				if obj.GetNamespace() == failingTenant {
					return terminating
				}
				return cli.Create(ctx, obj, opts...)
			},
		}, cluster, newRole(failingTenant, "alice"), newRole(healthyTenant, "bob"))

		_, err := reconciler.Reconcile(ctx, request())
		Expect(err).To(MatchError(terminating))
		Expect(events()).To(ContainElement(ContainSubstring("CrossNamespaceRBACFailed")))

		Expect(exists(&rbacv1.ClusterRole{}, "")).To(BeTrue())
		Expect(exists(&rbacv1.ClusterRoleBinding{}, "")).To(BeTrue())
		Expect(exists(&rbacv1.Role{}, healthyTenant)).To(BeTrue())
		Expect(exists(&rbacv1.RoleBinding{}, healthyTenant)).To(BeTrue())
	})

	It("removes the RBAC objects left for a cluster not enabling the feature", func() {
		// A previous cluster having the same name enabled it
		leftover := specs.CreateCrossNamespaceRoleBinding(*cluster)
		leftoverRole := specs.CreateCrossNamespaceRole(*cluster)
		cluster.Spec.EnableCrossNamespaceResources = false
		build(interceptor.Funcs{}, cluster, &leftover, &leftoverRole)

		_, err := reconciler.Reconcile(ctx, request())
		Expect(err).NotTo(HaveOccurred())

		Expect(exists(&rbacv1.ClusterRole{}, "")).To(BeFalse())
		Expect(exists(&rbacv1.ClusterRoleBinding{}, "")).To(BeFalse())
	})

	It("ignores a cluster that does not exist", func() {
		build(interceptor.Funcs{})

		_, err := reconciler.Reconcile(ctx, request())
		Expect(err).NotTo(HaveOccurred())
	})

	It("reports a ClusterRole hidden from the cache as not owned", func() {
		// The cache only holds the ClusterRoles carrying the labels of the
		// operator: one squatting the name without them cannot be read
		foreign := &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: rbacName}}
		build(interceptor.Funcs{
			Get: func(
				ctx context.Context, cli k8client.WithWatch, key k8client.ObjectKey, obj k8client.Object,
				opts ...k8client.GetOption,
			) error {
				if _, ok := obj.(*rbacv1.ClusterRole); ok {
					return apierrs.NewNotFound(schema.GroupResource{Resource: "clusterroles"}, key.Name)
				}
				return cli.Get(ctx, key, obj, opts...)
			},
		}, cluster, foreign)

		_, err := reconciler.Reconcile(ctx, request())
		Expect(err).To(MatchError(errCrossNamespaceRBACNotOwned))
	})

	It("does not bind the ServiceAccount to a Role not created for the cluster", func() {
		foreign := &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: rbacName, Namespace: healthyTenant}}
		build(interceptor.Funcs{}, cluster, newRole(healthyTenant, "bob"), foreign)

		_, err := reconciler.Reconcile(ctx, request())
		Expect(err).NotTo(HaveOccurred())

		Expect(exists(&rbacv1.RoleBinding{}, healthyTenant)).To(BeFalse())
		Expect(events()).To(ContainElement(ContainSubstring("CrossNamespaceRBACNotOwned")))
	})

	Describe("mapCrossNamespaceRBACToCluster", func() {
		It("enqueues the cluster recorded in the labels", func() {
			role := specs.CreateCrossNamespaceRole(*cluster)
			Expect(mapCrossNamespaceRBACToCluster(ctx, &role)).To(Equal([]reconcile.Request{
				{NamespacedName: k8client.ObjectKeyFromObject(cluster)},
			}))
		})

		It("ignores the objects not managed by the operator", func() {
			role := specs.CreateCrossNamespaceRole(*cluster)
			delete(role.Labels, "app.kubernetes.io/managed-by")
			Expect(mapCrossNamespaceRBACToCluster(ctx, &role)).To(BeEmpty())

			Expect(mapCrossNamespaceRBACToCluster(ctx, &rbacv1.Role{})).To(BeEmpty())
		})
	})
})

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
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("CrossNamespaceRBACSweeper", func() {
	var (
		ctx        context.Context
		fakeClient client.Client
		getErr     error
	)

	newCluster := func(namespace, name string) *apiv1.Cluster {
		return &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			Spec: apiv1.ClusterSpec{
				Instances:                     1,
				EnableCrossNamespaceResources: true,
			},
		}
	}

	// createWithRBAC creates an opted-in cluster and lets the cluster
	// reconciler create its cross-namespace RBAC objects
	createWithRBAC := func(namespace, name string) *apiv1.Cluster {
		cluster := newCluster(namespace, name)
		Expect(fakeClient.Create(ctx, cluster)).To(Succeed())
		reconciler := &ClusterReconciler{
			Client:   fakeClient,
			Recorder: record.NewFakeRecorder(100),
			Scheme:   fakeClient.Scheme(),
		}
		Expect(reconciler.reconcileCrossNamespaceRBAC(ctx, cluster)).To(Succeed())
		return cluster
	}

	rbacExists := func(cluster *apiv1.Cluster) (bool, bool) {
		name := specs.GetCrossNamespaceRoleName(*cluster)
		roleErr := fakeClient.Get(ctx, client.ObjectKey{Name: name}, &rbacv1.ClusterRole{})
		bindingErr := fakeClient.Get(ctx, client.ObjectKey{Name: name}, &rbacv1.ClusterRoleBinding{})
		for _, err := range []error{roleErr, bindingErr} {
			if err != nil {
				Expect(apierrs.IsNotFound(err)).To(BeTrue())
			}
		}
		return roleErr == nil, bindingErr == nil
	}

	expectRBAC := func(cluster *apiv1.Cluster, present bool) {
		role, binding := rbacExists(cluster)
		Expect(role).To(Equal(present), "ClusterRole presence")
		Expect(binding).To(Equal(present), "ClusterRoleBinding presence")
	}

	newSweeper := func(watchedNamespaces ...string) *CrossNamespaceRBACSweeper {
		return NewCrossNamespaceRBACSweeper(fakeClient, fakeClient, watchedNamespaces)
	}

	BeforeEach(func() {
		ctx = context.Background()
		getErr = nil
		fakeClient = fake.NewClientBuilder().
			WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
			WithInterceptorFuncs(interceptor.Funcs{
				Get: func(
					ctx context.Context,
					cli client.WithWatch,
					key client.ObjectKey,
					obj client.Object,
					opts ...client.GetOption,
				) error {
					if _, ok := obj.(*apiv1.Cluster); ok && getErr != nil {
						return getErr
					}
					return cli.Get(ctx, key, obj, opts...)
				},
			}).
			Build()
	})

	It("removes the RBAC left by a cluster deleted while no reconcile happened", func() {
		cluster := createWithRBAC("postgres", "gone")
		Expect(fakeClient.Delete(ctx, cluster)).To(Succeed())

		// Nothing reconciles the deletion: the RBAC objects are left behind
		expectRBAC(cluster, true)

		Expect(newSweeper().Sweep(ctx)).To(Succeed())
		expectRBAC(cluster, false)
	})

	It("keeps the RBAC of an existing cluster", func() {
		kept := createWithRBAC("postgres", "kept")
		gone := createWithRBAC("other", "gone")
		Expect(fakeClient.Delete(ctx, gone)).To(Succeed())

		Expect(newSweeper().Sweep(ctx)).To(Succeed())
		expectRBAC(kept, true)
		expectRBAC(gone, false)
	})

	It("ignores the objects without the cross-namespace labels", func() {
		partial := map[string]string{
			utils.ClusterLabelName:                "gone",
			utils.KubernetesAppManagedByLabelName: utils.ManagerName,
		}
		foreign := map[string]string{
			utils.ClusterLabelName:          "gone",
			utils.ClusterNamespaceLabelName: "postgres",
		}
		objects := []client.Object{
			&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "unlabelled"}},
			&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "partial", Labels: partial}},
			&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "foreign", Labels: foreign}},
			&rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "partial", Labels: partial}},
			&rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "foreign", Labels: foreign}},
		}
		for _, obj := range objects {
			Expect(fakeClient.Create(ctx, obj)).To(Succeed())
		}

		Expect(newSweeper().Sweep(ctx)).To(Succeed())

		for _, obj := range objects {
			Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(obj), obj)).To(Succeed())
		}
	})

	It("does not delete anything when the cluster cannot be read", func() {
		cluster := createWithRBAC("postgres", "unknown")
		getErr = apierrs.NewServiceUnavailable("API server unavailable")

		Expect(newSweeper().Sweep(ctx)).ToNot(Succeed())

		getErr = nil
		expectRBAC(cluster, true)
	})

	It("does not delete anything when the cluster read is forbidden", func() {
		cluster := createWithRBAC("postgres", "forbidden")
		Expect(fakeClient.Delete(ctx, cluster)).To(Succeed())
		getErr = apierrs.NewForbidden(apiv1.SchemeGroupVersion.WithResource("clusters").GroupResource(),
			"forbidden", errors.New("not allowed"))

		Expect(newSweeper().Sweep(ctx)).ToNot(Succeed())

		getErr = nil
		expectRBAC(cluster, true)
	})

	It("only sweeps the clusters of the watched namespaces", func() {
		watched := createWithRBAC("watched", "gone")
		unwatched := createWithRBAC("unwatched", "gone")
		Expect(fakeClient.Delete(ctx, watched)).To(Succeed())
		Expect(fakeClient.Delete(ctx, unwatched)).To(Succeed())

		Expect(newSweeper("watched", "another").Sweep(ctx)).To(Succeed())
		expectRBAC(watched, false)
		expectRBAC(unwatched, true)
	})

	It("needs leader election", func() {
		Expect(newSweeper().NeedLeaderElection()).To(BeTrue())
	})
})

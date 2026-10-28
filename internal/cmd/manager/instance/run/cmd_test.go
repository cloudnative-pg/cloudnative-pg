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

package run

import (
	"context"
	"errors"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/concurrency"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("NewCmd", func() {
	// An in-place instance manager upgrade re-execs this binary with the argv of
	// the Pod, so a flag dropped in a newer release must still parse.
	It("accepts the status-port-tls flag passed by Pods created before 1.31", func() {
		Expect(NewCmd().ParseFlags([]string{"--status-port-tls"})).To(Succeed())
	})
})

const (
	watchScopeNamespace   = "postgres"
	watchScopeClusterName = "cluster-example"
)

func newWatchScopeCluster(crossNamespace bool) *apiv1.Cluster {
	return &apiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      watchScopeClusterName,
			Namespace: watchScopeNamespace,
		},
		Spec: apiv1.ClusterSpec{
			Instances:                     1,
			EnableCrossNamespaceResources: crossNamespace,
		},
	}
}

func newWatchScopeClient(objects ...client.Object) client.Client {
	return fake.NewClientBuilder().
		WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
		WithObjects(objects...).
		Build()
}

var _ = Describe("getManagedResourcesCacheConfig", func() {
	It("restricts the watch to the cluster namespace by default", func() {
		config := getManagedResourcesCacheConfig(false, watchScopeNamespace)
		Expect(config.Namespaces).To(HaveKey(watchScopeNamespace))
		Expect(config.Namespaces).To(HaveLen(1))
	})

	It("watches every namespace when cross-namespace databases are enabled", func() {
		Expect(getManagedResourcesCacheConfig(true, watchScopeNamespace)).To(Equal(cache.ByObject{}))
	})
})

var _ = Describe("isCrossNamespaceResourcesEnabled", func() {
	DescribeTable("reports the setting of the cluster",
		func(ctx SpecContext, enabled bool) {
			cli := newWatchScopeClient(newWatchScopeCluster(enabled))

			Expect(isCrossNamespaceResourcesEnabled(
				ctx, cli, watchScopeNamespace, watchScopeClusterName)).To(Equal(enabled))
		},
		Entry("when disabled", false),
		Entry("when enabled", true),
	)

	It("fails when the cluster cannot be read", func(ctx SpecContext) {
		_, err := isCrossNamespaceResourcesEnabled(
			ctx, newWatchScopeClient(), watchScopeNamespace, watchScopeClusterName)
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("watchScopeReconciler", func() {
	var (
		instance   *postgres.Instance
		cancelled  bool
		restarts   int
		restartErr error

		accessChecks  int
		accessAllowed bool
		accessErr     error
	)

	reconcilerFor := func(cluster *apiv1.Cluster, bootValue bool) *watchScopeReconciler {
		instance = postgres.NewInstance().
			WithNamespace(watchScopeNamespace).
			WithClusterName(watchScopeClusterName)
		cancelled = false
		restarts = 0

		objects := make([]client.Object, 0, 1)
		if cluster != nil {
			objects = append(objects, cluster)
		}

		return &watchScopeReconciler{
			client:         newWatchScopeClient(objects...),
			instance:       instance,
			crossNamespace: bootValue,
			cancelFunc:     func() { cancelled = true },
			restart: func(cancelFunc context.CancelFunc, _ concurrency.MultipleExecuted) error {
				restarts++
				cancelFunc()
				return restartErr
			},
			checkAccess: func(context.Context) (bool, string, error) {
				accessChecks++
				if accessAllowed {
					return true, "", accessErr
				}
				return false, "list databases.postgresql.cnpg.io in every namespace is forbidden", accessErr
			},
		}
	}

	request := ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: watchScopeNamespace, Name: watchScopeClusterName},
	}

	BeforeEach(func() {
		restartErr = nil
		accessChecks = 0
		accessAllowed = true
		accessErr = nil
	})

	DescribeTable("keeps the instance manager running when the setting did not change",
		func(ctx SpecContext, value bool) {
			reconciler := reconcilerFor(newWatchScopeCluster(value), value)

			result, err := reconciler.Reconcile(ctx, request)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(BeZero())
			Expect(restarts).To(BeZero())
			Expect(cancelled).To(BeFalse())
			Expect(instance.InstanceManagerIsUpgrading.Load()).To(BeFalse())
		},
		Entry("when disabled", false),
		Entry("when enabled", true),
	)

	DescribeTable("restarts the instance manager when the setting changed",
		func(ctx SpecContext, clusterValue bool) {
			reconciler := reconcilerFor(newWatchScopeCluster(clusterValue), !clusterValue)

			_, err := reconciler.Reconcile(ctx, request)
			Expect(err).ToNot(HaveOccurred())
			Expect(restarts).To(Equal(1))
			Expect(cancelled).To(BeTrue())

			// PostgreSQL must survive the cancellation of the context
			Expect(instance.InstanceManagerIsUpgrading.Load()).To(BeTrue())
		},
		Entry("when it got enabled", true),
		Entry("when it got disabled", false),
	)

	It("does nothing when the cluster is gone", func(ctx SpecContext) {
		reconciler := reconcilerFor(nil, false)

		result, err := reconciler.Reconcile(ctx, request)
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(BeZero())
		Expect(restarts).To(BeZero())
	})

	It("does not restart when an online upgrade is already running", func(ctx SpecContext) {
		reconciler := reconcilerFor(newWatchScopeCluster(true), false)
		instance.InstanceManagerIsUpgrading.Store(true)

		result, err := reconciler.Reconcile(ctx, request)
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(BeZero())
		Expect(restarts).To(BeZero())
		Expect(cancelled).To(BeFalse())
	})

	It("clears the upgrading flag when the restart fails", func(ctx SpecContext) {
		restartErr = errors.New("cannot exec")
		reconciler := reconcilerFor(newWatchScopeCluster(true), false)

		_, err := reconciler.Reconcile(ctx, request)
		Expect(err).To(MatchError(restartErr))
		Expect(instance.InstanceManagerIsUpgrading.Load()).To(BeFalse())
	})

	Context("when the cross-namespace resources support got enabled", func() {
		It("waits for the cluster-wide access before restarting", func(ctx SpecContext) {
			accessAllowed = false
			reconciler := reconcilerFor(newWatchScopeCluster(true), false)

			result, err := reconciler.Reconcile(ctx, request)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0))
			Expect(accessChecks).To(Equal(1))
			Expect(restarts).To(BeZero())
			Expect(cancelled).To(BeFalse())
			Expect(instance.InstanceManagerIsUpgrading.Load()).To(BeFalse())
		})

		It("restarts once the cluster-wide access is granted", func(ctx SpecContext) {
			reconciler := reconcilerFor(newWatchScopeCluster(true), false)

			_, err := reconciler.Reconcile(ctx, request)
			Expect(err).ToNot(HaveOccurred())
			Expect(accessChecks).To(Equal(1))
			Expect(restarts).To(Equal(1))
		})

		It("retries without restarting when the access cannot be checked", func(ctx SpecContext) {
			accessErr = errors.New("connection refused")
			reconciler := reconcilerFor(newWatchScopeCluster(true), false)

			_, err := reconciler.Reconcile(ctx, request)
			Expect(err).To(MatchError(accessErr))
			Expect(restarts).To(BeZero())
			Expect(cancelled).To(BeFalse())
			Expect(instance.InstanceManagerIsUpgrading.Load()).To(BeFalse())
		})
	})

	It("does not check the cluster-wide access when the support got disabled", func(ctx SpecContext) {
		accessAllowed = false
		reconciler := reconcilerFor(newWatchScopeCluster(false), true)

		_, err := reconciler.Reconcile(ctx, request)
		Expect(err).ToNot(HaveOccurred())
		Expect(accessChecks).To(BeZero())
		Expect(restarts).To(Equal(1))
	})
})

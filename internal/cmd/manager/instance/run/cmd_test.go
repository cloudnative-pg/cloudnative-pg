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
	"time"

	authorizationv1 "k8s.io/api/authorization/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	kubefake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

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
	watchScopePodName     = "cluster-example-1"
)

// newWatchScopeCluster returns a healthy cluster, whose instance managers may
// be restarted
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
		Status: apiv1.ClusterStatus{
			Phase:          apiv1.PhaseHealthy,
			CurrentPrimary: watchScopePodName,
			TargetPrimary:  watchScopePodName,
		},
	}
}

// newWatchScopeBackup returns a backup of the cluster in the passed phase,
// taken from the passed Pod
func newWatchScopeBackup(phase apiv1.BackupPhase, podName string) *apiv1.Backup {
	return &apiv1.Backup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "backup",
			Namespace: watchScopeNamespace,
		},
		Spec: apiv1.BackupSpec{
			Cluster: apiv1.LocalObjectReference{Name: watchScopeClusterName},
		},
		Status: apiv1.BackupStatus{
			Phase:      phase,
			InstanceID: &apiv1.InstanceID{PodName: podName},
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

var _ = Describe("detectCrossNamespaceResources", func() {
	// noDelay retries without waiting, to keep the tests fast
	noDelay := wait.Backoff{Steps: 3}

	// accessChecker answers the access checks as passed, and counts them
	accessChecker := func(allowed bool, err error) (clusterWideAccessChecker, *int) {
		checks := 0
		return func(context.Context) (bool, string, error) {
			checks++
			return allowed, "cannot list databases in every namespace", err
		}, &checks
	}
	granted, _ := accessChecker(true, nil)

	// failingClient fails the first reads of the cluster, and counts them all
	failingClient := func(failures int, cluster *apiv1.Cluster) (client.Client, *int) {
		reads := 0
		cli := fake.NewClientBuilder().
			WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
			WithObjects(cluster).
			WithInterceptorFuncs(interceptor.Funcs{
				Get: func(
					ctx context.Context, cli client.WithWatch, key client.ObjectKey, obj client.Object,
					opts ...client.GetOption,
				) error {
					reads++
					if reads <= failures {
						return apierrors.NewServiceUnavailable("the API server is unavailable")
					}
					return cli.Get(ctx, key, obj, opts...)
				},
			}).
			Build()
		return cli, &reads
	}

	It("retries reading the cluster after a transient failure", func(ctx SpecContext) {
		cli, reads := failingClient(2, newWatchScopeCluster(true))

		Expect(detectCrossNamespaceResources(
			ctx, cli, granted, watchScopeNamespace, watchScopeClusterName, noDelay)).To(BeTrue())
		Expect(*reads).To(Equal(3))
	})

	It("falls back to the namespace of the cluster when it cannot be read", func(ctx SpecContext) {
		cli, reads := failingClient(10, newWatchScopeCluster(true))

		Expect(detectCrossNamespaceResources(
			ctx, cli, granted, watchScopeNamespace, watchScopeClusterName, noDelay)).To(BeFalse())
		Expect(*reads).To(Equal(noDelay.Steps))
	})

	It("does not check the cluster-wide access when the cluster did not opt in", func(ctx SpecContext) {
		checkAccess, checks := accessChecker(true, nil)

		Expect(detectCrossNamespaceResources(
			ctx, newWatchScopeClient(newWatchScopeCluster(false)), checkAccess,
			watchScopeNamespace, watchScopeClusterName, noDelay)).To(BeFalse())
		Expect(*checks).To(BeZero())
	})

	It("falls back to the namespace of the cluster until the cluster-wide access is granted",
		func(ctx SpecContext) {
			checkAccess, checks := accessChecker(false, nil)

			Expect(detectCrossNamespaceResources(
				ctx, newWatchScopeClient(newWatchScopeCluster(true)), checkAccess,
				watchScopeNamespace, watchScopeClusterName, noDelay)).To(BeFalse())
			Expect(*checks).To(Equal(1))
		})

	It("falls back to the namespace of the cluster when the access cannot be checked", func(ctx SpecContext) {
		checkAccess, _ := accessChecker(true, errors.New("the API server is unavailable"))

		Expect(detectCrossNamespaceResources(
			ctx, newWatchScopeClient(newWatchScopeCluster(true)), checkAccess,
			watchScopeNamespace, watchScopeClusterName, noDelay)).To(BeFalse())
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

		clock time.Time
	)

	reconcilerFor := func(
		cluster *apiv1.Cluster,
		bootValue bool,
		backups ...client.Object,
	) *watchScopeReconciler {
		instance = postgres.NewInstance().
			WithNamespace(watchScopeNamespace).
			WithClusterName(watchScopeClusterName).
			WithPodName(watchScopePodName)
		cancelled = false
		restarts = 0

		objects := make([]client.Object, 0, 1+len(backups))
		if cluster != nil {
			objects = append(objects, cluster)
		}
		objects = append(objects, backups...)

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
			now: func() time.Time { return clock },
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
		clock = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
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

		It("checks the access less and less often while it is refused", func(ctx SpecContext) {
			accessAllowed = false
			reconciler := reconcilerFor(newWatchScopeCluster(true), false)

			delays := make([]time.Duration, 0, 8)
			for range 8 {
				result, err := reconciler.Reconcile(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				delays = append(delays, result.RequeueAfter)
				clock = clock.Add(result.RequeueAfter)
			}

			Expect(delays).To(Equal([]time.Duration{
				5 * time.Second,
				10 * time.Second,
				20 * time.Second,
				40 * time.Second,
				80 * time.Second,
				160 * time.Second,
				clusterWideAccessMaxRetryDelay,
				clusterWideAccessMaxRetryDelay,
			}))
			Expect(accessChecks).To(Equal(8))
			Expect(restarts).To(BeZero())
		})

		It("does not check the access again before the delay elapsed", func(ctx SpecContext) {
			accessAllowed = false
			reconciler := reconcilerFor(newWatchScopeCluster(true), false)

			first, err := reconciler.Reconcile(ctx, request)
			Expect(err).ToNot(HaveOccurred())

			// A change of the cluster triggers a reconciliation before the delay elapsed
			clock = clock.Add(2 * time.Second)
			second, err := reconciler.Reconcile(ctx, request)
			Expect(err).ToNot(HaveOccurred())

			Expect(accessChecks).To(Equal(1))
			Expect(second.RequeueAfter).To(Equal(first.RequeueAfter - 2*time.Second))
			Expect(restarts).To(BeZero())
		})

		It("restarts once the access refused before is granted", func(ctx SpecContext) {
			accessAllowed = false
			reconciler := reconcilerFor(newWatchScopeCluster(true), false)

			result, err := reconciler.Reconcile(ctx, request)
			Expect(err).ToNot(HaveOccurred())
			Expect(restarts).To(BeZero())

			accessAllowed = true
			clock = clock.Add(result.RequeueAfter)
			_, err = reconciler.Reconcile(ctx, request)
			Expect(err).ToNot(HaveOccurred())
			Expect(accessChecks).To(Equal(2))
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

	Context("holding back the restart", func() {
		expectHeldBack := func(ctx SpecContext, reconciler *watchScopeReconciler) {
			result, err := reconciler.Reconcile(ctx, request)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(restartHeldBackRetryDelay))
			Expect(restarts).To(BeZero())
			Expect(cancelled).To(BeFalse())
			Expect(accessChecks).To(BeZero())
			Expect(instance.InstanceManagerIsUpgrading.Load()).To(BeFalse())
		}

		DescribeTable("waits while the cluster is not healthy",
			func(ctx SpecContext, phase string) {
				cluster := newWatchScopeCluster(true)
				cluster.Status.Phase = phase

				expectHeldBack(ctx, reconcilerFor(cluster, false))
			},
			Entry("during a switchover", apiv1.PhaseSwitchover),
			Entry("during a failover", apiv1.PhaseFailOver),
			Entry("during a rolling update", apiv1.PhaseUpgrade),
			Entry("during an online upgrade", apiv1.PhaseOnlineUpgrading),
			Entry("before the phase is set", ""),
		)

		It("waits while a new primary is being promoted", func(ctx SpecContext) {
			cluster := newWatchScopeCluster(true)
			cluster.Status.TargetPrimary = "cluster-example-2"

			expectHeldBack(ctx, reconcilerFor(cluster, false))
		})

		DescribeTable("waits while a backup is taken from this instance",
			func(ctx SpecContext, phase string) {
				backup := newWatchScopeBackup(apiv1.BackupPhase(phase), watchScopePodName)

				expectHeldBack(ctx, reconcilerFor(newWatchScopeCluster(true), false, backup))
			},
			Entry("when it started", apiv1.BackupPhaseStarted),
			Entry("when it is running", apiv1.BackupPhaseRunning),
		)

		DescribeTable("restarts when no backup is taken from this instance",
			func(ctx SpecContext, backup *apiv1.Backup) {
				reconciler := reconcilerFor(newWatchScopeCluster(true), false, backup)

				_, err := reconciler.Reconcile(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				Expect(restarts).To(Equal(1))
			},
			Entry("when the backup is taken from another instance",
				newWatchScopeBackup(apiv1.BackupPhaseRunning, "cluster-example-2")),
			Entry("when the backup is completed",
				newWatchScopeBackup(apiv1.BackupPhaseCompleted, watchScopePodName)),
			Entry("when the backup belongs to another cluster", func() *apiv1.Backup {
				backup := newWatchScopeBackup(apiv1.BackupPhaseRunning, watchScopePodName)
				backup.Spec.Cluster.Name = "another-cluster"
				return backup
			}()),
		)

		It("fails without restarting when the backups cannot be listed", func(ctx SpecContext) {
			reconciler := reconcilerFor(newWatchScopeCluster(true), false)
			listErr := errors.New("connection refused")
			reconciler.client = interceptor.NewClient(reconciler.client.(client.WithWatch), interceptor.Funcs{
				List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
					return listErr
				},
			})

			_, err := reconciler.Reconcile(ctx, request)
			Expect(err).To(MatchError(listErr))
			Expect(restarts).To(BeZero())
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

var _ = Describe("newClusterWideAccessChecker", func() {
	var (
		kubeClient *kubefake.Clientset
		reviews    []authorizationv1.ResourceAttributes
		denied     string
		reviewErr  error
	)

	BeforeEach(func() {
		reviews = nil
		denied = ""
		reviewErr = nil
		kubeClient = kubefake.NewClientset()
		kubeClient.PrependReactor("create", "selfsubjectaccessreviews",
			func(action k8stesting.Action) (bool, runtime.Object, error) {
				review := action.(k8stesting.CreateAction).GetObject().(*authorizationv1.SelfSubjectAccessReview)
				reviews = append(reviews, *review.Spec.ResourceAttributes)
				if reviewErr != nil {
					return true, nil, reviewErr
				}
				if review.Spec.ResourceAttributes.Resource == denied {
					review.Status.Reason = "no RBAC policy matched"
					return true, review, nil
				}
				review.Status.Allowed = true
				return true, review, nil
			})
	})

	It("allows the restart when every resource can be listed and watched everywhere", func(ctx SpecContext) {
		allowed, reason, err := newClusterWideAccessChecker(kubeClient)(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(allowed).To(BeTrue())
		Expect(reason).To(BeEmpty())

		expected := make([]authorizationv1.ResourceAttributes, 0, 8)
		for _, resource := range []string{"databases", "databaseroles", "publications", "subscriptions"} {
			for _, verb := range []string{"list", "watch"} {
				expected = append(expected, authorizationv1.ResourceAttributes{
					Namespace: "",
					Verb:      verb,
					Group:     "postgresql.cnpg.io",
					Resource:  resource,
				})
			}
		}
		Expect(reviews).To(Equal(expected))
	})

	It("reports the first missing permission", func(ctx SpecContext) {
		denied = "publications"

		allowed, reason, err := newClusterWideAccessChecker(kubeClient)(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(allowed).To(BeFalse())
		Expect(reason).To(Equal(
			"cannot list publications.postgresql.cnpg.io cluster-wide: no RBAC policy matched"))
		Expect(reviews).To(HaveLen(5))
	})

	It("fails when the review cannot be created", func(ctx SpecContext) {
		reviewErr = errors.New("connection refused")

		allowed, _, err := newClusterWideAccessChecker(kubeClient)(ctx)
		Expect(err).To(MatchError(ContainSubstring("connection refused")))
		Expect(allowed).To(BeFalse())
	})
})

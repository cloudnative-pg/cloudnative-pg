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

package client

import (
	"context"
	"errors"

	"github.com/cloudnative-pg/cnpg-i/pkg/reconciler"
	"google.golang.org/grpc"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8client "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/connection"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type fakeReconcilerHooksClient struct {
	behavior     reconciler.ReconcilerHooksResult_Behavior
	requeueAfter int64
	err          error
	capabilities []reconciler.ReconcilerHooksCapability_Kind
	callCount    int
}

func newFakeReconcilerHooksClient(
	capabilities []reconciler.ReconcilerHooksCapability_Kind,
	behavior reconciler.ReconcilerHooksResult_Behavior,
	requeueAfter int64,
	err error,
) *fakeReconcilerHooksClient {
	return &fakeReconcilerHooksClient{
		capabilities: capabilities,
		behavior:     behavior,
		requeueAfter: requeueAfter,
		err:          err,
	}
}

func (f *fakeReconcilerHooksClient) GetCapabilities(
	_ context.Context,
	_ *reconciler.ReconcilerHooksCapabilitiesRequest,
	_ ...grpc.CallOption,
) (*reconciler.ReconcilerHooksCapabilitiesResult, error) {
	return &reconciler.ReconcilerHooksCapabilitiesResult{}, nil
}

func (f *fakeReconcilerHooksClient) Pre(
	_ context.Context,
	_ *reconciler.ReconcilerHooksRequest,
	_ ...grpc.CallOption,
) (*reconciler.ReconcilerHooksResult, error) {
	f.callCount++
	if f.err != nil {
		return nil, f.err
	}
	return &reconciler.ReconcilerHooksResult{
		Behavior:     f.behavior,
		RequeueAfter: f.requeueAfter,
	}, nil
}

func (f *fakeReconcilerHooksClient) Post(
	_ context.Context,
	_ *reconciler.ReconcilerHooksRequest,
	_ ...grpc.CallOption,
) (*reconciler.ReconcilerHooksResult, error) {
	f.callCount++
	if f.err != nil {
		return nil, f.err
	}
	return &reconciler.ReconcilerHooksResult{
		Behavior:     f.behavior,
		RequeueAfter: f.requeueAfter,
	}, nil
}

func (f *fakeReconcilerHooksClient) getCallCount() int {
	return f.callCount
}

func (f *fakeReconcilerHooksClient) set(d *fakeConnection) {
	if d == nil {
		return
	}

	d.reconcilerHooksClient = f
	d.reconcilerCapabilities = f.capabilities
}

var _ = Describe("reconcilerHook", func() {
	var (
		ctx           context.Context
		plugins       []connection.Interface
		cluster       k8client.Object
		executePreHook = func(
			ctx context.Context,
			plugin reconciler.ReconcilerHooksClient,
			request *reconciler.ReconcilerHooksRequest,
		) (*reconciler.ReconcilerHooksResult, error) {
			return plugin.Pre(ctx, request)
		}
	)

	BeforeEach(func() {
		ctx = context.Background()
		plugins = []connection.Interface{
			&fakeConnection{
				name: "test",
			},
		}

		cluster = &apiv1.Cluster{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "postgresql.cnpg.io/v1",
				Kind:       "Cluster",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "default",
			},
		}
	})

	It("should continue when a plugin returns CONTINUE behavior", func() {
		f := newFakeReconcilerHooksClient(
			[]reconciler.ReconcilerHooksCapability_Kind{reconciler.ReconcilerHooksCapability_KIND_CLUSTER},
			reconciler.ReconcilerHooksResult_BEHAVIOR_CONTINUE,
			0,
			nil,
		)
		f.set(plugins[0].(*fakeConnection))

		result := reconcilerHook(ctx, cluster, cluster, plugins, executePreHook)
		Expect(result.StopReconciliation).To(BeFalse())
		Expect(result.Err).ToNot(HaveOccurred())
		Expect(result.Identifier).To(Equal(cnpgOperatorKey))
	})

	It("should stop when a plugin returns TERMINATE behavior", func() {
		f := newFakeReconcilerHooksClient(
			[]reconciler.ReconcilerHooksCapability_Kind{reconciler.ReconcilerHooksCapability_KIND_CLUSTER},
			reconciler.ReconcilerHooksResult_BEHAVIOR_TERMINATE,
			0,
			nil,
		)
		f.set(plugins[0].(*fakeConnection))

		result := reconcilerHook(ctx, cluster, cluster, plugins, executePreHook)
		Expect(result.StopReconciliation).To(BeTrue())
		Expect(result.Err).ToNot(HaveOccurred())
		Expect(result.Identifier).To(Equal("test"))
	})

	It("should requeue when a plugin returns REQUEUE behavior", func() {
		f := newFakeReconcilerHooksClient(
			[]reconciler.ReconcilerHooksCapability_Kind{reconciler.ReconcilerHooksCapability_KIND_CLUSTER},
			reconciler.ReconcilerHooksResult_BEHAVIOR_REQUEUE,
			10,
			nil,
		)
		f.set(plugins[0].(*fakeConnection))

		result := reconcilerHook(ctx, cluster, cluster, plugins, executePreHook)
		Expect(result.StopReconciliation).To(BeTrue())
		Expect(result.Result.RequeueAfter.Seconds()).To(BeEquivalentTo(10))
		Expect(result.Err).ToNot(HaveOccurred())
		Expect(result.Identifier).To(Equal("test"))
	})

	It("should return error when plugin execution fails", func() {
		expectedErr := errors.New("plugin execution failed")
		f := newFakeReconcilerHooksClient(
			[]reconciler.ReconcilerHooksCapability_Kind{reconciler.ReconcilerHooksCapability_KIND_CLUSTER},
			reconciler.ReconcilerHooksResult_BEHAVIOR_CONTINUE,
			0,
			expectedErr,
		)
		f.set(plugins[0].(*fakeConnection))

		result := reconcilerHook(ctx, cluster, cluster, plugins, executePreHook)
		Expect(result.StopReconciliation).To(BeTrue())
		Expect(result.Err).To(HaveOccurred())
		Expect(result.Identifier).To(Equal("test"))
	})

	It("should process multiple plugins when all return CONTINUE", func() {
		plugins = []connection.Interface{
			&fakeConnection{name: "plugin-1"},
			&fakeConnection{name: "plugin-2"},
			&fakeConnection{name: "plugin-3"},
		}

		f1 := newFakeReconcilerHooksClient(
			[]reconciler.ReconcilerHooksCapability_Kind{reconciler.ReconcilerHooksCapability_KIND_CLUSTER},
			reconciler.ReconcilerHooksResult_BEHAVIOR_CONTINUE,
			0,
			nil,
		)
		f1.set(plugins[0].(*fakeConnection))

		f2 := newFakeReconcilerHooksClient(
			[]reconciler.ReconcilerHooksCapability_Kind{reconciler.ReconcilerHooksCapability_KIND_CLUSTER},
			reconciler.ReconcilerHooksResult_BEHAVIOR_CONTINUE,
			0,
			nil,
		)
		f2.set(plugins[1].(*fakeConnection))

		f3 := newFakeReconcilerHooksClient(
			[]reconciler.ReconcilerHooksCapability_Kind{reconciler.ReconcilerHooksCapability_KIND_CLUSTER},
			reconciler.ReconcilerHooksResult_BEHAVIOR_CONTINUE,
			0,
			nil,
		)
		f3.set(plugins[2].(*fakeConnection))

		result := reconcilerHook(ctx, cluster, cluster, plugins, executePreHook)
		Expect(result.StopReconciliation).To(BeFalse())
		Expect(result.Err).ToNot(HaveOccurred())
		Expect(result.Identifier).To(Equal(cnpgOperatorKey))
		Expect(f1.getCallCount()).To(Equal(1), "plugin-1 should be called once")
		Expect(f2.getCallCount()).To(Equal(1), "plugin-2 should be called once")
		Expect(f3.getCallCount()).To(Equal(1), "plugin-3 should be called once")
	})

	It("should stop at second plugin when it returns TERMINATE after first returns CONTINUE", func() {
		plugins = []connection.Interface{
			&fakeConnection{name: "plugin-1"},
			&fakeConnection{name: "plugin-2"},
			&fakeConnection{name: "plugin-3"},
		}

		f1 := newFakeReconcilerHooksClient(
			[]reconciler.ReconcilerHooksCapability_Kind{reconciler.ReconcilerHooksCapability_KIND_CLUSTER},
			reconciler.ReconcilerHooksResult_BEHAVIOR_CONTINUE,
			0,
			nil,
		)
		f1.set(plugins[0].(*fakeConnection))

		f2 := newFakeReconcilerHooksClient(
			[]reconciler.ReconcilerHooksCapability_Kind{reconciler.ReconcilerHooksCapability_KIND_CLUSTER},
			reconciler.ReconcilerHooksResult_BEHAVIOR_TERMINATE,
			0,
			nil,
		)
		f2.set(plugins[1].(*fakeConnection))

		f3 := newFakeReconcilerHooksClient(
			[]reconciler.ReconcilerHooksCapability_Kind{reconciler.ReconcilerHooksCapability_KIND_CLUSTER},
			reconciler.ReconcilerHooksResult_BEHAVIOR_CONTINUE,
			0,
			nil,
		)
		f3.set(plugins[2].(*fakeConnection))

		result := reconcilerHook(ctx, cluster, cluster, plugins, executePreHook)
		Expect(result.StopReconciliation).To(BeTrue())
		Expect(result.Err).ToNot(HaveOccurred())
		Expect(result.Identifier).To(Equal("plugin-2"))
		Expect(f1.getCallCount()).To(Equal(1), "plugin-1 should be called once")
		Expect(f2.getCallCount()).To(Equal(1), "plugin-2 should be called once")
		Expect(f3.getCallCount()).To(Equal(0), "plugin-3 should not be called")
	})

	It("should skip plugins without the required capability", func() {
		plugins = []connection.Interface{
			&fakeConnection{name: "plugin-without-capability"},
			&fakeConnection{name: "plugin-with-capability"},
		}

		f1 := newFakeReconcilerHooksClient(
			[]reconciler.ReconcilerHooksCapability_Kind{reconciler.ReconcilerHooksCapability_KIND_BACKUP},
			reconciler.ReconcilerHooksResult_BEHAVIOR_TERMINATE,
			0,
			nil,
		)
		f1.set(plugins[0].(*fakeConnection))

		f2 := newFakeReconcilerHooksClient(
			[]reconciler.ReconcilerHooksCapability_Kind{reconciler.ReconcilerHooksCapability_KIND_CLUSTER},
			reconciler.ReconcilerHooksResult_BEHAVIOR_CONTINUE,
			0,
			nil,
		)
		f2.set(plugins[1].(*fakeConnection))

		result := reconcilerHook(ctx, cluster, cluster, plugins, executePreHook)
		Expect(result.StopReconciliation).To(BeFalse())
		Expect(result.Err).ToNot(HaveOccurred())
		Expect(result.Identifier).To(Equal(cnpgOperatorKey))
		Expect(f1.getCallCount()).To(Equal(0), "plugin without capability should not be called")
		Expect(f2.getCallCount()).To(Equal(1), "plugin with capability should be called once")
	})

	It("should handle Backup objects", func() {
		backup := &apiv1.Backup{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "postgresql.cnpg.io/v1",
				Kind:       "Backup",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-backup",
				Namespace: "default",
			},
		}

		f := newFakeReconcilerHooksClient(
			[]reconciler.ReconcilerHooksCapability_Kind{reconciler.ReconcilerHooksCapability_KIND_BACKUP},
			reconciler.ReconcilerHooksResult_BEHAVIOR_CONTINUE,
			0,
			nil,
		)
		f.set(plugins[0].(*fakeConnection))

		result := reconcilerHook(ctx, cluster, backup, plugins, executePreHook)
		Expect(result.StopReconciliation).To(BeFalse())
		Expect(result.Err).ToNot(HaveOccurred())
		Expect(result.Identifier).To(Equal(cnpgOperatorKey))
	})

	It("should skip unknown object kinds", func() {
		pod := &corev1.Pod{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "Pod",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pod",
				Namespace: "default",
			},
		}

		f := newFakeReconcilerHooksClient(
			[]reconciler.ReconcilerHooksCapability_Kind{reconciler.ReconcilerHooksCapability_KIND_CLUSTER},
			reconciler.ReconcilerHooksResult_BEHAVIOR_TERMINATE,
			0,
			nil,
		)
		f.set(plugins[0].(*fakeConnection))

		result := reconcilerHook(ctx, cluster, pod, plugins, executePreHook)
		Expect(result.StopReconciliation).To(BeFalse())
		Expect(result.Err).ToNot(HaveOccurred())
		Expect(result.Identifier).To(Equal(cnpgOperatorKey))
	})
})

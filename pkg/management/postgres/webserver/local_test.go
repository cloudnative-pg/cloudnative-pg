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

package webserver

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("requestBackup", func() {
	const (
		namespace   = "test"
		clusterName = "test-cluster"
		backupName  = "test-backup"
	)

	var (
		cluster  *apiv1.Cluster
		instance *postgres.Instance
	)

	// newEndpoints builds a localWebserverEndpoints backed by a fake client that
	// counts every status patch issued against the Backup object, so tests can
	// assert on whether a backup was actually started, not just on the HTTP
	// response code. The counter is local to this call: a leftover goroutine
	// from a previous test's async plugin path must not be able to bump a
	// later test's counter.
	newEndpoints := func(backup *apiv1.Backup, patchErr error) (*localWebserverEndpoints, *atomic.Int32) {
		patchCounter := &atomic.Int32{}

		cli := fake.NewClientBuilder().
			WithScheme(scheme.BuildWithAllKnownScheme()).
			WithObjects(cluster, backup).
			WithStatusSubresource(cluster, backup).
			WithInterceptorFuncs(interceptor.Funcs{
				SubResourcePatch: func(
					ctx context.Context, cli client.Client, subResourceName string, obj client.Object,
					patch client.Patch, opts ...client.SubResourcePatchOption,
				) error {
					if _, ok := obj.(*apiv1.Backup); ok {
						patchCounter.Add(1)
					}
					if patchErr != nil {
						return patchErr
					}
					return cli.SubResource(subResourceName).Patch(ctx, obj, patch, opts...)
				},
			}).
			Build()

		return &localWebserverEndpoints{
			typedClient:   cli,
			instance:      instance,
			eventRecorder: &record.FakeRecorder{},
		}, patchCounter
	}

	doRequest := func(ws *localWebserverEndpoints) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/backup?name="+backupName, nil)
		w := httptest.NewRecorder()
		ws.requestBackup(w, req)
		return w
	}

	BeforeEach(func() {
		instance = postgres.NewInstance().WithNamespace(namespace).WithClusterName(clusterName)

		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: namespace},
			Spec: apiv1.ClusterSpec{
				Backup: &apiv1.BackupConfiguration{
					BarmanObjectStore: &apiv1.BarmanObjectStoreConfiguration{
						DestinationPath: "s3://bucket/path",
					},
				},
			},
		}
	})

	DescribeTable("does not start a second backup when the phase is already running",
		func(method apiv1.BackupMethod) {
			backup := &apiv1.Backup{
				ObjectMeta: metav1.ObjectMeta{Name: backupName, Namespace: namespace},
				Spec: apiv1.BackupSpec{
					Cluster: apiv1.LocalObjectReference{Name: clusterName},
					Method:  method,
					PluginConfiguration: &apiv1.BackupPluginConfiguration{
						Name: "test-plugin",
					},
				},
				Status: apiv1.BackupStatus{
					Phase: apiv1.BackupPhaseRunning,
				},
			}

			ws, patchCounter := newEndpoints(backup, nil)
			w := doRequest(ws)

			body, _ := io.ReadAll(w.Result().Body)
			Expect(w.Code).To(Equal(http.StatusOK))
			Expect(string(body)).To(Equal("OK"))
			Expect(patchCounter.Load()).To(Equal(int32(0)), "no backup status patch should have happened")
		},
		Entry("barman object store method", apiv1.BackupMethodBarmanObjectStore),
		Entry("plugin method", apiv1.BackupMethodPlugin),
	)

	It("starts a plugin backup normally when the phase is not running", func() {
		backup := &apiv1.Backup{
			ObjectMeta: metav1.ObjectMeta{Name: backupName, Namespace: namespace},
			Spec: apiv1.BackupSpec{
				Cluster: apiv1.LocalObjectReference{Name: clusterName},
				Method:  apiv1.BackupMethodPlugin,
				PluginConfiguration: &apiv1.BackupPluginConfiguration{
					Name: "test-plugin",
				},
			},
			Status: apiv1.BackupStatus{
				Phase: apiv1.BackupPhasePending,
			},
		}

		ws, patchCounter := newEndpoints(backup, nil)
		w := doRequest(ws)

		body, _ := io.ReadAll(w.Result().Body)
		Expect(w.Code).To(Equal(http.StatusOK))
		Expect(string(body)).To(Equal("OK"))
		Expect(patchCounter.Load()).To(BeNumerically(">=", 1), "a backup status patch should have happened")
	})

	// The barman object store method, once past the guard, drives a real
	// ensureWalArchiveIsWorking check against PostgreSQL, which this suite
	// cannot fake without a running instance. So this case is proven instead
	// by making the barman configuration invalid: if the guard let the request
	// through (as it must, since the phase is not running), the switch
	// statement reaches the configuration check and answers 409, instead of
	// the 200 an over-broad guard would have produced.
	It("lets a non-running barman backup reach the method dispatch instead of the guard", func() {
		cluster.Spec.Backup = nil

		backup := &apiv1.Backup{
			ObjectMeta: metav1.ObjectMeta{Name: backupName, Namespace: namespace},
			Spec: apiv1.BackupSpec{
				Cluster: apiv1.LocalObjectReference{Name: clusterName},
				Method:  apiv1.BackupMethodBarmanObjectStore,
			},
			Status: apiv1.BackupStatus{
				Phase: apiv1.BackupPhasePending,
			},
		}

		ws, patchCounter := newEndpoints(backup, nil)
		w := doRequest(ws)

		body, _ := io.ReadAll(w.Result().Body)
		Expect(w.Code).To(Equal(http.StatusConflict))
		Expect(string(body)).To(ContainSubstring("Barman backup not configured"))
		Expect(patchCounter.Load()).To(Equal(int32(0)))
	})

	It("answers 500 rather than OK when the plugin start fails synchronously", func() {
		backup := &apiv1.Backup{
			ObjectMeta: metav1.ObjectMeta{Name: backupName, Namespace: namespace},
			Spec: apiv1.BackupSpec{
				Cluster: apiv1.LocalObjectReference{Name: clusterName},
				Method:  apiv1.BackupMethodPlugin,
				PluginConfiguration: &apiv1.BackupPluginConfiguration{
					Name: "test-plugin",
				},
			},
			Status: apiv1.BackupStatus{
				Phase: apiv1.BackupPhasePending,
			},
		}

		ws, _ := newEndpoints(backup, errors.New("injected patch failure"))
		w := doRequest(ws)

		Expect(w.Code).To(Equal(http.StatusInternalServerError))
	})
})

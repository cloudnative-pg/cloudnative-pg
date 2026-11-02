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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/webserver/metricserver"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("reconcileMonitoringQueries", func() {
	const (
		namespace   = "default"
		clusterName = "cluster-example"
		configMap   = "custom-queries"
		key         = "queries"
		queries     = `pg_example:
  query: "SELECT 1 AS one"
  metrics:
    - one:
        usage: "GAUGE"
        description: "Always one"
`
	)

	var (
		cluster    *apiv1.Cluster
		fakeClient client.Client
		reconciler *InstanceReconciler
	)

	BeforeEach(func() {
		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: clusterName},
			Spec: apiv1.ClusterSpec{
				Monitoring: &apiv1.MonitoringConfiguration{
					CustomQueriesConfigMap: []apiv1.ConfigMapKeySelector{
						{LocalObjectReference: apiv1.LocalObjectReference{Name: configMap}, Key: key},
					},
				},
			},
		}
		queriesConfigMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: configMap},
			Data:       map[string]string{key: queries},
		}

		fakeClient = fake.NewClientBuilder().
			WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
			WithObjects(cluster, queriesConfigMap).
			Build()

		pgInstance := postgres.NewInstance().
			WithNamespace(namespace).
			WithPodName(clusterName + "-1").
			WithClusterName(clusterName)

		reconciler = &InstanceReconciler{
			client:                fakeClient,
			instance:              pgInstance,
			metricsServerExporter: metricserver.NewExporter(pgInstance, nil),
		}
	})

	It("keeps the current collector when only the Cluster status changes", func(ctx SpecContext) {
		reconciler.reconcileMonitoringQueries(ctx, cluster)
		built := reconciler.monitoringQueriesInputs
		Expect(built).ToNot(BeNil())
		Expect(built.sources).To(HaveLen(1))

		cluster.Status.Phase = apiv1.PhaseWaitingForUser
		cluster.Status.PhaseReason = "User must issue a supervised switchover"
		reconciler.reconcileMonitoringQueries(ctx, cluster)

		Expect(reconciler.monitoringQueriesInputs).To(BeIdenticalTo(built))
	})

	It("replaces the collector when the queries change", func(ctx SpecContext) {
		reconciler.reconcileMonitoringQueries(ctx, cluster)
		built := reconciler.monitoringQueriesInputs

		var queriesConfigMap corev1.ConfigMap
		Expect(fakeClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: configMap}, &queriesConfigMap)).
			To(Succeed())
		queriesConfigMap.Data[key] = queries + "  target_databases: [\"*\"]\n"
		Expect(fakeClient.Update(ctx, &queriesConfigMap)).To(Succeed())

		reconciler.reconcileMonitoringQueries(ctx, cluster)

		Expect(reconciler.monitoringQueriesInputs).ToNot(BeIdenticalTo(built))
		Expect(string(reconciler.monitoringQueriesInputs.sources[0].data)).
			To(Equal(queriesConfigMap.Data[key]))
	})

	It("replaces the collector when the application database changes", func(ctx SpecContext) {
		reconciler.reconcileMonitoringQueries(ctx, cluster)
		built := reconciler.monitoringQueriesInputs
		Expect(built.dbname).To(Equal("postgres"))

		cluster.Spec.Bootstrap = &apiv1.BootstrapConfiguration{
			InitDB: &apiv1.BootstrapInitDB{Database: "app"},
		}
		reconciler.reconcileMonitoringQueries(ctx, cluster)

		Expect(reconciler.monitoringQueriesInputs).ToNot(BeIdenticalTo(built))
		Expect(reconciler.monitoringQueriesInputs.dbname).To(Equal("app"))
	})

	It("replaces the collector when a ConfigMap with queries is removed", func(ctx SpecContext) {
		// This is what disabling the default queries does: the operator stops
		// providing the ConfigMap the Cluster refers to
		reconciler.reconcileMonitoringQueries(ctx, cluster)
		built := reconciler.monitoringQueriesInputs
		Expect(built.sources).To(HaveLen(1))

		Expect(fakeClient.Delete(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: configMap},
		})).To(Succeed())
		reconciler.reconcileMonitoringQueries(ctx, cluster)

		Expect(reconciler.monitoringQueriesInputs).ToNot(BeIdenticalTo(built))
		Expect(reconciler.monitoringQueriesInputs.sources).To(BeEmpty())
	})

	It("replaces the collector when a Secret with queries changes", func(ctx SpecContext) {
		const secretName = "custom-queries-secret"
		queriesSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: secretName},
			Data:       map[string][]byte{key: []byte(queries)},
		}
		Expect(fakeClient.Create(ctx, queriesSecret)).To(Succeed())
		cluster.Spec.Monitoring.CustomQueriesSecret = []apiv1.SecretKeySelector{
			{LocalObjectReference: apiv1.LocalObjectReference{Name: secretName}, Key: key},
		}

		reconciler.reconcileMonitoringQueries(ctx, cluster)
		built := reconciler.monitoringQueriesInputs
		Expect(built.sources).To(HaveLen(2))

		reconciler.reconcileMonitoringQueries(ctx, cluster)
		Expect(reconciler.monitoringQueriesInputs).To(BeIdenticalTo(built))

		queriesSecret.Data[key] = []byte(queries + "  target_databases: [\"*\"]\n")
		Expect(fakeClient.Update(ctx, queriesSecret)).To(Succeed())
		reconciler.reconcileMonitoringQueries(ctx, cluster)

		Expect(reconciler.monitoringQueriesInputs).ToNot(BeIdenticalTo(built))
		Expect(reconciler.monitoringQueriesInputs.sources[1].data).To(Equal(queriesSecret.Data[key]))
	})
})

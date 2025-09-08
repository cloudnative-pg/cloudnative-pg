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

package metricserver

import (
	"context"
	"fmt"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/prometheus/client_golang/prometheus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/cache"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	postgresconf "github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type fakePluginCollector struct{}

func (f fakePluginCollector) Collect(context.Context, chan<- prometheus.Metric, *apiv1.Cluster) error {
	return nil
}

func (f fakePluginCollector) Describe(context.Context, chan<- *prometheus.Desc, *apiv1.Cluster) {
}

var _ = Describe("test metrics parsing", func() {
	var exporter *Exporter

	BeforeEach(func() {
		cache.Delete(cache.ClusterKey)
		instance := postgres.NewInstance()
		exporter = NewExporter(instance, fakePluginCollector{})
	})

	It("fails if there's no cluster in the cache", func() {
		exporter.collectFirstPointOnTimeRecovery()
		exporter.collectLastAvailableBackupTimestamp()
		exporter.collectLastFailedBackupTimestamp()

		registry := prometheus.NewRegistry()
		registry.MustRegister(exporter.Metrics.FirstRecoverabilityPoint)
		registry.MustRegister(exporter.Metrics.LastAvailableBackupTimestamp)
		registry.MustRegister(exporter.Metrics.LastFailedBackupTimestamp)

		metrics, _ := registry.Gather()
		for _, metric := range metrics {
			m := metric.GetMetric()
			Expect(m[0].GetGauge().GetValue()).To(BeEquivalentTo(0))
		}
	})

	It("should set the proper time from the cluster to the metric", func() {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "cluster-example",
			},
			Status: apiv1.ClusterStatus{
				FirstRecoverabilityPoint: "2023-02-16T22:44:56Z",
				LastSuccessfulBackup:     "2023-02-16T22:44:56Z",
				LastFailedBackup:         "2023-02-16T22:44:56Z",
			},
		}

		exporter.getCluster = func() (*apiv1.Cluster, error) {
			return cluster, nil
		}
		exporter.collectFirstPointOnTimeRecovery()
		exporter.collectLastAvailableBackupTimestamp()
		exporter.collectLastFailedBackupTimestamp()

		registry := prometheus.NewRegistry()
		registry.MustRegister(exporter.Metrics.FirstRecoverabilityPoint)
		registry.MustRegister(exporter.Metrics.LastAvailableBackupTimestamp)
		registry.MustRegister(exporter.Metrics.LastFailedBackupTimestamp)

		metrics, _ := registry.Gather()
		for _, metric := range metrics {
			m := metric.GetMetric()
			t := time.Unix(int64(m[0].GetGauge().GetValue()), 0).UTC()
			Expect(t.Year()).To(BeEquivalentTo(2023))
			Expect(t.Month()).To(BeEquivalentTo(2))
			Expect(t.Day()).To(BeEquivalentTo(16))
			Expect(t.Hour()).To(BeEquivalentTo(22))
			Expect(t.Minute()).To(BeEquivalentTo(44))
			Expect(t.Second()).To(BeEquivalentTo(56))

		}
	})

	It("correctly parses the number of sync replicas when quorum-based", func() {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())

		rows := sqlmock.NewRows([]string{"synchronous_standby_names"}).
			AddRow(`ANY 2 ( "cluster-example-2","cluster-example-3")`)
		mock.ExpectQuery(fmt.Sprintf("SHOW %s", postgresconf.SynchronousStandbyNames)).WillReturnRows(rows)

		exporter.collectFromPrimarySynchronousStandbysNumber(db)

		registry := prometheus.NewRegistry()
		registry.MustRegister(exporter.Metrics.SyncReplicas)
		metrics, _ := registry.Gather()

		for _, metric := range metrics {
			m := metric.GetMetric()
			Expect(m[0].GetGauge().GetValue()).To(BeEquivalentTo(2))
		}
	})

	It("correctly parses the number of sync replicas when preferential", func() {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())

		rows := sqlmock.NewRows([]string{"synchronous_standby_names"}).
			AddRow(`FIRST 2 ( "cluster-example-2","cluster-example-3")`)
		mock.ExpectQuery(fmt.Sprintf("SHOW %s", postgresconf.SynchronousStandbyNames)).WillReturnRows(rows)

		exporter.collectFromPrimarySynchronousStandbysNumber(db)

		registry := prometheus.NewRegistry()
		registry.MustRegister(exporter.Metrics.SyncReplicas)
		metrics, _ := registry.Gather()

		for _, metric := range metrics {
			m := metric.GetMetric()
			Expect(m[0].GetGauge().GetValue()).To(BeEquivalentTo(2))
		}
	})

	It("should return an error when encountering unexpected results", func() {
		By("not matching the synchronous standby names regex", func() {
			db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
			Expect(err).ToNot(HaveOccurred())

			// This row will generate only two strings in the array
			rows := sqlmock.NewRows([]string{"synchronous_standby_names"}).AddRow("ANY q (xx)")
			mock.ExpectQuery(fmt.Sprintf("SHOW %s", postgresconf.SynchronousStandbyNames)).WillReturnRows(rows)
			_, err = getRequestedSynchronousStandbysNumber(db)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not matching synchronous standby names regex: ANY q (xx)"))
		})

		By("not matching the number of sync replicas", func() {
			db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
			Expect(err).ToNot(HaveOccurred())

			// This row will generate only two strings in the array
			rows := sqlmock.NewRows([]string{"synchronous_standby_names"}).AddRow("ANY 2 (xx, ")
			mock.ExpectQuery(fmt.Sprintf("SHOW %s", postgresconf.SynchronousStandbyNames)).WillReturnRows(rows)
			_, err = getRequestedSynchronousStandbysNumber(db)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not matching synchronous standby names regex: ANY 2 (xx"))
		})
	})

	It("sets the number of sync replicas as -1 if it can't parse the sync replicas string", func() {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())

		rows := sqlmock.NewRows([]string{"synchronous_standby_names"}).
			AddRow(`( "cluster-example-2","cluster-example-3")`)
		mock.ExpectQuery(fmt.Sprintf("SHOW %s", postgresconf.SynchronousStandbyNames)).WillReturnRows(rows)

		exporter.collectFromPrimarySynchronousStandbysNumber(db)

		registry := prometheus.NewRegistry()
		registry.MustRegister(exporter.Metrics.SyncReplicas)
		metrics, _ := registry.Gather()

		for _, metric := range metrics {
			m := metric.GetMetric()
			Expect(m[0].GetGauge().GetValue()).To(BeEquivalentTo(-1))
		}
	})

	Context("collectUsedNodes", func() {
		const (
			nodesUsedName         = "cnpg_collector_nodes_used"
			errorMetricName       = "cnpg_collector_last_collection_error"
			pgCollectionErrorName = "cnpg_collector_collection_errors_total"
		)

		It("should return an error when no custer is defined", func() {
			exporter.collectNodesUsed()

			registry := prometheus.NewRegistry()
			registry.MustRegister(exporter.Metrics.Error)
			registry.MustRegister(exporter.Metrics.PgCollectionErrors)
			registry.MustRegister(exporter.Metrics.NodesUsed)
			metrics, err := registry.Gather()
			Expect(err).ToNot(HaveOccurred())

			nodesUsedMetric := getMetric(metrics, nodesUsedName)
			Expect(nodesUsedMetric).ToNot(BeNil())
			Expect(nodesUsedMetric.GetMetric()[0].GetGauge().GetValue()).To(BeEquivalentTo(-1))

			errorMetric := getMetric(metrics, errorMetricName)
			Expect(errorMetric).ToNot(BeNil())
			Expect(errorMetric.GetMetric()[0].GetGauge().GetValue()).To(BeEquivalentTo(1))

			pgCollectionErrorMetric := getMetric(metrics, pgCollectionErrorName)
			Expect(pgCollectionErrorMetric).ToNot(BeNil())
			Expect(pgCollectionErrorMetric.GetMetric()[0].GetCounter().GetValue()).To(BeEquivalentTo(1))
		})

		It("it should return -1 without an error when Topology.SuccessfullyExtracted is false", func() {
			cluster := &apiv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-example",
				},
				Status: apiv1.ClusterStatus{
					Topology: apiv1.Topology{
						SuccessfullyExtracted: false,
					},
				},
			}
			exporter.getCluster = func() (*apiv1.Cluster, error) {
				return cluster, nil
			}

			exporter.collectNodesUsed()

			registry := prometheus.NewRegistry()
			registry.MustRegister(exporter.Metrics.Error)
			registry.MustRegister(exporter.Metrics.PgCollectionErrors)
			registry.MustRegister(exporter.Metrics.NodesUsed)
			metrics, err := registry.Gather()
			Expect(err).ToNot(HaveOccurred())

			nodesUsedMetric := getMetric(metrics, nodesUsedName)
			Expect(nodesUsedMetric).ToNot(BeNil())
			Expect(nodesUsedMetric.GetMetric()[0].GetGauge().GetValue()).To(BeEquivalentTo(-1))

			errorMetric := getMetric(metrics, errorMetricName)
			Expect(errorMetric).ToNot(BeNil())
			Expect(errorMetric.GetMetric()[0].GetGauge().GetValue()).To(BeEquivalentTo(0))

			pgCollectionErrorMetric := getMetric(metrics, pgCollectionErrorName)
			Expect(pgCollectionErrorMetric).To(BeNil())
		})

		It("should return the number of used nodes", func() {
			// Create a cluster with successfully extracted topology and 3 used nodes
			cluster := &apiv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-example",
				},
				Status: apiv1.ClusterStatus{
					Topology: apiv1.Topology{
						SuccessfullyExtracted: true,
						NodesUsed:             3,
					},
				},
			}
			exporter.getCluster = func() (*apiv1.Cluster, error) {
				return cluster, nil
			}

			exporter.collectNodesUsed()

			registry := prometheus.NewRegistry()
			registry.MustRegister(exporter.Metrics.Error)
			registry.MustRegister(exporter.Metrics.PgCollectionErrors)
			registry.MustRegister(exporter.Metrics.NodesUsed)
			metrics, err := registry.Gather()
			Expect(err).ToNot(HaveOccurred())

			nodesUsedMetric := getMetric(metrics, nodesUsedName)
			Expect(nodesUsedMetric).ToNot(BeNil())
			Expect(nodesUsedMetric.GetMetric()[0].GetGauge().GetValue()).To(BeEquivalentTo(3))

			errorMetric := getMetric(metrics, errorMetricName)
			Expect(errorMetric).ToNot(BeNil())
			Expect(errorMetric.GetMetric()[0].GetGauge().GetValue()).To(BeEquivalentTo(0))

			pgCollectionErrorMetric := getMetric(metrics, pgCollectionErrorName)
			Expect(pgCollectionErrorMetric).To(BeNil())
		})
	})
})

type nameGetter interface {
	GetName() string
}

// getMetric is used to avoid having the direct dependency on: github.com/prometheus/client_model library
func getMetric[T nameGetter](
	metrics []T,
	metricName string,
) T {
	var t T
	for _, metric := range metrics {
		if metric.GetName() == metricName {
			return metric
		}
	}

	return t
}

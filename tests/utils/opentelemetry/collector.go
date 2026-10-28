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

package opentelemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/deployments"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/objects"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/run"
)

const (
	// collectorImage is the image used to run the OpenTelemetry Collector
	collectorImage = "docker.io/otel/opentelemetry-collector-contrib:0.163.2"
	// busyboxImage is the image used to read the records written by the
	// collector, whose image has no shell
	busyboxImage = "docker.io/library/busybox:1.38.0"

	// CollectorName is the name of the Deployment and of the Service of the
	// collector, which receives OTLP via gRPC on port 4317 and via HTTP on
	// port 4318
	CollectorName = "otel-collector"

	// recordsPath is the file where the collector writes the records
	recordsPath = "/data/logs.jsonl"

	collectorConfig = `extensions:
  health_check:
    endpoint: 0.0.0.0:13133
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: 0.0.0.0:4317
      http:
        endpoint: 0.0.0.0:4318
exporters:
  file:
    path: ` + recordsPath + `
service:
  extensions: [health_check]
  pipelines:
    logs:
      receivers: [otlp]
      exporters: [file]
`
)

// Record is a log record received by the collector. Its values are
// strings, or maps for the key-value lists, while the other types are nil.
type Record struct {
	Resource   map[string]any
	Scope      string
	Body       any
	Attributes map[string]any
}

// exportRequest is the OTLP JSON encoding of the records, as written by the
// file exporter
type exportRequest struct {
	ResourceLogs []struct {
		Resource struct {
			Attributes []keyValue `json:"attributes"`
		} `json:"resource"`
		ScopeLogs []struct {
			Scope struct {
				Name string `json:"name"`
			} `json:"scope"`
			LogRecords []struct {
				Body       anyValue   `json:"body"`
				Attributes []keyValue `json:"attributes"`
			} `json:"logRecords"`
		} `json:"scopeLogs"`
	} `json:"resourceLogs"`
}

type keyValue struct {
	Key   string   `json:"key"`
	Value anyValue `json:"value"`
}

type anyValue struct {
	StringValue *string `json:"stringValue"`
	KvlistValue *struct {
		Values []keyValue `json:"values"`
	} `json:"kvlistValue"`
}

func (v anyValue) value() any {
	switch {
	case v.StringValue != nil:
		return *v.StringValue
	case v.KvlistValue != nil:
		return toMap(v.KvlistValue.Values)
	default:
		return nil
	}
}

func toMap(keyValues []keyValue) map[string]any {
	result := make(map[string]any, len(keyValues))
	for _, kv := range keyValues {
		result[kv.Key] = kv.Value.value()
	}
	return result
}

// Deploy creates a collector in the given namespace and waits for it to be
// ready
func Deploy(ctx context.Context, crudClient client.Client, namespace string, timeoutSeconds uint) error {
	labels := map[string]string{"app": CollectorName}
	securityContext := &corev1.SecurityContext{
		AllowPrivilegeEscalation: ptr.To(false),
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}
	dataMount := corev1.VolumeMount{Name: "data", MountPath: "/data"}

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: CollectorName, Namespace: namespace},
		Data:       map[string]string{"config.yaml": collectorConfig},
	}
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: CollectorName, Namespace: namespace},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(1)),
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "collector",
							Image: collectorImage,
							Args:  []string{"--config=/etc/otelcol/config.yaml"},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{Path: "/", Port: intstr.FromInt32(13133)},
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "config", MountPath: "/etc/otelcol"},
								dataMount,
							},
							SecurityContext: securityContext,
						},
						{
							Name:            "reader",
							Image:           busyboxImage,
							Command:         []string{"sleep", "infinity"},
							VolumeMounts:    []corev1.VolumeMount{dataMount},
							SecurityContext: securityContext,
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "config",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{Name: CollectorName},
								},
							},
						},
						{
							Name:         "data",
							VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
						},
					},
				},
			},
		},
	}
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: CollectorName, Namespace: namespace},
		Spec: corev1.ServiceSpec{
			Selector: labels,
			Ports: []corev1.ServicePort{
				{Name: "grpc", Port: 4317},
				{Name: "http", Port: 4318},
			},
		},
	}

	for _, object := range []client.Object{configMap, deployment, service} {
		if _, err := objects.Create(ctx, crudClient, object); err != nil {
			return err
		}
	}

	return deployments.WaitForReady(ctx, crudClient, deployment, timeoutSeconds)
}

// Records returns the log records received by the collector in the given
// namespace
func Records(namespace string) ([]Record, error) {
	stdout, _, err := run.Unchecked(fmt.Sprintf(
		"kubectl exec -n %v deployment/%v -c reader -- cat %v",
		namespace, CollectorName, recordsPath))
	if err != nil {
		return nil, err
	}

	var records []Record
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	for i, line := range lines {
		var request exportRequest
		if err := json.Unmarshal([]byte(line), &request); err != nil {
			// The collector may be still writing the last line
			if i == len(lines)-1 {
				break
			}
			return nil, err
		}

		for _, resourceLogs := range request.ResourceLogs {
			resource := toMap(resourceLogs.Resource.Attributes)
			for _, scopeLogs := range resourceLogs.ScopeLogs {
				for _, logRecord := range scopeLogs.LogRecords {
					records = append(records, Record{
						Resource:   resource,
						Scope:      scopeLogs.Scope.Name,
						Body:       logRecord.Body.value(),
						Attributes: toMap(logRecord.Attributes),
					})
				}
			}
		}
	}

	return records, nil
}

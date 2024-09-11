/*
Copyright The CloudNativePG Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package sternmultitailer

import (
	"context"
	"io"
	"os"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

const (
	// ClusterLogsDirectory contains the fixed path to store the cluster logs
	ClusterLogsDirectory = "cluster_logs/"

	// OperatorLogsDirectory is the fixed path to store the logs of all the operator pods
	OperatorLogsDirectory = "operator_logs/"
)

// SternMultiTailer contains the necessary data for the logs of every cluster
type SternMultiTailer struct {
	stdOut       *io.PipeReader
	openFilesMap map[string]*os.File
}

// CatchClusterLogs execute StreamLogs with the specific labels to match
// only the CNPG pods and send them to ClusterLogsDirectory path
func (s *SternMultiTailer) CatchClusterLogs(ctx context.Context, client kubernetes.Interface) chan struct{} {
	// Select all the pods belonging to CNPG
	labelSelector, _ := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{
			{
				Key:      utils.ClusterLabelName,
				Operator: metav1.LabelSelectorOpExists,
			},
		},
	})

	return s.streamLogs(ctx, client, labelSelector, ClusterLogsDirectory)
}

// CatchOperatorLogs execute streamLogs with the labels to match the Operator labels
// and send them to OperatorLogsDirectory
func (s *SternMultiTailer) CatchOperatorLogs(ctx context.Context, client kubernetes.Interface) chan struct{} {
	// Select all the pods belonging to CNPG
	labelSelector, _ := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchLabels: map[string]string{
			"app.kubernetes.io/name": "cloudnative-pg",
		},
	})

	return s.streamLogs(ctx, client, labelSelector, OperatorLogsDirectory)
}

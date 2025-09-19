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

package logs

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/podlogs"
)

// clusterLogs contains the options and context to retrieve cluster logs
type clusterLogs struct {
	ctx         context.Context
	clusterName string
	namespace   string
	timestamp   bool
	tailLines   int64
	outputFile  string
	follow      bool
	client      kubernetes.Interface
}

func getCluster(cl clusterLogs) (*apiv1.Cluster, error) {
	var cluster apiv1.Cluster
	err := plugin.Client.Get(cl.ctx,
		types.NamespacedName{Namespace: cl.namespace, Name: cl.clusterName},
		&cluster)

	return &cluster, err
}

func getStreamClusterLogs(cluster *apiv1.Cluster, cl clusterLogs) podlogs.ClusterWriter {
	var sinceTime *metav1.Time
	var tail *int64
	if cl.timestamp {
		sinceTime = &metav1.Time{Time: time.Now().UTC()}
	}
	if cl.tailLines >= 0 {
		tail = &cl.tailLines
	}
	return podlogs.ClusterWriter{
		Cluster: cluster,
		Options: &corev1.PodLogOptions{
			Timestamps: cl.timestamp,
			Follow:     cl.follow,
			SinceTime:  sinceTime,
			TailLines:  tail,
		},
		Client: cl.client,
	}
}

// followCluster will tail all pods in the cluster, and will watch for any
// new pods
//
// It will write lines to standard-out, and will only return when there are
// no pods left, or it is interrupted by the user
func followCluster(cl clusterLogs) error {
	cluster, err := getCluster(cl)
	if err != nil {
		return fmt.Errorf("could not get cluster: %w", err)
	}

	streamClusterLogs := getStreamClusterLogs(cluster, cl)
	return streamClusterLogs.SingleStream(cl.ctx, os.Stdout)
}

// saveClusterLogs will tail all pods in the cluster, and read their logs
// until the present time, then exit.
//
// It will write lines to standard-out, or to a file if the `file` argument
// is provided.
func saveClusterLogs(cl clusterLogs) error {
	cluster, err := getCluster(cl)
	if err != nil {
		return fmt.Errorf("could not get cluster: %w", err)
	}

	var output io.Writer = os.Stdout
	if cl.outputFile != "" {
		outputFile, err := os.Create(filepath.Clean(cl.outputFile))
		if err != nil {
			return fmt.Errorf("could not create file: %w", err)
		}
		output = outputFile

		defer func() {
			errF := outputFile.Sync()
			if errF != nil && err == nil {
				err = fmt.Errorf("could not flush file: %w", errF)
			}

			errF = outputFile.Close()
			if errF != nil && err == nil {
				err = fmt.Errorf("could not close file: %w", errF)
			}
		}()
	}

	streamClusterLogs := getStreamClusterLogs(cluster, cl)
	err = streamClusterLogs.SingleStream(cl.ctx, output)
	if err != nil {
		return fmt.Errorf("could not stream the logs: %w", err)
	}
	if cl.outputFile != "" {
		fmt.Printf("Successfully written logs to \"%s\"\n", cl.outputFile)
	}
	return nil
}

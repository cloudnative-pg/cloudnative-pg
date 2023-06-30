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

package logs

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils/logs"
)

// followCluster will tail all pods in the cluster, and will watch for any
// new pods
//
// It will write lines to standard-out, and will only return when there are
// no pods left, or it is interrupted by the user
func followCluster(ctx context.Context, clusterName, namespace string,
	logTimeStamp bool, timestamp time.Time,
) error {
	var cluster cnpgv1.Cluster
	err := plugin.Client.Get(ctx,
		types.NamespacedName{Namespace: namespace, Name: clusterName},
		&cluster)
	if err != nil {
		return fmt.Errorf("could not get cluster: %w", err)
	}

	streamClusterLogs := logs.ClusterStreamingRequest{
		Cluster: cluster,
		Options: &v1.PodLogOptions{
			Timestamps: logTimeStamp,
			Follow:     true,
			SinceTime:  &metav1.Time{Time: timestamp},
		},
	}
	return streamClusterLogs.SingleStream(ctx, os.Stdout)
}

// saveClusterLogs will tail all pods in the cluster, and will watch for any
// new pods
//
// It will write lines to standard-out, and will only return when there are
// no pods left, or it is interrupted by the user
func saveClusterLogs(ctx context.Context, clusterName, namespace string,
	logTimeStamp bool, file string,
) error {
	var cluster cnpgv1.Cluster
	err := plugin.Client.Get(ctx,
		types.NamespacedName{Namespace: namespace, Name: clusterName},
		&cluster)
	if err != nil {
		return fmt.Errorf("could not get cluster: %w", err)
	}

	var output io.Writer = os.Stdout
	if file != "" {
		outputFile, err := os.Create(filepath.Clean(file))
		if err != nil {
			return fmt.Errorf("could not create zip file: %w", err)
		}
		output = outputFile

		defer func() {
			errF := outputFile.Sync()
			if errF != nil && err == nil {
				err = fmt.Errorf("could not flush the file: %w", errF)
			}

			errF = outputFile.Close()
			if errF != nil && err == nil {
				err = fmt.Errorf("could not close the file: %w", errF)
			}
		}()
	}

	streamClusterLogs := logs.ClusterStreamingRequest{
		Cluster: cluster,
		Options: &v1.PodLogOptions{
			Timestamps: logTimeStamp,
			Follow:     false,
		},
	}
	err = streamClusterLogs.SingleStream(ctx, output)
	if err != nil {
		return fmt.Errorf("could not stream the logs: %w", err)
	}
	if file != "" {
		fmt.Printf("Successfully written logs to \"%s\"\n", file)
	}
	return nil
}

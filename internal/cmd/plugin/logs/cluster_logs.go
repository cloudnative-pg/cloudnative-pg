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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils/logs"
)

// ClusterLogs contains the options and context to retrieve cluster logs
type ClusterLogs struct {
	Ctx         context.Context
	ClusterName string
	Namespace   string
	timestamp   bool
	TailLines   int64
	OutputFile  string
	Follow      bool
	Client      kubernetes.Interface
}

func getCluster(cl ClusterLogs) (*cnpgv1.Cluster, error) {
	var cluster cnpgv1.Cluster
	err := plugin.Client.Get(cl.Ctx,
		types.NamespacedName{Namespace: cl.Namespace, Name: cl.ClusterName},
		&cluster)

	return &cluster, err
}

// GetStreamClusterLogs return a normlized struct with the default values for the stream request
func GetStreamClusterLogs(cluster *cnpgv1.Cluster, cl ClusterLogs) logs.ClusterStreamingRequest {
	var sinceTime *metav1.Time
	var tail *int64
	if cl.timestamp {
		sinceTime = &metav1.Time{Time: time.Now().UTC()}
	}
	if cl.TailLines >= 0 {
		tail = &cl.TailLines
	}
	return logs.ClusterStreamingRequest{
		Cluster: cluster,
		Options: &corev1.PodLogOptions{
			Timestamps: cl.timestamp,
			Follow:     cl.Follow,
			SinceTime:  sinceTime,
			TailLines:  tail,
		},
		Client: cl.Client,
	}
}

// FollowCluster will tail all pods in the cluster, and will watch for any
// new pods
//
// It will write lines to standard-out, and will only return when there are
// no pods left, or it is interrupted by the user
func FollowCluster(cl ClusterLogs) error {
	cluster, err := getCluster(cl)
	if err != nil {
		return fmt.Errorf("could not get cluster: %w", err)
	}

	streamClusterLogs := GetStreamClusterLogs(cluster, cl)
	return streamClusterLogs.SingleStream(cl.Ctx, os.Stdout)
}

// saveClusterLogs will tail all pods in the cluster, and read their logs
// until the present time, then exit.
//
// It will write lines to standard-out, or to a file if the `file` argument
// is provided.
func saveClusterLogs(cl ClusterLogs) error {
	cluster, err := getCluster(cl)
	if err != nil {
		return fmt.Errorf("could not get cluster: %w", err)
	}

	var output io.Writer = os.Stdout
	if cl.OutputFile != "" {
		outputFile, err := os.Create(filepath.Clean(cl.OutputFile))
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

	streamClusterLogs := GetStreamClusterLogs(cluster, cl)
	err = streamClusterLogs.SingleStream(cl.Ctx, output)
	if err != nil {
		return fmt.Errorf("could not stream the logs: %w", err)
	}
	if cl.OutputFile != "" {
		fmt.Printf("Successfully written logs to \"%s\"\n", cl.OutputFile)
	}
	return nil
}

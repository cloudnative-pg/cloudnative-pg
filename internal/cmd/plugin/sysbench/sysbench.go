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

// Package sysbench implements the kubectl-cnpg sysbench sub-command

package sysbench

import (
	"context"
	"fmt"
	"os"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	client "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
)

type sysbenchRun struct {
	clusterName             string
	jobName                 string
	dbName                  string
	sysbenchImage           string
	sysbenchCommandArgs     []string
	nodeSelector            []string
	dryRun                  bool
	ttlSecondsAfterFinished int32
}

const (
	sysBenchKeyword = "sysbench"
)

var jobExample = `
  # Dry-run with default values
  kubectl-cnpg sysbench cluster-example --dry-run

  # Initialize the database with oltp_read_write workload with 4 tables and 10000 rows per table
  kubectl-cnpg sysbench cluster-example -- oltp_read_write --tables=4 --table-size=10000 prepare

  # Run the benchmark with 4 threads for 30 seconds
  kubectl-cnpg sysbench cluster-example -- oltp_read_write --tables=4 --table-size=10000 --time=30 --threads=4 --report-interval=1 run

  # Cleanup the sysbench tables after benchmarking
  kubectl-cnpg sysbench cluster-example -- oltp_read_write --tables=4 --table-size=10000 cleanup
`

// Method executes the sysbench command, creating a job with the specified parameters and printing the result.
func (cmd *sysbenchRun) execute(ctx context.Context) error {
	cluster, err := cmd.getCluster(ctx)
	if err != nil {
		return err
	}

	job := cmd.buildJob(cluster)

	if cmd.dryRun {
		return plugin.Print(job, plugin.OutputFormatYAML, os.Stdout)
	}

	if err := plugin.Client.Create(ctx, job); err != nil {
		return err
	}

	fmt.Printf("job/%v created\n", job.Name)
	return nil
}

func (cmd *sysbenchRun) getCluster(ctx context.Context) (*apiv1.Cluster, error) {
	var cluster apiv1.Cluster
	err := plugin.Client.Get(
		ctx,
		client.ObjectKey{Namespace: plugin.Namespace, Name: cmd.clusterName},
		&cluster,
	)
	if err != nil {
		return nil, err
	}
	return &cluster, nil
}

func (cmd *sysbenchRun) buildArgs() []string {
	connArgs := []string{
		"--db-driver=pgsql",
		fmt.Sprintf("--pgsql-host=%s%s", cmd.clusterName, apiv1.ServiceReadWriteSuffix),
		"--pgsql-port=5432",
		fmt.Sprintf("--pgsql-db=%s", cmd.dbName),
		"--pgsql-user=$(PGUSER)",         // resolved from env var
		"--pgsql-password=$(PGPASSWORD)", // resolved from env var
	}
	return append(connArgs, cmd.sysbenchCommandArgs...)
}

func (cmd *sysbenchRun) buildJob(cluster *apiv1.Cluster) *batchv1.Job {
	labels := map[string]string{
		"sysbench": cluster.Name,
	}

	result := &batchv1.Job{
		// To ensure we have manifest with Kind and API in --dry-run
		TypeMeta: metav1.TypeMeta{
			APIVersion: "batch/v1",
			Kind:       "Job",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      cmd.getJobName(),
			Namespace: cluster.Namespace,
			Labels:    labels,
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					SchedulerName: cluster.Spec.SchedulerName,
					Containers: []corev1.Container{
						{
							Name:            "sysbench",
							Image:           cmd.sysbenchImage,
							ImagePullPolicy: corev1.PullAlways,
							Env:             cmd.buildEnvVariables(),
							Command:         []string{sysBenchKeyword},
							Args:            cmd.buildArgs(),
						},
					},
					NodeSelector:     cmd.buildNodeSelector(),
					ImagePullSecrets: buildImagePullSecrets(cluster),
				},
			},
		},
	}

	if cmd.ttlSecondsAfterFinished != 0 {
		result.Spec.TTLSecondsAfterFinished = &cmd.ttlSecondsAfterFinished
	}

	return result
}

func (cmd *sysbenchRun) getJobName() string {
	if cmd.jobName != "" {
		return cmd.jobName
	}

	return fmt.Sprintf("%v-sysbench-%v", cmd.clusterName, rand.Intn(100000))
}

func (cmd *sysbenchRun) buildNodeSelector() map[string]string {
	selectorLength := len(cmd.nodeSelector)
	if selectorLength < 1 {
		return nil
	}

	mappedSelectors := make(map[string]string, selectorLength)
	for _, v := range cmd.nodeSelector {
		selector := strings.Split(v, "=")
		if len(selector) <= 1 {
			continue
		}
		mappedSelectors[selector[0]] = selector[1]
	}
	return mappedSelectors
}

func buildImagePullSecrets(cluster *apiv1.Cluster) []corev1.LocalObjectReference {
	if len(cluster.Spec.ImagePullSecrets) == 0 {
		return nil
	}

	secrets := make([]corev1.LocalObjectReference, len(cluster.Spec.ImagePullSecrets))
	for i, s := range cluster.Spec.ImagePullSecrets {
		secrets[i] = corev1.LocalObjectReference{Name: s.Name}
	}
	return secrets
}

func (cmd *sysbenchRun) buildEnvVariables() []corev1.EnvVar {
	appSecretName := fmt.Sprintf("%v-%v", cmd.clusterName, "app")

	envVar := []corev1.EnvVar{
		{
			Name: "PGUSER",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: appSecretName,
					},
					Key: "username",
				},
			},
		},
		{
			Name: "PGPASSWORD",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: appSecretName,
					},
					Key: "password",
				},
			},
		},
	}

	return envVar
}

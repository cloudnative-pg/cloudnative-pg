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

// Package pgbench implements the kubectl-cnpg pgbench sub-command
package pgbench

import (
	"context"
	"fmt"
	"os"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
)

type pgBenchRun struct {
	jobName                 string
	clusterName             string
	dbName                  string
	nodeSelector            []string
	pgBenchCommandArgs      []string
	dryRun                  bool
	ttlSecondsAfterFinished int32
}

const (
	pgBenchKeyWord = "pgbench"
)

var jobExample = `
  # Dry-run command with default values and [cluster] "cluster-example"
  kubectl-cnpg pgbench cluster-example --dry-run

  # Create a pgbench job with default values and [cluster] "cluster-example"
  kubectl-cnpg pgbench cluster-example

  # Dry-run command with given values and [cluster] "cluster-example"
  kubectl-cnpg pgbench cluster-example --db-name pgbenchDBName --job-name job-name --dry-run -- \
    --time 30 --client 1 --jobs 1

  # Create a job with given values and [cluster] "cluster-example"
  kubectl-cnpg pgbench cluster-example --db-name pgbenchDBName --job-name job-name -- \
    --time 30 --client 1 --jobs 1

  # Create a job with given values on [cluster] "cluster-example". The job will be cleaned after 10 minutes.
  kubectl-cnpg pgbench cluster-example --db-name pgbenchDBName --job-name job-name --ttl 600 -- \
    --time 30 --client 1 --jobs 1`

func (cmd *pgBenchRun) execute(ctx context.Context) error {
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

func (cmd *pgBenchRun) getCluster(ctx context.Context) (*apiv1.Cluster, error) {
	var cluster apiv1.Cluster
	err := plugin.Client.Get(
		ctx,
		client.ObjectKey{Namespace: plugin.Namespace, Name: cmd.clusterName},
		&cluster)
	if err != nil {
		return nil, fmt.Errorf("could not get cluster: %v", err)
	}

	return &cluster, nil
}

func (cmd *pgBenchRun) getJobName() string {
	if cmd.jobName == "" {
		return fmt.Sprintf("%v-%v-%v", cmd.clusterName, pgBenchKeyWord, rand.Intn(1000000))
	}

	return cmd.jobName
}

func (cmd *pgBenchRun) buildNodeSelector() map[string]string {
	selectorsLength := len(cmd.nodeSelector)
	if selectorsLength < 1 {
		return nil
	}

	mappedSelectors := make(map[string]string, selectorsLength)
	for _, v := range cmd.nodeSelector {
		selector := strings.Split(v, "=")
		if len(selector) <= 1 {
			continue
		}
		mappedSelectors[selector[0]] = selector[1]
	}
	return mappedSelectors
}

func (cmd *pgBenchRun) buildJob(cluster *apiv1.Cluster) *batchv1.Job {
	labels := map[string]string{
		"pgBenchJob": cluster.Name,
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
							Name:            "pgbench",
							Image:           cluster.Status.Image,
							ImagePullPolicy: corev1.PullAlways,
							Env:             cmd.buildEnvVariables(),
							Command:         []string{pgBenchKeyWord},
							Args:            cmd.pgBenchCommandArgs,
							SecurityContext: &corev1.SecurityContext{
								AllowPrivilegeEscalation: ptr.To[bool](false),
								Privileged:               ptr.To[bool](false),
								Capabilities: &corev1.Capabilities{
									Drop: []corev1.Capability{
										"ALL",
									},
								},
							},
						},
					},
					NodeSelector: cmd.buildNodeSelector(),
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: ptr.To[bool](true),
						RunAsUser:    ptr.To[int64](1000),
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
					},
				},
			},
		},
	}

	if cmd.ttlSecondsAfterFinished != 0 {
		result.Spec.TTLSecondsAfterFinished = &cmd.ttlSecondsAfterFinished
	}

	return result
}

func (cmd *pgBenchRun) buildEnvVariables() []corev1.EnvVar {
	clusterName := cmd.clusterName
	pgHost := fmt.Sprintf("%v%v", clusterName, apiv1.ServiceReadWriteSuffix)
	appSecreteName := fmt.Sprintf("%v-%v", clusterName, "app")

	envVar := []corev1.EnvVar{
		{
			Name:  "PGHOST",
			Value: pgHost,
		},
		{
			Name:  "PGDATABASE",
			Value: cmd.dbName,
		},
		{
			Name:  "PGPORT",
			Value: "5432",
		},
		{
			Name: "PGUSER",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: appSecreteName,
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
						Name: appSecreteName,
					},
					Key: "password",
				},
			},
		},
	}

	return envVar
}

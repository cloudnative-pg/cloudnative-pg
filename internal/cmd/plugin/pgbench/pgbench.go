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

// Package pgbench implements the kubectl-cnpg pgbench sub-command
package pgbench

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/i18n"
	"k8s.io/kubectl/pkg/util/templates"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
)

type createPgBenchJobOptions struct {
	jobName            string
	clusterName        string
	dbName             string
	pgBenchCommandArgs []string
	dryRun             bool
}

const (
	pgBenchDBName  = "app"
	pgBenchKeyWord = "pgbench"
)

var jobExample = templates.Examples(i18n.T(`
		# Dry-run command with default values and clusterName "cluster-example"
		kubectl-cnpg pgbench cluster-example --dry-run
		
		# Create a pgbench job with default values and clusterName "cluster-example"
		kubectl-cnpg pgbench cluster-example 
		
		# Dry-run command with given values and clusterName "cluster-example"
		kubectl-cnpg pgbench cluster-example --db-name pgbenchDBName --pgbench-job-name jobName --dry-run -- --time 30 
		--client 1 --jobs 1

		# Create a job with given values and clusterName "cluster-example"
		kubectl-cnpg pgbench cluster-example --db-name pgbenchDBName --pgbench-job-name jobName -- --time 30 --client 1
        --jobs 1`))

// initJobOptions initialize pgbench job options
func initJobOptions(cmd *cobra.Command, args []string) (*createPgBenchJobOptions, error) {
	argsLen := cmd.ArgsLenAtDash()
	// ArgsLenAtDash returns -1 when -- was not specified
	if argsLen == -1 {
		argsLen = len(args)
	}
	if argsLen != 1 {
		return nil, cmdutil.UsageErrorf(cmd, "Cluster Name is required to create pgbench job, got empty %d",
			argsLen)
	}
	clusterName := args[0]
	var pgBenchCommandArgs []string
	if len(args) > 1 {
		pgBenchCommandArgs = args[1:]
	}
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	jobName, _ := cmd.Flags().GetString("pgbench-job-name")
	dbName, _ := cmd.Flags().GetString("db-name")
	if jobName == "" {
		jobName = fmt.Sprintf("%v-%v-%v", clusterName,
			pgBenchKeyWord, rand.Intn(1000000))
	}
	return &createPgBenchJobOptions{
		jobName:            jobName,
		pgBenchCommandArgs: pgBenchCommandArgs,
		dryRun:             dryRun,
		clusterName:        clusterName,
		dbName:             dbName,
	}, nil
}

// Create a new job with given inputs
func Create(ctx context.Context, createOptions *createPgBenchJobOptions) error {
	var cluster apiv1.Cluster
	clusterName := createOptions.clusterName

	// Get the Cluster object
	err := plugin.Client.Get(
		ctx,
		client.ObjectKey{Namespace: plugin.Namespace, Name: clusterName},
		&cluster)
	if err != nil {
		return fmt.Errorf("could not get cluster: %v", err)
	}

	// Get the cluster image
	imageName := cluster.Spec.ImageName
	if imageName == "" {
		return fmt.Errorf("could not get imageName: %v", err)
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      createOptions.jobName,
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				"pbBenchJob": cluster.Name,
			},
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"pbBenchJob": cluster.Name,
					},
				},

				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{
							Name:  "wait-for-cnpg",
							Image: imageName,
							Env:   createEnvVariables(*createOptions),
							Command: []string{
								"sh",
								"-c",
								"until psql -c \"SELECT 1\"; do echo 'Waiting for service' sleep 15; done",
							},
						},
						{
							Name:  "pgbench-init",
							Image: imageName,
							Env:   createEnvVariables(*createOptions),
							Command: []string{
								"pgbench",
							},
							Args: []string{
								"--initialize",
								"--scale",
								"1",
							},
						},
					},

					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:            "pgbench",
							Image:           imageName,
							ImagePullPolicy: corev1.PullAlways,
							Env:             createEnvVariables(*createOptions),
							Command:         []string{pgBenchKeyWord},
							Args:            createOptions.pgBenchCommandArgs,
						},
					},
				},
			},
		},
	}
	if createOptions.dryRun {
		outPutFormate := plugin.OutputFormatYAML
		err = plugin.Print(job, plugin.OutputFormat(outPutFormate), os.Stdout)
		if err != nil {
			return err
		}
		return nil
	}
	err = plugin.Client.Create(ctx, job)
	if err != nil {
		return err
	}

	fmt.Printf("job/%v created\n", job.Name)
	return nil
}

func createEnvVariables(createOptions createPgBenchJobOptions) []corev1.EnvVar {
	clusterName := createOptions.clusterName
	pgHost := fmt.Sprintf("%v%v", clusterName, apiv1.ServiceReadWriteSuffix)
	appSecreteName := fmt.Sprintf("%v-%v", clusterName, "app")

	envVar := []corev1.EnvVar{
		{
			Name:  "PGHOST",
			Value: pgHost,
		},
		{
			Name:  "PGDATABASE",
			Value: createOptions.dbName,
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

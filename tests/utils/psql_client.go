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

package utils

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/versions"
)

// GetPsqlClient gets a psql client pod for service connectivity
func GetPsqlClient(namespace string, env *TestingEnvironment) (*corev1.Pod, error) {
	_ = corev1.AddToScheme(env.Scheme)
	pod := &corev1.Pod{}
	err := env.CreateNamespace(namespace)
	if err != nil {
		return pod, err
	}
	pod, err = createPsqlClient(namespace, env)
	if err != nil {
		return pod, err
	}
	err = PodWaitForReady(env, pod, 300)
	if err != nil {
		return pod, err
	}
	return pod, nil
}

// createPsqlClient creates a psql client
func createPsqlClient(namespace string, env *TestingEnvironment) (*corev1.Pod, error) {
	seccompProfile := &corev1.SeccompProfile{
		Type: corev1.SeccompProfileTypeRuntimeDefault,
	}
	if !utils.HaveSeccompSupport() || utils.HaveSecurityContextConstraints() {
		seccompProfile = nil
	}

	psqlPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      "psql-client",
			Labels:    map[string]string{"run": "psql"},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  specs.PostgresContainerName,
					Image: versions.DefaultImageName,
					// sleep long enough to avoid starting the postgres server until we finish the testing
					Command: []string{"bash", "-c", "sleep 7200"},
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: pointer.Bool(false),
						SeccompProfile:           seccompProfile,
					},
				},
			},
			DNSPolicy:     corev1.DNSClusterFirst,
			RestartPolicy: corev1.RestartPolicyAlways,
			SecurityContext: &corev1.PodSecurityContext{
				SeccompProfile: seccompProfile,
			},
		},
	}

	err := env.Client.Create(env.Ctx, psqlPod)
	if err != nil {
		return &corev1.Pod{}, err
	}

	return psqlPod, nil
}

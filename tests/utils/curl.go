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
	"fmt"
	"k8s.io/utils/pointer"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CurlClient returns the Pod definition for a curl client
func CurlClient(namespace string) corev1.Pod {
	curlPod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      "curl",
			Labels:    map[string]string{"run": "curl"},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:    "curl",
					Image:   "curlimages/curl:7.82.0",
					Command: []string{"sleep", "3600"},
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: pointer.Bool(false),
						SeccompProfile:           &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
						RunAsNonRoot:             pointer.Bool(true),
					},
				},
			},
			DNSPolicy:     corev1.DNSClusterFirst,
			RestartPolicy: corev1.RestartPolicyAlways,
			SecurityContext: &corev1.PodSecurityContext{
				SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
				RunAsNonRoot:   pointer.Bool(true),
			},
		},
	}
	return curlPod
}

// CurlGetMetrics returns true if test connection is successful else false
func CurlGetMetrics(namespace, curlPodName, podIP string, port int) (string, error) {
	out, _, err := RunRetry(fmt.Sprintf(
		"kubectl exec -n %v %v -- curl -s %v:%v/metrics",
		namespace,
		curlPodName,
		podIP,
		port))
	return out, err
}

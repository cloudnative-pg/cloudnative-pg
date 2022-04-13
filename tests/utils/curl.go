/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package utils

import (
	"fmt"

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
				},
			},
			DNSPolicy:     corev1.DNSClusterFirst,
			RestartPolicy: corev1.RestartPolicyAlways,
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

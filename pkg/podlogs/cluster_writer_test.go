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

package podlogs

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type syncBuffer struct {
	b bytes.Buffer
	m sync.Mutex
}

func (b *syncBuffer) Write(p []byte) (n int, err error) {
	b.m.Lock()
	defer b.m.Unlock()
	return b.b.Write(p)
}

func (b *syncBuffer) String() string {
	b.m.Lock()
	defer b.m.Unlock()
	return b.b.String()
}

var _ = Describe("Cluster logging tests", func() {
	clusterNamespace := "cluster-test"
	clusterName := "myTestCluster"
	cluster := &apiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: clusterNamespace,
			Name:      clusterName,
		},
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: clusterNamespace,
			Name:      clusterName + "-1",
			Labels: map[string]string{
				utils.ClusterLabelName: clusterName,
			},
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name: "postgresql",
					State: corev1.ContainerState{
						Running: &corev1.ContainerStateRunning{},
					},
				},
			},
		},
	}
	podWithSidecars := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: clusterNamespace,
			Name:      clusterName + "-1",
			Labels: map[string]string{
				utils.ClusterLabelName: clusterName,
			},
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name: "postgresql",
					State: corev1.ContainerState{
						Running: &corev1.ContainerStateRunning{},
					},
				},
				{
					Name: "sidecar",
					State: corev1.ContainerState{
						Running: &corev1.ContainerStateRunning{},
					},
				},
			},
		},
	}
	It("should exit on ended pod logs with the non-follow option", func(ctx context.Context) {
		client := fake.NewClientset(pod)
		var logBuffer bytes.Buffer
		var wait sync.WaitGroup
		wait.Add(1)
		go func() {
			defer GinkgoRecover()
			defer wait.Done()
			streamClusterLogs := ClusterWriter{
				Cluster: cluster,
				Options: &corev1.PodLogOptions{
					Follow: false,
				},
				Client: client,
			}
			err := streamClusterLogs.SingleStream(ctx, &logBuffer)
			Expect(err).NotTo(HaveOccurred())
		}()
		ctx.Done()
		wait.Wait()
		Expect(logBuffer.String()).To(BeEquivalentTo("fake logs\n"))
	})

	It("should catch the logs of the sidecar too", func(ctx context.Context) {
		client := fake.NewClientset(podWithSidecars)
		var logBuffer bytes.Buffer
		var wait sync.WaitGroup
		wait.Add(1)
		go func() {
			defer GinkgoRecover()
			defer wait.Done()
			streamClusterLogs := ClusterWriter{
				Cluster: cluster,
				Options: &corev1.PodLogOptions{
					Follow: false,
				},
				Client: client,
			}
			err := streamClusterLogs.SingleStream(ctx, &logBuffer)
			Expect(err).NotTo(HaveOccurred())
		}()
		ctx.Done()
		wait.Wait()
		Expect(logBuffer.String()).To(BeEquivalentTo("fake logs\nfake logs\n"))
	})

	It("should catch extra logs if given the follow option", func(ctx context.Context) {
		client := fake.NewClientset(pod)

		wg := sync.WaitGroup{}
		wg.Add(1)
		var logBuffer syncBuffer

		// let's set a short follow-wait, and keep the cluster streaming for two
		// cycles
		followWaiting := 150 * time.Millisecond
		ctx2, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
		go func() {
			// we always invoke done no matter what happens
			defer wg.Done()
			defer GinkgoRecover()
			streamClusterLogs := ClusterWriter{
				Cluster: cluster,
				Options: &corev1.PodLogOptions{
					Follow: true,
				},
				FollowWaiting: followWaiting,
				Client:        client,
			}
			err := streamClusterLogs.SingleStream(ctx2, &logBuffer)
			// we cannot reliably now if we will close the function before the context
			// deadline, so we accept both nil and context.DeadlineExceeded
			Expect(err).To(Or(BeNil(), Equal(context.DeadlineExceeded)))
		}()

		time.Sleep(350 * time.Millisecond)
		cancel()
		wg.Wait()

		fakeLogCount := strings.Count(logBuffer.String(), "fake logs\n")
		Expect(fakeLogCount).To(BeNumerically(">=", 2))
	})
})

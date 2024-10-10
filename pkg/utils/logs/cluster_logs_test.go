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
	"bytes"
	"context"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Cluster logging tests", func() {
	clusterNamespace := "cluster-test"
	clusterName := "myTestCluster"
	cluster := &apiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: clusterNamespace,
			Name:      clusterName,
		},
	}
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: clusterNamespace,
			Name:      clusterName + "-1",
			Labels: map[string]string{
				utils.ClusterLabelName: clusterName,
			},
		},
	}
	It("should exit on ended pod logs with the non-follow option", func(ctx context.Context) {
		client := fake.NewSimpleClientset(pod)
		var logBuffer bytes.Buffer
		var wait sync.WaitGroup
		wait.Add(1)
		go func() {
			defer GinkgoRecover()
			defer wait.Done()
			streamClusterLogs := ClusterStreamingRequest{
				Cluster: cluster,
				Options: &v1.PodLogOptions{
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

	It("should catch extra logs if given the follow option", func(ctx context.Context) {
		client := fake.NewSimpleClientset(pod)
		var logBuffer bytes.Buffer
		// let's set a short follow-wait, and keep the cluster streaming for two
		// cycles
		followWaiting := 200 * time.Millisecond
		ctx2, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
		go func() {
			defer GinkgoRecover()
			streamClusterLogs := ClusterStreamingRequest{
				Cluster: cluster,
				Options: &v1.PodLogOptions{
					Follow: true,
				},
				FollowWaiting: followWaiting,
				Client:        client,
			}
			err := streamClusterLogs.SingleStream(ctx2, &logBuffer)
			Expect(err).NotTo(HaveOccurred())
		}()
		// give the stream call time to do a new search for pods
		time.Sleep(350 * time.Millisecond)
		cancel()
		// the fake pod will be seen twice
		Expect(logBuffer.String()).To(BeEquivalentTo("fake logs\nfake logs\n"))
	})
})

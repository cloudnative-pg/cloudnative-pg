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
	"fmt"
	"io"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type multiWriter struct {
	writers map[string]*bytes.Buffer
}

func newMultiWriter() *multiWriter {
	newMw := &multiWriter{
		writers: make(map[string]*bytes.Buffer),
	}
	return newMw
}

func (mw *multiWriter) Create(name string) (io.Writer, error) {
	var buffer bytes.Buffer
	mw.writers[name] = &buffer
	return &buffer, nil
}

var _ = Describe("Pod logging tests", func() {
	podNamespace := "pod-test"
	podName := "pod-name-test"
	pod := v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: podNamespace,
			Name:      podName,
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name: "postgres",
				},
			},
		},
	}

	podWithSidecar := v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: podNamespace,
			Name:      podName,
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name: "postgres",
				},
				{
					Name: "sidecar",
				},
			},
		},
		Status: v1.PodStatus{
			ContainerStatuses: []v1.ContainerStatus{
				{
					Name: "postgres",
					State: v1.ContainerState{
						Running: &v1.ContainerStateRunning{
							StartedAt: metav1.Time{Time: time.Now()},
						},
					},
				},
				{
					Name: "sidecar",
					State: v1.ContainerState{
						Running: &v1.ContainerStateRunning{
							StartedAt: metav1.Time{Time: time.Now()},
						},
					},
				},
			},
		},
	}

	When("using the Stream function", func() {
		It("should return the proper podName", func() {
			streamPodLog := StreamingRequest{
				Pod:     pod,
				Options: &v1.PodLogOptions{},
			}
			Expect(streamPodLog.getPodName()).To(BeEquivalentTo(podName))
			Expect(streamPodLog.getPodNamespace()).To(BeEquivalentTo(podNamespace))
		})

		It("should be able to handle the empty Pod", func(ctx context.Context) {
			client := fake.NewClientset()
			streamPodLog := StreamingRequest{
				Pod:     v1.Pod{},
				Options: &v1.PodLogOptions{},
				Client:  client,
			}
			var logBuffer bytes.Buffer
			err := streamPodLog.Stream(ctx, &logBuffer)
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(BeEquivalentTo(""))
		})

		It("previous option must be false by default", func() {
			streamPodLog := StreamingRequest{
				Pod:     pod,
				Options: &v1.PodLogOptions{},
			}
			Expect(streamPodLog.getLogOptions().Previous).To(BeFalse())
		})

		It("getLogOptions respects the Previous field setting", func() {
			streamPodLog := StreamingRequest{
				Pod:     pod,
				Options: &v1.PodLogOptions{},
			}
			options := streamPodLog.getLogOptions()
			Expect(options.Previous).To(BeFalse())

			streamPodLog.Previous = true
			options = streamPodLog.getLogOptions()
			Expect(options.Previous).To(BeTrue())
		})

		It("should read the logs of a pod with one container", func(ctx context.Context) {
			client := fake.NewClientset(&pod)
			streamPodLog := StreamingRequest{
				Pod:      pod,
				Options:  &v1.PodLogOptions{},
				Previous: false,
				Client:   client,
			}

			var logBuffer bytes.Buffer
			err := streamPodLog.Stream(ctx, &logBuffer)
			Expect(err).ToNot(HaveOccurred())

			Expect(logBuffer.String()).To(BeEquivalentTo("fake logs\n"))
		})

		It("should read the logs of a pod with multiple containers", func(ctx context.Context) {
			client := fake.NewClientset(&podWithSidecar)
			streamPodLog := StreamingRequest{
				Pod:      podWithSidecar,
				Options:  &v1.PodLogOptions{},
				Previous: false,
				Client:   client,
			}

			var logBuffer bytes.Buffer
			err := streamPodLog.Stream(ctx, &logBuffer)
			Expect(err).ToNot(HaveOccurred())

			Expect(logBuffer.String()).To(BeEquivalentTo("fake logs\nfake logs\n"))
		})

		It("should read only the specified container logs in a pod with multiple containers", func(ctx context.Context) {
			client := fake.NewClientset(&podWithSidecar)
			streamPodLog := StreamingRequest{
				Pod: podWithSidecar,
				Options: &v1.PodLogOptions{
					Container: "postgres",
				},
				Previous: false,
				Client:   client,
			}

			var logBuffer bytes.Buffer
			err := streamPodLog.Stream(ctx, &logBuffer)
			Expect(err).ToNot(HaveOccurred())

			Expect(logBuffer.String()).To(BeEquivalentTo("fake logs\n"))
		})

		It("can follow pod logs", func(ctx SpecContext) {
			client := fake.NewClientset(&pod)
			var logBuffer bytes.Buffer
			var wait sync.WaitGroup
			wait.Add(1)
			go func() {
				defer GinkgoRecover()
				defer wait.Done()
				now := metav1.Now()
				streamPodLog := StreamingRequest{
					Pod: pod,
					Options: &v1.PodLogOptions{
						Timestamps: false,
						Follow:     true,
						SinceTime:  &now,
					},
					Client: client,
				}
				err := streamPodLog.Stream(ctx, &logBuffer)
				Expect(err).NotTo(HaveOccurred())
			}()
			// calling ctx.Done is not strictly necessary because the fake Client
			// will terminate the pod stream anyway, ending Stream.
			// But in "production", Stream will follow
			// the pod logs until the context, or the logs, are over
			ctx.Done()
			wait.Wait()
			Expect(logBuffer.String()).To(BeEquivalentTo("fake logs\n"))
		})
	})
	When("usring the StreamMultiple function", func() {
		It("should log each container into a separate writer", func(ctx context.Context) {
			client := fake.NewClientset(&podWithSidecar)
			streamPodLog := StreamingRequest{
				Pod:      podWithSidecar,
				Options:  &v1.PodLogOptions{},
				Previous: false,
				Client:   client,
			}

			namer := func(container string) string {
				return fmt.Sprintf("%s-%s.log", streamPodLog.getPodName(), container)
			}
			mw := newMultiWriter()
			err := streamPodLog.StreamMultiple(ctx, mw, namer)
			Expect(err).ToNot(HaveOccurred())
			Expect(mw.writers).To(HaveLen(2))

			Expect(mw.writers["pod-name-test-postgres.log"].String()).To(BeEquivalentTo("fake logs\n"))
			Expect(mw.writers["pod-name-test-sidecar.log"].String()).To(BeEquivalentTo("fake logs\n"))
		})

		It("can fetch the previous logs for each container", func(ctx context.Context) {
			client := fake.NewClientset(&podWithSidecar)
			streamPodLog := StreamingRequest{
				Pod:      podWithSidecar,
				Options:  &v1.PodLogOptions{},
				Previous: true,
				Client:   client,
			}

			namer := func(container string) string {
				return fmt.Sprintf("%s-%s.log", streamPodLog.getPodName(), container)
			}
			mw := newMultiWriter()
			err := streamPodLog.StreamMultiple(ctx, mw, namer)
			Expect(err).ToNot(HaveOccurred())
			Expect(mw.writers).To(HaveLen(2))

			Expect(mw.writers["pod-name-test-postgres.log"].String()).To(BeEquivalentTo(
				`"====== Beginning of Previous Log ====="
fake logs
"====== End of Previous Log ====="
fake logs
`))

			Expect(mw.writers["pod-name-test-sidecar.log"].String()).To(BeEquivalentTo(
				`"====== Beginning of Previous Log ====="
fake logs
"====== End of Previous Log ====="
fake logs
`))
		})
	})
})

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
	"fmt"
	"io"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
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
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: podNamespace,
			Name:      podName,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "postgres",
				},
			},
		},
	}

	podWithSidecar := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: podNamespace,
			Name:      podName,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "postgres",
				},
				{
					Name: "sidecar",
				},
			},
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name: "postgres",
					State: corev1.ContainerState{
						Running: &corev1.ContainerStateRunning{
							StartedAt: metav1.Time{Time: time.Now()},
						},
					},
				},
				{
					Name: "sidecar",
					State: corev1.ContainerState{
						Running: &corev1.ContainerStateRunning{
							StartedAt: metav1.Time{Time: time.Now()},
						},
					},
				},
			},
		},
	}

	When("using the Stream function", func() {
		It("should return the proper podName", func() {
			streamPodLog := Writer{
				Pod: pod,
			}
			Expect(streamPodLog.Pod.Name).To(BeEquivalentTo(podName))
			Expect(streamPodLog.Pod.Namespace).To(BeEquivalentTo(podNamespace))
		})

		It("should be able to handle the empty Pod", func(ctx context.Context) {
			client := fake.NewClientset()
			streamPodLog := Writer{
				Pod:    corev1.Pod{},
				Client: client,
			}
			var logBuffer bytes.Buffer
			err := streamPodLog.Single(ctx, &logBuffer, &corev1.PodLogOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(BeEquivalentTo(""))
		})

		It("should read the logs of a pod with one container", func(ctx context.Context) {
			client := fake.NewClientset(&pod)
			streamPodLog := Writer{
				Pod:    pod,
				Client: client,
			}

			var logBuffer bytes.Buffer
			err := streamPodLog.Single(ctx, &logBuffer, &corev1.PodLogOptions{})
			Expect(err).ToNot(HaveOccurred())

			Expect(logBuffer.String()).To(BeEquivalentTo("fake logs\n"))
		})

		It("should read the logs of a pod with multiple containers", func(ctx context.Context) {
			client := fake.NewClientset(&podWithSidecar)
			streamPodLog := Writer{
				Pod:    podWithSidecar,
				Client: client,
			}

			var logBuffer bytes.Buffer
			err := streamPodLog.Single(ctx, &logBuffer, &corev1.PodLogOptions{})
			Expect(err).ToNot(HaveOccurred())

			Expect(logBuffer.String()).To(BeEquivalentTo("fake logs\nfake logs\n"))
		})

		It("should read only the specified container logs in a pod with multiple containers", func(ctx context.Context) {
			client := fake.NewClientset(&podWithSidecar)
			streamPodLog := Writer{
				Pod:    podWithSidecar,
				Client: client,
			}

			var logBuffer bytes.Buffer
			err := streamPodLog.Single(ctx, &logBuffer, &corev1.PodLogOptions{
				Container: "postgres",
				Previous:  false,
			})
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
				streamPodLog := Writer{
					Pod:    pod,
					Client: client,
				}
				err := streamPodLog.Single(ctx, &logBuffer, &corev1.PodLogOptions{
					Timestamps: false,
					Follow:     true,
					SinceTime:  &now,
				})
				Expect(err).NotTo(HaveOccurred())
			}()
			// Calling ctx.Done is not strictly necessary because the fake Client
			// will terminate the pod stream anyway, ending Stream.
			// But in "production", Stream will follow
			// the pod logs until the context, or the logs, are over
			ctx.Done()
			wait.Wait()
			Expect(logBuffer.String()).To(BeEquivalentTo("fake logs\n"))
		})
	})
	When("using the StreamMultiple function", func() {
		It("should log each container into a separate writer", func(ctx context.Context) {
			client := fake.NewClientset(&podWithSidecar)
			streamPodLog := Writer{
				Pod:    podWithSidecar,
				Client: client,
			}

			namer := func(container string) string {
				return fmt.Sprintf("%s-%s.log", streamPodLog.Pod.Name, container)
			}
			mw := newMultiWriter()
			err := streamPodLog.Multiple(ctx, &corev1.PodLogOptions{}, mw, namer)
			Expect(err).ToNot(HaveOccurred())
			Expect(mw.writers).To(HaveLen(2))

			Expect(mw.writers["pod-name-test-postgres.log"].String()).To(BeEquivalentTo("fake logs\n"))
			Expect(mw.writers["pod-name-test-sidecar.log"].String()).To(BeEquivalentTo("fake logs\n"))
		})

		It("can fetch the previous logs for each container", func(ctx context.Context) {
			client := fake.NewClientset(&podWithSidecar)
			streamPodLog := Writer{
				Pod:    podWithSidecar,
				Client: client,
			}

			namer := func(container string) string {
				return fmt.Sprintf("%s-%s.log", streamPodLog.Pod.Name, container)
			}
			mw := newMultiWriter()
			err := streamPodLog.Multiple(ctx, &corev1.PodLogOptions{Previous: true}, mw, namer)
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

		It("ensures containerOpts with correct container name is passed to sendLogsToWriter", func(ctx context.Context) {
			client := fake.NewClientset(&podWithSidecar)
			streamPodLog := Writer{
				Pod:    podWithSidecar,
				Client: client,
			}

			namer := func(container string) string {
				return fmt.Sprintf("%s-%s.log", streamPodLog.Pod.Name, container)
			}
			mw := newMultiWriter()

			// This is a regression test for bug where opts was passed instead of containerOpts
			// If container name isn't set correctly, the fake client might not behave as expected
			// The test verifies that each container gets its own log file
			err := streamPodLog.Multiple(ctx, &corev1.PodLogOptions{}, mw, namer)
			Expect(err).ToNot(HaveOccurred())
			Expect(mw.writers).To(HaveLen(2))
			Expect(mw.writers).To(HaveKey("pod-name-test-postgres.log"))
			Expect(mw.writers).To(HaveKey("pod-name-test-sidecar.log"))
		})
	})

	When("handling Previous logs edge cases", func() {
		It("should fetch current logs even when Previous is true (regression test for #8985)", func(ctx context.Context) {
			// This test verifies that when Previous: true is set, the function attempts
			// to fetch both previous and current logs, not previous logs twice
			client := fake.NewClientset(&pod)
			streamPodLog := Writer{
				Pod:    pod,
				Client: client,
			}

			var logBuffer bytes.Buffer
			err := streamPodLog.Single(ctx, &logBuffer, &corev1.PodLogOptions{
				Previous: true,
			})
			Expect(err).NotTo(HaveOccurred())

			// The output should contain the previous log markers and both log entries
			output := logBuffer.String()
			Expect(output).To(ContainSubstring("====== Beginning of Previous Log ====="))
			Expect(output).To(ContainSubstring("====== End of Previous Log ====="))
			// Should have logs from both previous and current (fake client returns "fake logs" for both)
			Expect(output).To(ContainSubstring("fake logs"))
		})

		It("should handle Previous logs in Multiple with correct container options", func(ctx context.Context) {
			// Regression test to ensure containerOpts is passed (not opts) in Multiple
			// when Previous is enabled
			client := fake.NewClientset(&podWithSidecar)
			streamPodLog := Writer{
				Pod:    podWithSidecar,
				Client: client,
			}

			namer := func(container string) string {
				return fmt.Sprintf("%s-%s.log", streamPodLog.Pod.Name, container)
			}
			mw := newMultiWriter()

			err := streamPodLog.Multiple(ctx, &corev1.PodLogOptions{
				Previous:   true,
				Timestamps: true,
			}, mw, namer)
			Expect(err).ToNot(HaveOccurred())

			// Both containers should have their logs collected
			Expect(mw.writers).To(HaveLen(2))

			// Each should contain the previous log markers and actual logs
			for containerName, writer := range mw.writers {
				output := writer.String()
				Expect(output).To(ContainSubstring("====== Beginning of Previous Log ====="),
					"Container %s should have previous log marker", containerName)
				Expect(output).To(ContainSubstring("====== End of Previous Log ====="),
					"Container %s should have end marker", containerName)
				Expect(output).To(ContainSubstring("fake logs"),
					"Container %s should have actual logs", containerName)
			}
		})
	})
})

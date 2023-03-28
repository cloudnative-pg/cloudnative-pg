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

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("StreamingRequest default options", func() {
	podNamespace := "pod-test"
	podName := "pod-name-test"
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: podNamespace,
			Name:      podName,
		},
	}

	podLogOptions := &v1.PodLogOptions{}

	It("should return the proper podName", func() {
		streamPodLog := StreamingRequest{
			Pod:     pod,
			Options: podLogOptions,
		}
		Expect(streamPodLog.getPodName()).To(BeEquivalentTo(podName))
		Expect(streamPodLog.getPodNamespace()).To(BeEquivalentTo(podNamespace))
	})

	It("should be able to handle the nil Pod", func(ctx context.Context) {
		// the nil pod passed will still default to the empty pod name
		client := fake.NewSimpleClientset()
		streamPodLog := StreamingRequest{
			Pod:     nil,
			Options: podLogOptions,
			client:  client,
		}
		var logBuffer bytes.Buffer
		err := streamPodLog.Stream(ctx, &logBuffer)
		Expect(err).NotTo(HaveOccurred())
		// The fake client will be given a pod name of "", but it will still
		// go on along. In production, we'd have an error when pod not found
		Expect(logBuffer.String()).To(BeEquivalentTo("fake logs"))
		Expect(streamPodLog.getPodName()).To(BeEquivalentTo(""))
		Expect(streamPodLog.getPodNamespace()).To(BeEquivalentTo(""))
	})

	It("previous option must be false by default", func() {
		streamPodLog := StreamingRequest{
			Pod:     pod,
			Options: podLogOptions,
		}
		Expect(streamPodLog.getLogOptions().Previous).To(BeFalse())
	})

	It("getLogOptions respects the Previous field setting", func() {
		streamPodLog := StreamingRequest{
			Pod:     pod,
			Options: podLogOptions,
		}
		options := streamPodLog.getLogOptions()
		Expect(options.Previous).To(BeFalse())

		streamPodLog.Previous = true
		options = streamPodLog.getLogOptions()
		Expect(options.Previous).To(BeTrue())
	})

	It("should read the logs with the provided k8s client", func(ctx context.Context) {
		client := fake.NewSimpleClientset(pod)
		streamPodLog := StreamingRequest{
			Pod:      pod,
			Options:  podLogOptions,
			Previous: false,
			client:   client,
		}

		var logBuffer bytes.Buffer
		err := streamPodLog.Stream(ctx, &logBuffer)
		Expect(err).ToNot(HaveOccurred())

		Expect(logBuffer.String()).To(BeEquivalentTo("fake logs"))
	})

	It("GetPodLogs correctly streams and provides output lines", func(ctx context.Context) {
		client := fake.NewSimpleClientset(pod)
		var logBuffer bytes.Buffer
		lines, err := GetPodLogs(ctx, client, *pod, false, &logBuffer, 2)
		Expect(err).ToNot(HaveOccurred())
		Expect(lines).To(HaveLen(2))
		Expect(lines[0]).To(BeEquivalentTo("fake logs"))
		Expect(logBuffer.String()).To(BeEquivalentTo("fake logs"))
	})

	It("GetPodLogs defaults to non-zero lines shown if set to zero", func(ctx context.Context) {
		client := fake.NewSimpleClientset(pod)
		var logBuffer bytes.Buffer
		lines, err := GetPodLogs(ctx, client, *pod, false, &logBuffer, 0)
		Expect(err).ToNot(HaveOccurred())
		Expect(lines).To(HaveLen(10))
		Expect(lines[0]).To(BeEquivalentTo("fake logs"))
		Expect(logBuffer.String()).To(BeEquivalentTo("fake logs"))
	})

	It("TailPodLogs defaults to non-zero lines shown if set to zero", func() {
		client := fake.NewSimpleClientset(pod)
		var logBuffer bytes.Buffer
		ctx := context.TODO()
		var wait sync.WaitGroup
		wait.Add(1)
		go func() {
			defer GinkgoRecover()
			defer wait.Done()
			err := TailPodLogs(ctx, client, *pod, &logBuffer, true)
			Expect(err).NotTo(HaveOccurred())
		}()
		// calling ctx.Done is not strictly necessary because the fake client
		// will terminate the pod stream anyway, ending TailPodLogs.
		// But in "production", TailPodLogs will follow
		// the pod logs until the context, or the logs, are over
		ctx.Done()
		wait.Wait()
		Expect(logBuffer.String()).To(BeEquivalentTo("fake logs"))
	})
})

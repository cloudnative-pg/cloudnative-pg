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
	"bufio"
	"bytes"
	"context"
	"io"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("StreamPodLog default options", func() {
	podNamespace := "pod-test"
	podName := "pod-name-test"
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: podNamespace,
			Name:      podName,
		},
	}

	podLogOptions := &v1.PodLogOptions{}
	streamPodLog := StreamPodLog{
		Pod:     pod,
		Options: podLogOptions,
	}

	It("should return the proper podName", func() {
		Expect(streamPodLog.getPodName()).To(BeEquivalentTo(podName))
		Expect(streamPodLog.getPodNamespace()).To(BeEquivalentTo(podNamespace))
	})

	It("previous options must be false by default", func() {
		Expect(streamPodLog.Previous).To(BeFalse())
	})

	It("get PodLogOptions properly when setting Previous", func() {
		options := streamPodLog.getLogOptions()
		Expect(options.Previous).To(BeFalse())

		streamPodLog.Previous = true
		options = streamPodLog.getLogOptions()
		Expect(options.Previous).To(BeTrue())
	})

	It("it should provide the proper client", func() {
		streamPodLog.Previous = false
		client := fake.NewSimpleClientset(pod)
		streamPodLog.client = client

		logBuffer := new(bytes.Buffer)
		writer := bufio.NewWriter(logBuffer)
		streamPodLog.Writer = writer

		pods := streamPodLog.getStreamLogPod()
		err := streamPodLog.StreamPodLogs(context.TODO())
		Expect(err).ToNot(HaveOccurred())
		logs, err := pods.Stream(context.TODO())
		Expect(err).ToNot(HaveOccurred())

		rd := bufio.NewReader(logs)
		fakeLog, err := rd.ReadString('\n')
		Expect(err).To(BeEquivalentTo(io.EOF))
		Expect(fakeLog).To(BeEquivalentTo("fake logs"))
	})

	It("stream logs with a non set length", func() {
		_, err := streamPodLog.GetPodLogs(context.TODO())
		Expect(err).ToNot(HaveOccurred())
	})
})

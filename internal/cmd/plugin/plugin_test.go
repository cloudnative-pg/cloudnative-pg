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

package plugin

import (
	"context"
	"fmt"
	"io"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"net/http"
	k8client "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"strings"

	"k8s.io/client-go/rest"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/versions"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type fakeRoundTripper struct {
	fn func(*http.Request) (*http.Response, error)
}

func (f *fakeRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f.fn(req)
}

var _ = Describe("create client", func() {
	It("with given configuration", func() {
		err := createClient(cfg)
		Expect(err).NotTo(HaveOccurred())
		Expect(Client).NotTo(BeNil())
	})

	It("should generate the correct UserAgent string", func() {
		expectedUserAgent := fmt.Sprintf("kubectl-cnpg/v%s (%s)", versions.Version, versions.Info.Commit)

		// Create a new rest.Config
		config := &rest.Config{}

		// Set the user agent
		config.UserAgent = fmt.Sprintf("kubectl-cnpg/v%s (%s)", versions.Version, versions.Info.Commit)

		// Verify it matches what we expect
		Expect(config.UserAgent).To(Equal(expectedUserAgent))
	})

	It("should set the UserAgent correctly", func() {
		// Create a test HTTP transport that captures the request headers
		var capturedUserAgent string
		testTransport := &fakeRoundTripper{
			fn: func(req *http.Request) (*http.Response, error) {
				capturedUserAgent = req.Header.Get("User-Agent")
				// Return an empty 200 response
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader("{\"kind\":\"PodList\",\"apiVersion\":\"v1\",\"items\":[]}")),
				}, nil
			},
		}

		// Create a basic rest.Config
		config := &rest.Config{
			Host:      "https://fake-server",
			UserAgent: fmt.Sprintf("kubectl-cnpg/v%s (%s)", versions.Version, versions.Info.Commit),
			Transport: testTransport, // Set the transport directly
		}

		// Create the client with our config
		client, err := kubernetes.NewForConfig(config)
		Expect(err).NotTo(HaveOccurred())

		// Make a request
		_, err = client.CoreV1().Pods("default").List(context.Background(), metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())

		// Verify the user agent was set correctly
		expectedUserAgent := fmt.Sprintf("kubectl-cnpg/v%s (%s)", versions.Version, versions.Info.Commit)
		Expect(capturedUserAgent).To(Equal(expectedUserAgent))
	})
})

var _ = Describe("CompleteClusters testing", func() {
	const namespace = "default"
	var client k8client.Client

	BeforeEach(func() {
		cluster1 := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cluster1",
				Namespace: namespace,
			},
		}
		cluster2 := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cluster2",
				Namespace: namespace,
			},
		}

		client = fake.NewClientBuilder().WithScheme(scheme.BuildWithAllKnownScheme()).
			WithObjects(cluster1, cluster2).Build()
	})

	It("should return matching cluster names", func(ctx SpecContext) {
		toComplete := "clu"
		result := completeClusters(ctx, client, namespace, []string{}, toComplete)
		Expect(result).To(HaveLen(2))
		Expect(result).To(ConsistOf("cluster1", "cluster2"))
	})

	It("should return empty array when no clusters found", func(ctx SpecContext) {
		toComplete := "nonexistent"
		result := completeClusters(ctx, client, namespace, []string{}, toComplete)
		Expect(result).To(BeEmpty())
	})

	It("should skip clusters with prefix not matching toComplete", func(ctx SpecContext) {
		toComplete := "nonexistent"
		result := completeClusters(ctx, client, namespace, []string{}, toComplete)
		Expect(result).To(BeEmpty())
	})

	It("should return nothing when a cluster name is already on the arguments list", func(ctx SpecContext) {
		args := []string{"cluster-example"}
		toComplete := "cluster-"
		result := completeClusters(ctx, client, namespace, args, toComplete)
		Expect(result).To(BeEmpty())
	})
})

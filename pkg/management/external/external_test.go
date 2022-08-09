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

package external

import (
	"context"
	"io/ioutil"
	"os"
	"path"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("testing external functions", func() {
	const externalClusterName = "external-postgres-cluster"
	dir, err := ioutil.TempDir("", "external_test")
	Expect(err).ToNot(HaveOccurred())

	BeforeEach(func() {
		CustomExternalSecretsPath = dir
	})

	AfterEach(func() {
		_ = os.RemoveAll(dir)
		CustomExternalSecretsPath = ""
	})

	prepareEnvironment := func() (namespace string, passwordSecretName string) {
		By("creating the requirements")
		namespace = newFakeNamespace()
		passwordSecretName = rand.String(10)

		passwordSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      passwordSecretName,
				Namespace: namespace,
			},
			StringData: map[string]string{
				"password": "randompassword",
			},
		}

		err := k8sClient.Create(context.TODO(), passwordSecret)
		Expect(err).ToNot(HaveOccurred())

		return namespace, passwordSecretName
	}

	It("should properly construct the connection string with sslmode disable if no ssl parameter are passed", func() {
		namespace, secret := prepareEnvironment()
		connectionString, pgPass, err := ConfigureConnectionToServer(
			context.TODO(),
			k8sClient,
			namespace,
			&apiv1.ExternalCluster{
				Name: externalClusterName,
				ConnectionParameters: map[string]string{
					"host":   "random.host",
					"name":   "postgres",
					"dbname": "postgres",
				},
				Password: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: secret},
					Key:                  "password",
				},
			},
		)
		Expect(err).ToNot(HaveOccurred())
		Expect(connectionString).To(ContainSubstring("sslmode='disable'"))
		Expect(connectionString).To(ContainSubstring("random.host"))
		Expect(pgPass).To(Equal(path.Join(CustomExternalSecretsPath, externalClusterName, "pgpass")))
	})

	It("should properly construct the connection string with sslmode verify-ca the CA parameter is passed", func() {
		namespace, secret := prepareEnvironment()
		caSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "ca-secret", Namespace: namespace},
			StringData: map[string]string{
				"ca.crt": "randomtext",
			},
		}

		err := k8sClient.Create(context.TODO(), caSecret)
		Expect(err).ToNot(HaveOccurred())

		connectionString, pgPass, err := ConfigureConnectionToServer(
			context.TODO(),
			k8sClient,
			namespace,
			&apiv1.ExternalCluster{
				Name: externalClusterName,
				ConnectionParameters: map[string]string{
					"host":   "random.host",
					"name":   "postgres",
					"dbname": "postgres",
				},
				Password: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: secret},
					Key:                  "password",
				},
				SSLRootCert: &corev1.SecretKeySelector{
					Key:                  "ca.crt",
					LocalObjectReference: corev1.LocalObjectReference{Name: caSecret.Name},
				},
			},
		)
		Expect(err).ToNot(HaveOccurred())
		Expect(connectionString).To(ContainSubstring("sslmode='verify-ca'"))
		Expect(connectionString).To(ContainSubstring("random.host"))
		Expect(pgPass).To(Equal(path.Join(CustomExternalSecretsPath, externalClusterName, "pgpass")))
	})

	It("", func() {
		namespace, secret := prepareEnvironment()
		certSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "cert-secret", Namespace: namespace},
			StringData: map[string]string{
				"server.crt": "randomtext",
				"server.key": "randomtextkey",
			},
		}

		err := k8sClient.Create(context.TODO(), certSecret)
		Expect(err).ToNot(HaveOccurred())

		connectionString, pgPass, err := ConfigureConnectionToServer(
			context.TODO(),
			k8sClient,
			namespace,
			&apiv1.ExternalCluster{
				Name: externalClusterName,
				ConnectionParameters: map[string]string{
					"host":   "random.host",
					"name":   "postgres",
					"dbname": "postgres",
				},
				Password: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: secret},
					Key:                  "password",
				},
				SSLCert: &corev1.SecretKeySelector{
					Key:                  "server.crt",
					LocalObjectReference: corev1.LocalObjectReference{Name: certSecret.Name},
				},
				SSLKey: &corev1.SecretKeySelector{
					Key:                  "server.key",
					LocalObjectReference: corev1.LocalObjectReference{Name: certSecret.Name},
				},
			},
		)
		Expect(err).ToNot(HaveOccurred())
		Expect(connectionString).To(ContainSubstring("sslmode='require'"))
		Expect(connectionString).To(ContainSubstring("random.host"))
		Expect(pgPass).To(Equal(path.Join(CustomExternalSecretsPath, externalClusterName, "pgpass")))
	})
})

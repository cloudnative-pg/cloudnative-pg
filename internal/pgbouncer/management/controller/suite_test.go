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

package controller

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/scheme"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	authQueryName = "authquery"
	clientTLSName = "servertls"
	serverCAName  = "serverca"
	serverTLSName = "servertls"
	clientCAName  = "clientca"
)

func TestController(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "PGBouncer Controller Suite")
}

func buildTestEnv() (client.WithWatch, *apiv1.Pooler) {
	authQuerySecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: authQueryName, Namespace: "default"},
	}
	serverCASecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: serverCAName, Namespace: "default"},
	}
	serverCertSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: serverTLSName, Namespace: "default"},
	}
	clientCASecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: clientCAName, Namespace: "default"},
	}

	pooler := &apiv1.Pooler{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pooler", Namespace: "default"},
		Spec: apiv1.PoolerSpec{
			PgBouncer: &apiv1.PgBouncerSpec{
				AuthQuerySecret: &apiv1.LocalObjectReference{Name: authQueryName},
			},
			Cluster: apiv1.LocalObjectReference{
				Name: "cluster",
			},
		},
		Status: apiv1.PoolerStatus{
			Secrets: &apiv1.PoolerSecrets{
				ServerCA:  apiv1.SecretVersion{Name: serverCAName},
				ServerTLS: apiv1.SecretVersion{Name: serverTLSName},
				ClientCA:  apiv1.SecretVersion{Name: clientCAName},
				ClientTLS: apiv1.SecretVersion{Name: clientTLSName},
			},
		},
	}

	return fake.NewClientBuilder().WithScheme(scheme.BuildWithAllKnownScheme()).
		WithObjects(pooler, authQuerySecret, serverCASecret, serverCertSecret, clientCASecret).
		Build(), pooler
}

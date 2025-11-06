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

package pgadmin

import (
	"bytes"
	"path"
	"text/template"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/rand"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("command methods", func() {
	var cmd *command
	pgadminPassword := rand.String(12)

	BeforeEach(func() {
		cmd = &command{
			ClusterName:                   "example-cluster",
			ApplicationDatabaseSecretName: "example-secret",
			ApplicationDatabaseOwnerName:  "example-owner",
			DeploymentName:                "example-deployment",
			ConfigMapName:                 "example-configmap",
			ServiceName:                   "example-service",
			SecretName:                    "example-secret-name",
			PgadminUsername:               "example-username",
			PgadminPassword:               pgadminPassword,
			Mode:                          ModeServer, // or ModeDesktop for the desktop mode
			PgadminImage:                  "example-image",
		}
	})

	It("should generate the Deployment object", func() {
		deployment := cmd.generateDeployment()

		Expect(deployment).ToNot(BeNil())
		Expect(deployment.APIVersion).To(Equal("apps/v1"))
		Expect(deployment.Kind).To(Equal("Deployment"))
		Expect(deployment.ObjectMeta.Name).To(Equal("example-deployment"))
		Expect(deployment.ObjectMeta.Namespace).To(Equal(plugin.Namespace))
		Expect(*deployment.Spec.Replicas).To(Equal(int32(1)))
		Expect(deployment.Spec.Selector.MatchLabels).To(HaveKeyWithValue("app", "example-deployment"))

		podTemplate := deployment.Spec.Template
		Expect(podTemplate.ObjectMeta.Labels).To(HaveKeyWithValue("app", "example-deployment"))
		Expect(podTemplate.Spec.Containers).To(HaveLen(1))

		container := podTemplate.Spec.Containers[0]
		Expect(container.Image).To(Equal("example-image"))
		Expect(container.Name).To(Equal("pgadmin4"))
		Expect(container.Ports).To(HaveLen(1))
		Expect(container.Ports[0].Name).To(Equal("http"))
		Expect(container.Ports[0].ContainerPort).To(Equal(int32(80)))

		envVars := container.Env
		Expect(envVars).To(ContainElement(corev1.EnvVar{
			Name:  "PGADMIN_SERVER_JSON_FILE",
			Value: path.Join("/config", "servers.json"),
		}))
		Expect(envVars).To(ContainElement(corev1.EnvVar{Name: "PGADMIN_CONFIG_SERVER_MODE", Value: "True"}))
		Expect(envVars).To(ContainElement(corev1.EnvVar{Name: "PGADMIN_CONFIG_MASTER_PASSWORD_REQUIRED", Value: "False"}))

		volumeMounts := container.VolumeMounts
		Expect(volumeMounts).To(ContainElement(corev1.VolumeMount{Name: "pgadmin-cfg", MountPath: "/config"}))
		Expect(volumeMounts).To(ContainElement(corev1.VolumeMount{Name: "app-secret", MountPath: "/secret"}))

		readinessProbe := container.ReadinessProbe
		Expect(readinessProbe).ToNot(BeNil())
		Expect(readinessProbe.HTTPGet).ToNot(BeNil())
		Expect(readinessProbe.HTTPGet.Path).To(Equal("/"))
		Expect(readinessProbe.HTTPGet.Port).To(Equal(intstr.FromInt32(80)))
	})

	It("should generate the Service object", func() {
		service := cmd.generateService()

		Expect(service).ToNot(BeNil())
		Expect(service.APIVersion).To(Equal("v1"))
		Expect(service.Kind).To(Equal("Service"))
		Expect(service.ObjectMeta.Name).To(Equal("example-service"))
		Expect(service.ObjectMeta.Namespace).To(Equal(plugin.Namespace))

		serviceSpec := service.Spec
		Expect(serviceSpec.Ports).To(HaveLen(1))
		Expect(serviceSpec.Ports[0].Port).To(Equal(int32(80)))
		Expect(serviceSpec.Ports[0].Protocol).To(Equal(corev1.ProtocolTCP))
		Expect(serviceSpec.Ports[0].TargetPort).To(Equal(intstr.FromInt32(80)))
		Expect(serviceSpec.Selector).To(HaveKeyWithValue("app", "example-deployment"))
	})

	It("should generate the configuration template", func() {
		data := map[string]string{
			"ClusterName":                  "example-cluster",
			"ApplicationDatabaseOwnerName": "example-owner",
			"Mode":                         "desktop",
		}

		renderedTemplate := renderTemplate(configurationTemplate, data)

		Expect(renderedTemplate).To(ContainSubstring(`"Username": "example-owner"`))
		Expect(renderedTemplate).To(ContainSubstring(`"PasswordExecCommand": "cat /secret/password"`))
	})

	It("should generate the ConfigMap object", func() {
		configMap, err := cmd.generateConfigMap()

		Expect(err).ToNot(HaveOccurred())

		Expect(configMap).ToNot(BeNil())
		Expect(configMap.APIVersion).To(Equal("v1"))
		Expect(configMap.Kind).To(Equal("ConfigMap"))
		Expect(configMap.ObjectMeta.Name).To(Equal("example-configmap"))
		Expect(configMap.ObjectMeta.Namespace).To(Equal(plugin.Namespace))

		Expect(configMap.Data).To(HaveKey("servers.json"))
		Expect(configMap.Data["servers.json"]).To(ContainSubstring(`"Username": "example-owner"`))
	})

	It("should generate the Secret object", func() {
		secret := cmd.generateSecret()

		Expect(secret).ToNot(BeNil())
		Expect(secret.APIVersion).To(Equal("v1"))
		Expect(secret.Kind).To(Equal("Secret"))
		Expect(secret.ObjectMeta.Name).To(Equal("example-secret-name"))
		Expect(secret.ObjectMeta.Namespace).To(Equal(plugin.Namespace))

		Expect(secret.StringData).To(HaveKeyWithValue("username", "example-username"))
		Expect(secret.StringData).To(HaveKeyWithValue("password", pgadminPassword))
	})
})

func renderTemplate(t *template.Template, data map[string]string) string {
	var renderedTemplate bytes.Buffer
	err := t.Execute(&renderedTemplate, data)
	Expect(err).ToNot(HaveOccurred())
	return renderedTemplate.String()
}

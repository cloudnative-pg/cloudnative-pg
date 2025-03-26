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

package podspec

import (
	corev1 "k8s.io/api/core/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Pod template builder", func() {
	It("works without a Pod template", func() {
		Expect(NewFrom(nil).status).ToNot(BeNil())
	})

	It("works with a Pod template", func() {
		Expect(NewFrom(&apiv1.PodTemplateSpec{}).status).ToNot(BeNil())
	})

	It("adds annotations", func() {
		Expect(New().WithAnnotation("test", "annotation").Build().ObjectMeta.Annotations["test"]).
			To(Equal("annotation"))
	})

	It("adds volumes", func() {
		template := New().
			WithVolume(&corev1.Volume{
				Name: "test",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: "test",
					},
				},
			}).
			Build()

		Expect(template.Spec.Volumes[0].Name).To(Equal("test"))
		Expect(template.Spec.Volumes[0].VolumeSource.Secret.SecretName).To(Equal("test"))
	})

	It("replaces existing volumes", func() {
		template := New().
			WithVolume(&corev1.Volume{
				Name: "test",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: "test",
					},
				},
			}).
			WithVolume(&corev1.Volume{
				Name: "test",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: "test2",
					},
				},
			}).
			Build()

		Expect(template.Spec.Volumes[0].Name).To(Equal("test"))
		Expect(template.Spec.Volumes[0].VolumeSource.Secret.SecretName).To(Equal("test2"))
	})

	It("adds containers when needed", func() {
		template := New().
			WithContainer("first").
			WithContainer("first").
			Build()
		Expect(template.Spec.Containers[0].Name).To(Equal("first"))
	})

	It("correctly set the container image when not set", func() {
		template := New().
			WithContainerImage("first", "ubuntu:bionic", false).
			WithContainerImage("first", "ubuntu:focal", false).
			Build()
		Expect(template.Spec.Containers[0].Name).To(Equal("first"))
		Expect(template.Spec.Containers[0].Image).To(Equal("ubuntu:bionic"))
	})

	It("correctly override the container image when set", func() {
		template := New().
			WithContainerImage("first", "ubuntu:bionic", false).
			WithContainerImage("first", "ubuntu:focal", true).
			Build()
		Expect(template.Spec.Containers[0].Name).To(Equal("first"))
		Expect(template.Spec.Containers[0].Image).To(Equal("ubuntu:focal"))
	})

	It("correctly set the container command when not set", func() {
		template := New().
			WithContainerCommand("first", []string{"test"}, false).
			WithContainerCommand("first", []string{"toast"}, false).
			Build()
		Expect(template.Spec.Containers[0].Name).To(Equal("first"))
		Expect(template.Spec.Containers[0].Command).To(Equal([]string{"test"}))
	})

	It("correctly override the container command when not set", func() {
		template := New().
			WithContainerCommand("first", []string{"test"}, false).
			WithContainerCommand("first", []string{"toast"}, true).
			Build()
		Expect(template.Spec.Containers[0].Name).To(Equal("first"))
		Expect(template.Spec.Containers[0].Command).To(Equal([]string{"toast"}))
	})

	It("correctly add a volumeMount to a container", func() {
		template := New().
			WithContainerVolumeMount("first", &corev1.VolumeMount{
				Name:      "volume",
				MountPath: "/volume",
			}, false).
			Build()
		Expect(template.Spec.Containers[0].VolumeMounts[0].Name).To(Equal("volume"))
		Expect(template.Spec.Containers[0].VolumeMounts[0].MountPath).To(Equal("/volume"))
	})

	It("correctly overwrite a volumeMount to a container", func() {
		template := New().
			WithContainerVolumeMount("first", &corev1.VolumeMount{
				Name:      "volume",
				MountPath: "/volume",
			}, false).
			WithContainerVolumeMount("first", &corev1.VolumeMount{
				Name:      "volume",
				MountPath: "/volume/mount",
			}, true).
			Build()
		Expect(template.Spec.Containers[0].VolumeMounts[0].Name).To(Equal("volume"))
		Expect(template.Spec.Containers[0].VolumeMounts[0].MountPath).To(Equal("/volume/mount"))
	})

	It("correctly add a container port to a container", func() {
		template := New().
			WithContainerPort("first", &corev1.ContainerPort{
				Name:          "postgresql",
				ContainerPort: 5432,
			}).
			Build()
		Expect(template.Spec.Containers[0].Ports[0].Name).To(Equal("postgresql"))
		Expect(template.Spec.Containers[0].Ports[0].ContainerPort).To(BeEquivalentTo(5432))
	})

	It("correctly override a container port to a container", func() {
		template := New().
			WithContainerPort("first", &corev1.ContainerPort{
				Name:          "postgresql",
				ContainerPort: 5432,
			}).
			WithContainerPort("first", &corev1.ContainerPort{
				Name:          "postgresql",
				ContainerPort: 6432,
			}).
			Build()
		Expect(template.Spec.Containers[0].Ports[0].Name).To(Equal("postgresql"))
		Expect(template.Spec.Containers[0].Ports[0].ContainerPort).To(BeEquivalentTo(6432))
	})

	It("adds init containers when needed", func() {
		template := New().
			WithInitContainer("first").
			WithInitContainer("first").
			Build()
		Expect(template.Spec.InitContainers[0].Name).To(Equal("first"))
	})

	It("correctly set the init container image when not set", func() {
		template := New().
			WithInitContainerImage("first", "ubuntu:bionic", false).
			WithInitContainerImage("first", "ubuntu:focal", false).
			Build()
		Expect(template.Spec.InitContainers[0].Name).To(Equal("first"))
		Expect(template.Spec.InitContainers[0].Image).To(Equal("ubuntu:bionic"))
	})

	It("correctly overwrite the init the container image when set", func() {
		template := New().
			WithInitContainerImage("first", "ubuntu:bionic", false).
			WithInitContainerImage("first", "ubuntu:focal", true).
			Build()
		Expect(template.Spec.InitContainers[0].Name).To(Equal("first"))
		Expect(template.Spec.InitContainers[0].Image).To(Equal("ubuntu:focal"))
	})

	It("correctly add a volumeMount to an init container", func() {
		template := New().
			WithInitContainerVolumeMount("first", &corev1.VolumeMount{
				Name:      "volume",
				MountPath: "/volume",
			}, false).
			Build()
		Expect(template.Spec.InitContainers[0].VolumeMounts[0].Name).To(Equal("volume"))
		Expect(template.Spec.InitContainers[0].VolumeMounts[0].MountPath).To(Equal("/volume"))
	})

	It("correctly overwrite a volumeMount to an init container", func() {
		template := New().
			WithInitContainerVolumeMount("first", &corev1.VolumeMount{
				Name:      "volume",
				MountPath: "/volume",
			}, false).
			WithInitContainerVolumeMount("first", &corev1.VolumeMount{
				Name:      "volume",
				MountPath: "/volume/mount",
			}, true).
			Build()
		Expect(template.Spec.InitContainers[0].VolumeMounts[0].Name).To(Equal("volume"))
		Expect(template.Spec.InitContainers[0].VolumeMounts[0].MountPath).To(Equal("/volume/mount"))
	})

	It("correctly set the init container command when not set", func() {
		template := New().
			WithInitContainerCommand("first", []string{"test"}, false).
			WithInitContainerCommand("first", []string{"toast"}, false).
			Build()
		Expect(template.Spec.InitContainers[0].Name).To(Equal("first"))
		Expect(template.Spec.InitContainers[0].Command).To(Equal([]string{"test"}))
	})

	It("correctly override the init container command when set", func() {
		template := New().
			WithInitContainerCommand("first", []string{"test"}, false).
			WithInitContainerCommand("first", []string{"toast"}, true).
			Build()
		Expect(template.Spec.InitContainers[0].Name).To(Equal("first"))
		Expect(template.Spec.InitContainers[0].Command).To(Equal([]string{"toast"}))
	})

	It("correctly set a container environment variable when not set", func() {
		template := New().
			WithContainerCommand("first", []string{"test"}, false).
			WithContainerEnv("first", corev1.EnvVar{Name: "one", Value: "one"}, false).
			WithContainerEnv("first", corev1.EnvVar{Name: "one", Value: "two"}, false).
			Build()
		Expect(template.Spec.Containers[0].Name).To(Equal("first"))
		Expect(template.Spec.Containers[0].Env[0].Name).To(Equal("one"))
		Expect(template.Spec.Containers[0].Env[0].Value).To(Equal("one"))
	})

	It("correctly override a container environment variable when set", func() {
		template := New().
			WithContainerCommand("first", []string{"test"}, false).
			WithContainerEnv("first", corev1.EnvVar{Name: "one", Value: "one"}, false).
			WithContainerEnv("first", corev1.EnvVar{Name: "one", Value: "two"}, true).
			Build()
		Expect(template.Spec.Containers[0].Name).To(Equal("first"))
		Expect(template.Spec.Containers[0].Env[0].Name).To(Equal("one"))
		Expect(template.Spec.Containers[0].Env[0].Value).To(Equal("two"))
	})
})

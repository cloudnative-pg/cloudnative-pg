/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package specs

import (
	corev1 "k8s.io/api/core/v1"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Container merging", func() {
	base := []corev1.Container{
		{
			Name:  "one",
			Image: "base/one",
		},
		{
			Name:  "two",
			Image: "base/two",
		},
	}
	changes := []corev1.Container{
		{
			Name:  "two",
			Image: "base/two:2",
		},
		{
			Name:  "three",
			Image: "base/three",
		},
	}

	It("override containers with the same name", func() {
		result := UpsertContainers(base, changes)
		Expect(len(result)).To(Equal(3))
		Expect(result[1].Name).To(Equal("two"))
		Expect(result[1].Image).To(Equal("base/two:2"))
		Expect(result[2].Name).To(Equal("three"))
	})

	It("is idempotent", func() {
		result := UpsertContainers(base, changes)
		resultAfter := UpsertContainers(result, changes)
		Expect(result).To(Equal(resultAfter))
	})
})

var _ = Describe("Volume merging", func() {
	base := []corev1.Volume{
		{
			Name: "one",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: "one",
				},
			},
		},
		{
			Name: "two",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: "two",
				},
			},
		},
	}
	changes := []corev1.Volume{
		{
			Name: "two",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: "two:2",
				},
			},
		},
		{
			Name: "three",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: "three",
				},
			},
		},
	}

	It("override Volumes with the same name", func() {
		result := UpsertVolumes(base, changes)
		Expect(len(result)).To(Equal(3))
		Expect(result[1].Name).To(Equal("two"))
		Expect(result[1].VolumeSource.PersistentVolumeClaim.ClaimName).To(Equal("two:2"))
		Expect(result[2].Name).To(Equal("three"))
	})

	It("is idempotent", func() {
		result := UpsertVolumes(base, changes)
		resultAfter := UpsertVolumes(result, changes)
		Expect(result).To(Equal(resultAfter))
	})
})

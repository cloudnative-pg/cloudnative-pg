/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package specs

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/2ndquadrant/cloud-native-postgresql/api/v1alpha1"
	"github.com/2ndquadrant/cloud-native-postgresql/pkg/versions"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestPodProperties(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "PostgreSQL pods properties")
}

var _ = Describe("Serial ID of a PostgreSQL node", func() {
	cluster := v1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "clusterName",
			Namespace: "default",
		},
	}
	firstPod := CreatePrimaryPod(cluster, 1)

	It("can be extracted from a Pod", func() {
		result, err := GetNodeSerial(firstPod.ObjectMeta)
		Expect(err).To(BeNil())
		Expect(result).To(Equal(1))
	})

	It("cannot be extracted if the Pod is not created by the operator", func() {
		pod := corev1.Pod{}
		_, err := GetNodeSerial(pod.ObjectMeta)
		Expect(err).ToNot(BeNil())
	})

	It("cannot be extracted if the Pod is created by the operator but has a wrong label", func() {
		brokenPod := firstPod.DeepCopy()
		brokenPod.Annotations[ClusterSerialAnnotationName] = "thisisatest"

		_, err := GetNodeSerial(brokenPod.ObjectMeta)
		Expect(err).ToNot(BeNil())
	})
})

var _ = Describe("Check if it a primary or a replica", func() {
	cluster := v1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "clusterName",
			Namespace: "default",
		},
	}
	primaryPod := CreatePrimaryPod(cluster, 1)
	replicaPod := JoinReplicaInstance(cluster, 2)

	It("a primary is detected as a primary", func() {
		Expect(IsPodPrimary(*primaryPod)).To(BeTrue())
		Expect(IsPodStandby(*primaryPod)).To(BeFalse())
	})

	It("a replica is detected as a replica", func() {
		Expect(IsPodPrimary(*replicaPod)).To(BeFalse())
		Expect(IsPodStandby(*replicaPod)).To(BeTrue())
	})
})

var _ = Describe("Extract the used image name", func() {
	cluster := v1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "clusterName",
			Namespace: "default",
		},
	}
	pod := CreatePrimaryPod(cluster, 1)

	It("extract the default image name", func() {
		Expect(GetPostgreSQLImageName(*pod)).To(Equal(versions.GetDefaultImageName()))
	})
})

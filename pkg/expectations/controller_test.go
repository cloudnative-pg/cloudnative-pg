/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package expectations

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Key function", func() {
	firstCluster := &apiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "first",
			Namespace: "default",
		},
	}
	secondCluster := &apiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "second",
			Namespace: "default",
		},
	}
	thirdCluster := &apiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "first",
			Namespace: "another",
		},
	}

	It("should return a string from a k8s object", func() {
		Expect(KeyFunc(firstCluster)).NotTo(BeNil())
	})

	It("should return different strings for different objects", func() {
		firstKey := KeyFunc(firstCluster)
		Expect(KeyFunc(secondCluster)).ToNot(Equal(firstKey))
		Expect(KeyFunc(thirdCluster)).ToNot(Equal(firstKey))
	})
})

var _ = Describe("Expectation controller", func() {
	expectations := NewControllerExpectations()
	key := "thisKeyHere"
	anotherKey := "anotherKey"

	It("should track object creation", func() {
		Expect(expectations.ExpectCreations(key, 3)).To(Succeed())
		expectations.LowerExpectations(key, 3, 0)
		Expect(expectations.SatisfiedExpectations(key)).To(BeTrue())
	})

	It("should track object deletion", func() {
		Expect(expectations.ExpectDeletions(key, 3)).To(Succeed())
		expectations.LowerExpectations(key, 0, 3)
		Expect(expectations.SatisfiedExpectations(key)).To(BeTrue())
	})

	It("should warn when expectations are not really met", func() {
		Expect(expectations.ExpectCreations(key, 3)).To(Succeed())
		expectations.LowerExpectations(key, 1, 0)
		Expect(expectations.SatisfiedExpectations(key)).NotTo(BeTrue())
	})

	It("should not mix different keys", func() {
		Expect(expectations.ExpectCreations(key, 3)).To(Succeed())
		expectations.LowerExpectations(key, 3, 0)
		Expect(expectations.ExpectCreations(anotherKey, 3)).To(Succeed())

		Expect(expectations.SatisfiedExpectations(key)).To(BeTrue())
		Expect(expectations.SatisfiedExpectations(anotherKey)).NotTo(BeTrue())

	})
})

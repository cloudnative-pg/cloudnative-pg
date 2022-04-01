/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package controllers

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/specs"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"
)

var _ = Describe("pooler_predicates unit tests", func() {
	It("makes sure isUsefulPoolerSecret works correctly", func() {
		namespace := newFakeNamespace()
		cluster := newFakeCNPCluster(namespace)
		pooler := newFakePooler(cluster)

		By("making sure it returns true for owned secrets", func() {
			secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: rand.String(10), Namespace: namespace}}
			utils.SetAsOwnedBy(&secret.ObjectMeta, pooler.ObjectMeta, pooler.TypeMeta)
			isUseful := isUsefulPoolerSecret(secret)
			Expect(isUseful).To(BeTrue())
		})

		By("making sure it returns true for secrets with reload label", func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rand.String(10),
					Namespace: namespace,
					Labels: map[string]string{
						specs.WatchedLabelName: "true",
					},
				},
			}
			isUseful := isUsefulPoolerSecret(secret)
			Expect(isUseful).To(BeTrue())
		})

		By("making sure it returns false with not owned secrets", func() {
			secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: rand.String(10), Namespace: namespace}}
			isUseful := isUsefulPoolerSecret(secret)
			Expect(isUseful).To(BeFalse())
		})
	})

	It("makes sure isOwnedByPoolerOrSatisfiesPredicate works correctly", func() {
		namespace := newFakeNamespace()
		cluster := newFakeCNPCluster(namespace)
		pooler := newFakePooler(cluster)

		secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: rand.String(10), Namespace: namespace}}
		utils.SetAsOwnedBy(&secret.ObjectMeta, pooler.ObjectMeta, pooler.TypeMeta)
		isOwnedByPoolerOrSatisfiesPredicate(secret, func(object client.Object) bool {
			return false
		})
	})
})

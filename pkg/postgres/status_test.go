/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/
package postgres

import (
	"sort"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("PostgreSQL status", func() {
	list := PostgresqlStatusList{
		Items: []PostgresqlStatus{
			{
				PodName:     "server-3",
				IsPrimary:   false,
				ReceivedLsn: "1/23",
				ReplayLsn:   "1/22",
			},
			{
				PodName:     "server-2",
				IsPrimary:   false,
				ReceivedLsn: "1/21",
			},
			{
				PodName:   "server-1",
				IsPrimary: true,
			},
			{
				PodName:     "server-4",
				IsPrimary:   false,
				ReceivedLsn: "1/23",
				ReplayLsn:   "1/23",
			},
		},
	}

	Describe("when sorted", func() {
		sort.Sort(&list)

		It("primary servers are come first", func() {
			Expect(list.Items[0].IsPrimary).To(BeTrue())
			Expect(list.Items[0].PodName).To(Equal("server-1"))
		})

		It("contains the more updated server as the second element", func() {
			Expect(list.Items[1].IsPrimary).To(BeFalse())
			Expect(list.Items[1].PodName).To(Equal("server-4"))
		})

		It("contains other servers considering their status", func() {
			Expect(list.Items[2].PodName).To(Equal("server-3"))
			Expect(list.Items[3].PodName).To(Equal("server-2"))
		})
	})
})

var _ = Describe("PostgreSQL instance ordering", func() {
	podList := []corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod1",
				Namespace: "default",
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod2",
				Namespace: "default",
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod3",
				Namespace: "default",
			},
		},
	}

	It("select a Pod by its name", func() {
		Expect(ExtractPodFromList(podList, "pod2").Name).To(Equal("pod2"))
		Expect(ExtractPodFromList(podList, "nonexisteng")).To(BeNil())
	})
})

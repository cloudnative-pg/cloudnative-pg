/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package postgres

import (
	"fmt"
	"sort"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("PostgreSQL status", func() {
	list := PostgresqlStatusList{
		Items: []PostgresqlStatus{
			{
				PodName:   "server-04",
				ExecError: fmt.Errorf("cannot find postgres container"),
				IsReady:   true,
			},
			{
				PodName:     "server-06",
				IsPrimary:   false,
				ReceivedLsn: "1/23",
				ReplayLsn:   "1/22",
				IsReady:     false,
			},
			{
				PodName:     "server-30",
				IsPrimary:   false,
				ReceivedLsn: "1/23",
				ReplayLsn:   "1/22",
				IsReady:     true,
			},
			{
				PodName:     "server-20",
				IsPrimary:   false,
				ReceivedLsn: "1/21",
				IsReady:     true,
			},
			{
				PodName:   "server-10",
				IsPrimary: true,
				IsReady:   true,
			},
			{
				PodName:     "server-40",
				IsPrimary:   false,
				ReceivedLsn: "1/23",
				ReplayLsn:   "1/23",
				IsReady:     true,
			},
		},
	}

	It("check for errors in the Pod status", func() {
		greenList := PostgresqlStatusList{
			Items: []PostgresqlStatus{
				{
					PodName:     "server-20",
					IsPrimary:   false,
					ReceivedLsn: "1/21",
					IsReady:     true,
				},
				{
					PodName:   "server-10",
					IsPrimary: true,
					IsReady:   true,
				},
			},
		}

		Expect(list.IsComplete()).To(BeFalse())
		Expect(greenList.IsComplete()).To(BeTrue())
	})

	Describe("when sorted", func() {
		sort.Sort(&list)

		It("primary servers are come first", func() {
			Expect(list.Items[0].IsPrimary).To(BeTrue())
			Expect(list.Items[0].PodName).To(Equal("server-10"))
		})

		It("contains the more updated server as the second element", func() {
			Expect(list.Items[1].IsPrimary).To(BeFalse())
			Expect(list.Items[1].PodName).To(Equal("server-40"))
		})

		It("contains other servers considering their status", func() {
			Expect(list.Items[2].PodName).To(Equal("server-30"))
			Expect(list.Items[3].PodName).To(Equal("server-20"))
		})

		It("put the non-ready servers after the ready ones", func() {
			Expect(list.Items[4].PodName).To(Equal("server-06"))
			Expect(list.Items[4].PodName).To(Equal("server-06"))
		})

		It("put the incomplete statuses at the bottom of the list", func() {
			Expect(list.Items[5].PodName).To(Equal("server-04"))
			Expect(list.Items[5].PodName).To(Equal("server-04"))
		})
	})
})

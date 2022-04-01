/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package concurrency

import (
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Executed", func() {
	It("correctly ignores wait if already executed", func() {
		i := NewExecuted()
		i.Broadcast()
		Expect(i.done).To(BeTrue())
		i.Wait()
		Expect(i.done).To(BeTrue())
	})
	It("correctly waits between goroutines", func() {
		i := NewExecuted()
		wg := sync.WaitGroup{}
		wg.Add(1)
		go func() {
			defer GinkgoRecover()
			defer wg.Done()
			i.Wait()
			Expect(i.done).To(BeTrue())
		}()
		Expect(i.done).To(BeFalse())
		i.Broadcast()
		Expect(i.done).To(BeTrue())
		wg.Wait()
	})
})

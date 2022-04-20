/*
Copyright The CloudNativePG Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
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
			defer wg.Done()
			defer GinkgoRecover()
			i.Wait()
			Expect(i.done).To(BeTrue())
		}()
		Expect(i.done).To(BeFalse())
		i.Broadcast()
		Expect(i.done).To(BeTrue())
		wg.Wait()
	})
})

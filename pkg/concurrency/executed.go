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
)

// Executed can be used to wait for something to be executed,
// it has a similar semantics to sync.Cond,
// but with a way to know whether it was already executed once
// and waiting only if that's not the case.
type Executed struct {
	cond sync.Cond
	done bool
}

// MultipleExecuted can be used to wrap multiple Executed conditions that
// should all be waited
type MultipleExecuted []*Executed

// Wait waits for the execution for all the conditions in a MultipleExecuted
func (m MultipleExecuted) Wait() {
	for _, cond := range m {
		cond.Wait()
	}
}

// NewExecuted creates a new Executed
func NewExecuted() *Executed {
	return &Executed{
		cond: *sync.NewCond(&sync.Mutex{}),
		done: false,
	}
}

// Wait waits for execution
func (i *Executed) Wait() {
	i.cond.L.Lock()
	defer i.cond.L.Unlock()
	if !i.done {
		i.cond.Wait()
	}
}

// Broadcast broadcasts execution to waiting goroutines
func (i *Executed) Broadcast() {
	i.cond.L.Lock()
	defer i.cond.L.Unlock()
	if !i.done {
		i.done = true
		i.cond.Broadcast()
	}
}

/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package concurrency

import "sync"

// Executed can be used to wait for something to be executed,
// it has a similar semantics to sync.Cond,
// but with a way to know whether it was already executed once
// and waiting only if that's not the case.
type Executed struct {
	done bool
	cond sync.Cond
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

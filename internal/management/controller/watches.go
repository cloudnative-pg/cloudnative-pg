/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package controller

import (
	"sync"

	"k8s.io/apimachinery/pkg/watch"
)

// WatchCollection represent a collection on watches that are multiplexed
// in a single result channel
type WatchCollection struct {
	watches           []watch.Interface
	done              chan interface{}
	multiplexedStream chan watch.Event
}

// NewWatchCollection create a new collection of watches
func NewWatchCollection(watches ...watch.Interface) *WatchCollection {
	w := &WatchCollection{
		watches: watches,
		done:    make(chan interface{}),
	}
	w.multiplexedStream = watchFanIn(watches, w.done)
	return w
}

// ResultChan get a channel multiplexing events from all the watches in
// the collection.
func (r *WatchCollection) ResultChan() <-chan watch.Event {
	return r.multiplexedStream
}

// Stop will close the channel returned by ResultChan(). Releases
// any resources used by the watch.
func (r *WatchCollection) Stop() {
	select {
	case <-r.done:
		// the channel is closed. do nothing
	default:
		for _, w := range r.watches {
			w.Stop()
		}
		close(r.done)
	}
}

// watchFanIn multiplexes a series of watches into a single channel, with
// a done out-of-band signal
func watchFanIn(watches []watch.Interface, done chan interface{}) chan watch.Event {
	var wg sync.WaitGroup

	multiplexedChannel := make(chan watch.Event)

	multiplex := func(w watch.Interface) {
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			case o, ok := <-w.ResultChan():
				if !ok {
					return
				}
				multiplexedChannel <- o
			}
		}
	}

	wg.Add(len(watches))
	for _, w := range watches {
		go multiplex(w)
	}

	go func() {
		wg.Wait()
		close(multiplexedChannel)
	}()

	return multiplexedChannel
}

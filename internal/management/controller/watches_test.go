/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package controller

import (
	"fmt"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/watch"

	. "github.com/onsi/ginkgo"
)

type fakeWatch struct {
	eventChan chan watch.Event
}

func newFakeWatch() *fakeWatch {
	return &fakeWatch{
		eventChan: make(chan watch.Event),
	}
}

func (f *fakeWatch) Stop() {
	close(f.eventChan)
}

func (f *fakeWatch) ResultChan() <-chan watch.Event {
	return f.eventChan
}

func (f *fakeWatch) fireEvent() {
	go func() { f.eventChan <- watch.Event{} }()
}

var _ = Describe("how WatchCollection fan-in works", func() {

	It("closes the channel when it watches no channels", func() {
		wc := NewWatchCollection()
		assertEventuallyClosed(wc)
	})

	It("receives a fired event", func() {
		f := newFakeWatch()
		wc := NewWatchCollection(f)
		assertEventuallyEmpty(wc)
		f.fireEvent()
		assertEventuallyNotEmpty(wc)
	})

	It("doesn't receive any event when the source is closed", func() {
		f := newFakeWatch()
		wc := NewWatchCollection(f)
		f.Stop()
		assertEventuallyClosed(wc)
	})

	It("closes the channels when stopped", func() {
		f := newFakeWatch()
		wc := NewWatchCollection(f)
		wc.Stop()
		assertEventuallyClosed(f)
		assertEventuallyClosed(wc)
	})

	It("doesn't panic if stopped twice", func() {
		f := newFakeWatch()
		wc := NewWatchCollection(f)
		wc.Stop()
		wc.Stop()
	})
})

func BenchmarkWatchCollection(b *testing.B) {
	f := newFakeWatch()
	wc := NewWatchCollection(f)
	for n := 0; n < b.N; n++ {
		f.fireEvent()
		<-wc.ResultChan()
	}
}

func BenchmarkDirect(b *testing.B) {
	eventChan := make(chan watch.Event)
	for n := 0; n < b.N; n++ {
		go func() { eventChan <- watch.Event{} }()
		<-eventChan
	}
}

const watchCollectionTimeout = 1 * time.Millisecond

func assertEventuallyEmpty(w watch.Interface) {
	select {
	case x, ok := <-w.ResultChan():
		if ok {
			Fail(fmt.Sprintf("empty channel was expected but returned %v", x), 1)
		} else {
			Fail("empty channel was expected but the channel is closed", 1)
		}
	case <-time.After(watchCollectionTimeout):
		return
	}
}

func assertEventuallyNotEmpty(w watch.Interface) {
	select {
	case _, ok := <-w.ResultChan():
		if ok {
			return
		}
		Fail("not empty channel was expected but the channel is closed", 1)
	case <-time.After(watchCollectionTimeout):
		Fail(fmt.Sprintf("not empty channel was expected but the channel is empty after %s", watchCollectionTimeout), 1)
	}
}

func assertEventuallyClosed(w watch.Interface) {
	select {
	case x, ok := <-w.ResultChan():
		if !ok {
			return
		}
		Fail(fmt.Sprintf("closed channel was expected but returned %v", x), 1)
	case <-time.After(watchCollectionTimeout):
		Fail(fmt.Sprintf("closed channel was expected but the channel is still open after %s", watchCollectionTimeout), 1)
	}
}

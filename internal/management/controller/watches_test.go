/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package controller

import (
	"testing"

	"k8s.io/apimachinery/pkg/watch"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
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
		Eventually(wc.ResultChan()).Should(BeClosed())
	})

	It("receives a fired event", func() {
		f := newFakeWatch()
		wc := NewWatchCollection(f)
		Expect(wc.ResultChan()).ShouldNot(Receive())
		f.fireEvent()
		Eventually(wc.ResultChan()).Should(Receive())
	})

	It("doesn't receive any event when the source is closed", func() {
		f := newFakeWatch()
		wc := NewWatchCollection(f)
		f.Stop()
		Eventually(wc.ResultChan()).Should(BeClosed())
	})

	It("closes the channels when stopped", func() {
		f := newFakeWatch()
		wc := NewWatchCollection(f)
		wc.Stop()
		Eventually(f.ResultChan()).Should(BeClosed())
		Eventually(wc.ResultChan()).Should(BeClosed())
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

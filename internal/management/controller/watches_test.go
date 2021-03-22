/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package controller

import (
	"k8s.io/apimachinery/pkg/watch"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

// The following unit test want to test private function "watchFanIn"

type fakeWatch struct {
	eventChan chan watch.Event
}

func newFakeWatch() *fakeWatch {
	return &fakeWatch{
		eventChan: make(chan watch.Event, 1),
	}
}

func (f *fakeWatch) Stop() {
	close(f.eventChan)
}

func (f *fakeWatch) ResultChan() <-chan watch.Event {
	return f.eventChan
}

func (f *fakeWatch) fireEvent() {
	f.eventChan <- watch.Event{}
}

var _ = Describe("WatchCollection fan-in", func() {
	It("watches an empty slice", func() {
		wc := NewWatchCollection()
		Expect(wc.ResultChan()).To(BeEmpty())
	})

	It("watches a fakeWatch and receive an event", func() {
		f := newFakeWatch()
		wc := NewWatchCollection(f)
		Expect(wc.ResultChan()).To(BeEmpty())
		f.fireEvent()
		Expect(<-wc.ResultChan()).ToNot(BeNil())
	})
})

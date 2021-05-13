/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package logpipe

import (
	"errors"
	"io/ioutil"
	"os"
	"strings"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

// SpyRecordWriter is an implementation of the RecordWriter interface
// keeping track of the generated records
type SpyRecordWriter struct {
	records []Record
}

// Write write the PostgreSQL log record to the instance manager logger
func (writer *SpyRecordWriter) Write(record *Record) {
	writer.records = append(writer.records, *record)
}

var _ = Describe("CSV file reader", func() {
	It("can read multiple CSV lines", func() {
		f, err := os.Open("testdata/two_lines.csv")
		defer func() {
			_ = f.Close()
		}()
		Expect(err).ToNot(HaveOccurred())

		spy := SpyRecordWriter{}
		Expect(streamLogFromCSVFile(f, &spy)).To(Succeed())
		Expect(len(spy.records)).To(Equal(2))
	})

	It("can read multiple CSV lines on PostgreSQL version <= 12", func() {
		f, err := os.Open("testdata/two_lines_12.csv")
		defer func() {
			_ = f.Close()
		}()
		Expect(err).ToNot(HaveOccurred())

		spy := SpyRecordWriter{}
		Expect(streamLogFromCSVFile(f, &spy)).To(Succeed())
		Expect(len(spy.records)).To(Equal(2))
	})

	When("correctly handles a record with an invalid number of fields", func() {
		inputBuffer, err := ioutil.ReadFile("testdata/one_line.csv")
		Expect(err).ShouldNot(HaveOccurred())
		input := strings.TrimRight(string(inputBuffer), " \n")

		It("there are too many fields", func() {
			spy := SpyRecordWriter{}

			longerInput := input + ",test"
			reader := strings.NewReader(longerInput)
			err := streamLogFromCSVFile(reader, &spy)
			Expect(err).Should(HaveOccurred())
			Expect(len(spy.records)).To(Equal(0))

			extendedError := &ErrFieldCountExtended{}
			Expect(errors.As(err, &extendedError)).To(BeTrue())
			Expect(len(extendedError.Fields)).To(Equal(FieldsPerRecord13 + 1))
		})

		It("there are not enough fields", func() {
			spy := SpyRecordWriter{}

			shorterInput := "one,two,three"
			reader := strings.NewReader(shorterInput)
			err := streamLogFromCSVFile(reader, &spy)
			Expect(err).Should(HaveOccurred())
			Expect(len(spy.records)).To(Equal(0))

			extendedError := &ErrFieldCountExtended{}
			Expect(errors.As(err, &extendedError)).To(BeTrue())
			Expect(len(extendedError.Fields)).To(Equal(3))
		})

		It("there is a trailing comma", func() {
			spy := SpyRecordWriter{}

			trailingCommaInput := input + ","
			reader := strings.NewReader(trailingCommaInput)
			err := streamLogFromCSVFile(reader, &spy)
			Expect(err).Should(HaveOccurred())
			Expect(len(spy.records)).To(Equal(0))

			extendedError := &ErrFieldCountExtended{}
			Expect(errors.As(err, &extendedError)).To(BeTrue())
			Expect(len(extendedError.Fields)).To(Equal(FieldsPerRecord13 + 1))
		})

		It("there is a wrong number of fields on a line that is not the first", func() {
			spy := SpyRecordWriter{}

			longerInput := input + "\none,two,three"
			reader := strings.NewReader(longerInput)
			err := streamLogFromCSVFile(reader, &spy)
			Expect(err).Should(HaveOccurred())
			Expect(len(spy.records)).To(Equal(1))

			extendedError := &ErrFieldCountExtended{}
			Expect(errors.As(err, &extendedError)).To(BeTrue())
			Expect(len(extendedError.Fields)).To(Equal(3))
		})
	})

	It("correctly handles an empty stream", func() {
		spy := SpyRecordWriter{}

		Expect(streamLogFromCSVFile(strings.NewReader(""), &spy)).To(Succeed())
		Expect(len(spy.records)).To(Equal(0))
	})
})

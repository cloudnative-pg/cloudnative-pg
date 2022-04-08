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

package logpipe

import (
	"context"
	"errors"
	"io/ioutil"
	"os"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// SpyRecordWriter is an implementation of the RecordWriter interface
// keeping track of the generated records
type SpyRecordWriter struct {
	records []NamedRecord
}

// Write write the PostgreSQL log record to the instance manager logger
func (writer *SpyRecordWriter) Write(record NamedRecord) {
	writer.records = append(writer.records, record)
}

var _ = Describe("CSV file reader", func() {
	When("given CSV logs from logging_collector", func() {
		ctx := context.TODO()

		It("can read multiple CSV lines", func() {
			f, err := os.Open("testdata/two_lines.csv")
			defer func() {
				_ = f.Close()
			}()
			Expect(err).ToNot(HaveOccurred())

			spy := SpyRecordWriter{}
			p := LogPipe{
				record:          &LoggingRecord{},
				fieldsValidator: LogFieldValidator,
			}
			Expect(p.streamLogFromCSVFile(ctx, f, &spy)).To(Succeed())
			Expect(len(spy.records)).To(Equal(2))
		})

		It("can read multiple CSV lines on PostgreSQL version <= 12", func() {
			f, err := os.Open("testdata/two_lines_12.csv")
			defer func() {
				_ = f.Close()
			}()
			Expect(err).ToNot(HaveOccurred())

			spy := SpyRecordWriter{}
			p := LogPipe{
				record:          &LoggingRecord{},
				fieldsValidator: LogFieldValidator,
			}
			Expect(p.streamLogFromCSVFile(ctx, f, &spy)).To(Succeed())
			Expect(len(spy.records)).To(Equal(2))
		})

		It("can read multiple CSV lines on PostgreSQL version == 14", func() {
			f, err := os.Open("testdata/two_lines_14.csv")
			defer func() {
				_ = f.Close()
			}()
			Expect(err).ToNot(HaveOccurred())

			spy := SpyRecordWriter{}
			p := LogPipe{
				record:          &LoggingRecord{},
				fieldsValidator: LogFieldValidator,
			}
			Expect(p.streamLogFromCSVFile(ctx, f, &spy)).To(Succeed())
			Expect(len(spy.records)).To(Equal(2))
		})

		It("can read pgAudit CSV lines", func() {
			f, err := os.Open("testdata/pgaudit.csv")
			defer func() {
				_ = f.Close()
			}()
			Expect(err).ToNot(HaveOccurred())

			spy := SpyRecordWriter{}
			p := LogPipe{
				record:          NewPgAuditLoggingDecorator(),
				fieldsValidator: LogFieldValidator,
			}
			Expect(p.streamLogFromCSVFile(ctx, f, &spy)).To(Succeed())
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
				p := LogPipe{
					record:          &LoggingRecord{},
					fieldsValidator: LogFieldValidator,
				}
				err := p.streamLogFromCSVFile(ctx, reader, &spy)
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
				p := LogPipe{
					record:          &LoggingRecord{},
					fieldsValidator: LogFieldValidator,
				}
				err := p.streamLogFromCSVFile(ctx, reader, &spy)
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
				p := LogPipe{
					record:          &LoggingRecord{},
					fieldsValidator: LogFieldValidator,
				}
				err := p.streamLogFromCSVFile(ctx, reader, &spy)
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
				p := LogPipe{
					record:          &LoggingRecord{},
					fieldsValidator: LogFieldValidator,
				}
				err := p.streamLogFromCSVFile(ctx, reader, &spy)
				Expect(err).Should(HaveOccurred())
				Expect(len(spy.records)).To(Equal(1))

				extendedError := &ErrFieldCountExtended{}
				Expect(errors.As(err, &extendedError)).To(BeTrue())
				Expect(len(extendedError.Fields)).To(Equal(3))
			})
		})

		It("correctly handles an empty stream", func() {
			spy := SpyRecordWriter{}
			p := LogPipe{
				record:          &LoggingRecord{},
				fieldsValidator: LogFieldValidator,
			}
			Expect(p.streamLogFromCSVFile(ctx, strings.NewReader(""), &spy)).To(Succeed())
			Expect(len(spy.records)).To(Equal(0))
		})
	})
})

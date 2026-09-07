/*
Copyright © contributors to CloudNativePG, established as
CloudNativePG a Series of LF Projects, LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/

package report

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/fileutils"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
)

// Format to generate a sortable timestamp `YYYYMMDD_hhmmss`
const sortableTimestampFormat = "20060102_150405"

// reportName returns a filesystem-safe, timestamped report name.
// Format: report_<kind>[_<object>]_YYYYMMDD_hhmmss
func reportName(kind string, timestamp time.Time, objName ...string) string {
	var builder strings.Builder
	builder.WriteString("report_")
	builder.WriteString(kind)

	if len(objName) != 0 {
		builder.WriteString("_")
		builder.WriteString(objName[0])
	}

	builder.WriteString("_")
	builder.WriteString(timestamp.Format(sortableTimestampFormat))

	return builder.String()
}

// zipFileWriter abstracts any function that will write a new file into a ZIP
// within the `dirname` folder in the ZIP
type zipFileWriter func(zipper *zip.Writer, dirname string) error

// writerZippedReport writes a zip with the various report parts to file s
//   - file: the name of the zip file
//   - folder: the top-level folder created in the zip to contain all sections
func writeZippedReport(sections []zipFileWriter, file, folder string) (err error) {
	if exists, _ := fileutils.FileExists(file); exists {
		return fmt.Errorf("file already exist will not overwrite")
	}

	outputFile, err := os.Create(filepath.Clean(file))
	if err != nil {
		return fmt.Errorf("could not create zip file: %w", err)
	}

	defer func() {
		errF := outputFile.Sync()
		if errF != nil && err == nil {
			err = fmt.Errorf("could not flush the zip file: %w", errF)
		}

		errF = outputFile.Close()
		if errF != nil && err == nil {
			err = fmt.Errorf("could not close the zip file: %w", errF)
		}
	}()

	zipper := zip.NewWriter(outputFile)
	defer func() {
		if errZ := zipper.Close(); errZ != nil {
			if err == nil {
				err = fmt.Errorf("could not close the zip: %w", errZ)
			}
		}
	}()

	_, err = zipper.Create(folder + "/")
	if err != nil {
		return err
	}

	for _, section := range sections {
		err = section(zipper, folder)
		if err != nil {
			return err
		}
	}

	return err
}

type namedObject struct {
	Name   string
	Object any
}

func addContentToZip(c any, name, folder string, format plugin.OutputFormat, zipper *zip.Writer) error {
	var writer io.Writer
	fileName := filepath.Join(folder, name) + "." + string(format)
	writer, err := zipper.Create(fileName)
	if err != nil {
		return fmt.Errorf("could not add '%s' to zip: %w", fileName, err)
	}

	if err = plugin.Print(c, format, writer); err != nil {
		return fmt.Errorf("could not print '%s': %w", fileName, err)
	}
	return nil
}

func addObjectsToZip(objects []namedObject, folder string, format plugin.OutputFormat, zipper *zip.Writer) error {
	for _, obj := range objects {
		var objF io.Writer
		fileName := filepath.Join(folder, obj.Name) + "." + string(format)
		objF, err := zipper.Create(fileName)
		if err != nil {
			return fmt.Errorf("could not add object '%s' to zip: %w", obj, err)
		}

		if err = plugin.Print(obj.Object, format, objF); err != nil {
			return fmt.Errorf("could not print '%s': %w", fileName, err)
		}
	}
	return nil
}

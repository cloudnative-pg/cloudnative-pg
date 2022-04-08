/*
Copyright 2019-2022 The CloudNativePG Contributors

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

package report

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/fileutils"
)

// reportZipper is the common interface to print the results of the report commands
type reportZipper interface {
	writeToZip(zipper *zip.Writer, format plugin.OutputFormat) error
}

// writerZippedReport writes a zip with the various report parts to file
func writeZippedReport(rep reportZipper, format plugin.OutputFormat, file string) (err error) {
	var outputFile *os.File

	if exists, _ := fileutils.FileExists(file); exists {
		return fmt.Errorf("file already exist will not overwrite")
	}

	outputFile, err = os.Create(filepath.Clean(file))
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
		var errZ error
		if errZ = zipper.Flush(); errZ != nil {
			if err == nil {
				err = fmt.Errorf("could not flush the zip: %w", errZ)
			}
		}

		if errZ = zipper.Close(); errZ != nil {
			if err == nil {
				err = fmt.Errorf("could not close the zip: %w", errZ)
			}
		}
	}()

	err = rep.writeToZip(zipper, format)

	return err
}

type namedObject struct {
	Name   string
	Object interface{}
}

func addContentToZip(c interface{}, name string, zipper *zip.Writer, format plugin.OutputFormat) error {
	var writer io.Writer
	writer, err := zipper.Create(name + "." + string(format))
	if err != nil {
		return fmt.Errorf("could not add '%s' to zip: %w", name, err)
	}

	if err = plugin.Print(c, format, writer); err != nil {
		return fmt.Errorf("could not print '%s': %w", name, err)
	}
	return nil
}

func addObjectsToZip(objects []namedObject, zipper *zip.Writer, format plugin.OutputFormat) error {
	for _, obj := range objects {
		var objF io.Writer
		objF, err := zipper.Create(obj.Name + "." + string(format))
		if err != nil {
			return fmt.Errorf("could not add object '%s' to zip: %w", obj, err)
		}

		if err = plugin.Print(obj.Object, format, objF); err != nil {
			return fmt.Errorf("could not print: %w", err)
		}
	}
	return nil
}

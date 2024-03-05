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

// Package architectures implement the show-architectures command
package architectures

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
)

// NewCmd creates the new cobra command
func NewCmd() *cobra.Command {
	cmd := cobra.Command{
		Use:   "show-architectures",
		Short: "Lists all the CPU architectures supported by this image",
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := run(); err != nil {
				log.Error(err, "Error while extracting the list of supported architectures")
			}

			return nil
		},
	}

	return &cmd
}

func run() error {
	if err := utils.DetectAvailableArchitectures(); err != nil {
		return err
	}
	architectures := make([]string, 0, len(utils.GetAvailableArchitectures()))
	for _, arch := range utils.GetAvailableArchitectures() {
		architectures = append(architectures, arch.GoArch)
	}
	val, err := json.MarshalIndent(architectures, "", "    ")
	if err != nil {
		return err
	}
	fmt.Println(string(val))
	return nil
}

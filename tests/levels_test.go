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

package tests

import (
	"testing"
)

func TestLevelConstants(t *testing.T) {
	tests := []struct {
		name     string
		level    Level
		expected int
	}{
		{"Highest", Highest, 0},
		{"High", High, 1},
		{"Medium", Medium, 2},
		{"Low", Low, 3},
		{"Lowest", Lowest, 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if int(tt.level) != tt.expected {
				t.Errorf("expected %s to be %d, got %d", tt.name, tt.expected, tt.level)
			}
		})
	}
}

func TestDefaultTestDepth(t *testing.T) {
	if defaultTestDepth != int(Medium) {
		t.Errorf("expected defaultTestDepth to be %d (Medium), got %d", Medium, defaultTestDepth)
	}
}

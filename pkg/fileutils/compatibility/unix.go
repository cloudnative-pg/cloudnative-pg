//go:build linux || darwin
// +build linux darwin

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

// Package compatibility provides a layer to cross-compile with other OS than Linux
package compatibility

import (
	"os"

	"golang.org/x/sys/unix"
)

// CreateFifo invokes the Unix system call Mkfifo, if the given filename exists
func CreateFifo(fileName string) error {
	if _, err := os.Stat(fileName); err != nil {
		return unix.Mkfifo(fileName, 0o600)
	}
	return nil
}

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

package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"

	. "github.com/onsi/ginkgo/v2"
)

// diagnosticsDir is where the e2e workflows collect failure artifacts from.
// Specs run with the e2e package as working directory.
const diagnosticsDir = "out"

// keepWorkerCrashOutput saves the crash output of this Ginkgo worker in
// out/crash-proc<N>-<pid>.log.
//
// When a worker panics or hits a fatal runtime error, the Go runtime writes
// to the file descriptor Ginkgo redirected for the running spec. That text is
// never read back, the CLI only reports "a parallel process disappeared" and
// the cause is lost. The returned function must run on a clean exit: it removes
// the file when nothing was written.
func keepWorkerCrashOutput(proc int) func() {
	if err := os.MkdirAll(diagnosticsDir, 0o750); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "cannot keep the crash output: %v\n", err)
		return func() {}
	}

	path := filepath.Join(diagnosticsDir, fmt.Sprintf("crash-proc%d-%d.log", proc, os.Getpid()))
	f, err := os.OpenFile(filepath.Clean(path), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "cannot keep the crash output: %v\n", err)
		return func() {}
	}

	// Without this a panic only dumps the stack of the goroutine that failed
	debug.SetTraceback("all")
	if err := debug.SetCrashOutput(f, debug.CrashOptions{}); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "cannot keep the crash output: %v\n", err)
		_ = f.Close()
		_ = os.Remove(path)
		return func() {}
	}

	return func() {
		_ = debug.SetCrashOutput(nil, debug.CrashOptions{})
		if st, err := f.Stat(); err == nil && st.Size() == 0 {
			_ = os.Remove(path)
		}
		_ = f.Close()
	}
}

// currentSpecFile is where the running spec is recorded, empty when disabled
var currentSpecFile string

// trackCurrentSpec records the running spec in out/current-proc<N>-<pid>.txt.
//
// A worker killed from outside, such as by the OOM killer, leaves no crash
// output. The file then tells which spec it was running. The returned function
// must run on a clean exit: it removes the file.
func trackCurrentSpec(proc int) func() {
	if err := os.MkdirAll(diagnosticsDir, 0o750); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "cannot track the current spec: %v\n", err)
		return func() {}
	}

	currentSpecFile = filepath.Join(diagnosticsDir, fmt.Sprintf("current-proc%d-%d.txt", proc, os.Getpid()))
	return func() { _ = os.Remove(currentSpecFile) }
}

var _ = BeforeEach(func() {
	if currentSpecFile != "" {
		_ = os.WriteFile(currentSpecFile, []byte(CurrentSpecReport().FullText()+"\n"), 0o600)
	}
})

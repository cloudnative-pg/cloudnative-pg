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

// Package executablehash detect the SHA256 of the running binary
package executablehash

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"sync"
)

var (
	processBinaryHash string
	mx                sync.Mutex
)

// Stream opens a stream reading from the executable of the current binary
func Stream() (io.ReadCloser, error) {
	processBinaryFileName := os.Args[0]
	return os.Open(processBinaryFileName) // #nosec
}

// Get gets the hashcode of the executable of this binary
func Get() (string, error) {
	var err error

	mx.Lock()
	defer mx.Unlock()

	if processBinaryHash != "" {
		return processBinaryHash, nil
	}

	// Look for the binary of the operator
	binaryFileStream, err := Stream()
	if err != nil {
		return "", err
	}
	defer func() {
		err = binaryFileStream.Close()
	}()

	encoder := sha256.New()
	_, err = io.Copy(encoder, binaryFileStream)
	if err != nil {
		return "", err
	}

	processBinaryHash = fmt.Sprintf("%x", encoder.Sum(nil))
	return processBinaryHash, err
}

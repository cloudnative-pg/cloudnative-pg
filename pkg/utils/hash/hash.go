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

package hash

import (
	"fmt"
	"hash"
	"hash/fnv"

	"github.com/davecgh/go-spew/spew"
	"k8s.io/apimachinery/pkg/util/rand"
)

// DeepHashObject writes specified object to hash using the spew library
// which follows pointers and prints actual values of the nested objects
// ensuring the hash does not change when a pointer changes.
func DeepHashObject(hasher hash.Hash, objectToWrite any) error {
	hasher.Reset()
	printer := spew.ConfigState{
		Indent:         " ",
		SortKeys:       true,
		DisableMethods: true,
		SpewKeys:       true,
	}

	_, err := printer.Fprintf(hasher, "%#v", objectToWrite)
	return err
}

// ComputeHash returns a hash value calculated from the provided object.
// The hash will be safe encoded to avoid bad words.
func ComputeHash(object any) (string, error) {
	hasher := fnv.New32a()
	if err := DeepHashObject(hasher, object); err != nil {
		return "", err
	}

	return rand.SafeEncodeString(fmt.Sprint(hasher.Sum32())), nil
}

// ComputeVersionedHash follows the same rules of ComputeHash with the exception that accepts also a epoc value.
// The epoc value is used to generate a new hash from a same object.
// This is useful to force a new hash even if the original object is not changed.
// A practical use is to force a reconciliation loop of the object.
func ComputeVersionedHash(object any, epoc int) (string, error) {
	type versionedHash struct {
		object any
		epoc   int
	}

	return ComputeHash(versionedHash{object: object, epoc: epoc})
}

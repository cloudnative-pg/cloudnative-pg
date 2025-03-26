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

package utils

// anyNumber is a constraint that permits any number type. This type
// definition is copied rather than depending on x/exp/constraints since the
// dependency is otherwise unneeded, the definition is relatively trivial and
// static, and the Go language maintainers are not sure if/where these will live
// in the standard library.
//
// Reference: https://github.com/golang/go/issues/61914
type anyNumber interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr |
		~float32 | ~float64
}

// IsPowerOfTwo calculates if a number is power of two or not
// reference: https://github.com/golang/go/blob/master/src/strconv/itoa.go#L204 #wokeignore:rule=master
// This function will return false if the number is zero
func IsPowerOfTwo(n int) bool {
	return (n != 0) && (n&(n-1) == 0)
}

// ToBytes converts an input value in MB to bytes
// Input: value - a number representing size in MB
// Output: the size in bytes, calculated by multiplying the input value by 1024 * 1024
func ToBytes[T anyNumber](mb T) float64 {
	multiplier := float64(1024)
	return float64(mb) * multiplier * multiplier
}

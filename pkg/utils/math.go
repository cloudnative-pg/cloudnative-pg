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

package utils

import (
	"golang.org/x/exp/constraints"
)

// IsPowerOfTwo calculates if a number is power of two or not
// reference: https://github.com/golang/go/blob/master/src/strconv/itoa.go#L204 #wokeignore:rule=master
// This function will return false if the number is zero
func IsPowerOfTwo(n int) bool {
	return (n != 0) && (n&(n-1) == 0)
}

// ToBytes converts an input value in MB to bytes
// Input: value - an integer representing size in MB
// Output: the size in bytes, calculated by multiplying the input value by 1024 * 1024
func ToBytes[T constraints.Signed | constraints.Float](mb T) float64 {
	multiplier := float64(1024)
	return float64(mb) * multiplier * multiplier
}

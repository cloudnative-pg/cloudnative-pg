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

package utils

// IsPowerOfTwo calculates if a number is power of two or not
// reference: https://github.com/golang/go/blob/master/src/strconv/itoa.go#L204 #wokeignore:rule=master
// This function will return false if the number is zero
func IsPowerOfTwo(n int) bool {
	return (n != 0) && (n&(n-1) == 0)
}

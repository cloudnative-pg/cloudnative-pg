/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package utils

// IsPowerOfTwo calculates if a number is power of two or not
// reference: https://github.com/golang/go/blob/master/src/strconv/itoa.go#L204 #wokeignore:rule=master
// This function will return false if the number is zero
func IsPowerOfTwo(n int) bool {
	return (n != 0) && (n&(n-1) == 0)
}

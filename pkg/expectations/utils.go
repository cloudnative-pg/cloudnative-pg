/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package expectations

// LowerExpectationsDelta lowers the expectations of the given controller.
//
// When `delta` is positive the function will work on `add` count, and then
// `delta`is negative the function will work on the `delete` count.
//
// This is useful when the new and the old count of a certain resource
// is known. In that case the expectations can be reconciled via
//
//     r.LowerExpectationsDelta(key, newCount - oldCount)
//
func (r *ControllerExpectations) LowerExpectationsDelta(key string, delta int) {
	switch {
	case delta > 0:
		r.LowerExpectations(key, delta, 0)
	case delta < 0:
		r.LowerExpectations(key, 0, -delta)
	}
}

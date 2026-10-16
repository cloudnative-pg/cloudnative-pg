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

package url

import (
	"encoding/json"
	"net/http"
)

// FailSafeResponse is the JSON body of the PathFailSafe endpoint. It reports the
// responding instance's view of the cluster's target primary, so a peer
// that can't reach the API server can learn, via this instance, whether it
// has already been superseded.
type FailSafeResponse struct {
	TargetPrimary string `json:"targetPrimary,omitempty"`
}

// WriteFailSafe encodes resp as the JSON response body for a PathFailSafe request.
func WriteFailSafe(w http.ResponseWriter, resp FailSafeResponse) error {
	js, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json")
	_, err = w.Write(js)
	return err
}

// ParseFailSafe decodes a PathFailSafe response body. An unparseable body (e.g. a
// pre-upgrade peer's plain "OK" response) yields a zero Response, not an
// error: it carries no signal.
func ParseFailSafe(body []byte) FailSafeResponse {
	var resp FailSafeResponse
	_ = json.Unmarshal(body, &resp)
	return resp
}

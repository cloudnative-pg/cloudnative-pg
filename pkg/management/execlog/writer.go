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

package execlog

import (
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
)

// LogWriter implements the `Writer` interface using the logger,
// It uses "Info" as logging level.
type LogWriter struct {
	Logger log.Logger
}

// Write logs the given slice of bytes using the provided Logger.
func (w *LogWriter) Write(p []byte) (int, error) {
	if len(p) > 0 {
		w.Logger.Info(string(p))
	}

	return len(p), nil
}

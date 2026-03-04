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

/*
Package configparser contains the code required to fill a Go structure
representing the configuration information from several sources like:

 1. a struct containing the default values
 2. a map from string to strings (e.g. the one that can be extracted for a Kubernetes ConfigMap)
 3. an environment source like the system environment variables or a "fake"
    one suitable for testing.

To map the struct fields with the sources, the "env" tag is being
used like in the following example:

	type Data struct {
		// WebhookCertDir is the directory where the certificates for the webhooks
		// need to written. This is different between plain Kubernetes and OpenShift
		WebhookCertDir string `json:"webhookCertDir" env:"WEBHOOK_CERT_DIR"`

		// WatchNamespace is the namespace where the operator should watch and
		// is configurable via environment variables of via the OpenShift console
		WatchNamespace string `json:"watchNamespace" env:"WATCH_NAMESPACE"`
	}

The ReadConfigMap function will look for an "WEBHOOK_CERT_DIR" entry inside
the passed map or a corresponding environment variable. This function can
be used to implement a method in the previous struct like in the following
example:

	func (config *Data) ReadConfigMap(data map[string]string) {
		configparser.ReadConfigMap(config, &Data{}, data, configparser.OsEnvironment{})
	}
*/
package configparser

import (
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"

	"github.com/cloudnative-pg/machinery/pkg/log"
)

var configparserLog = log.WithName("configparser")

// ReadConfigMap reads the configuration from the environment and the passed in data map.
// Config and defaults are supposed to be pointers to structs of the same type
func ReadConfigMap(target any, defaults any, data map[string]string) { //nolint: gocognit
	ensurePointerToCompatibleStruct("target", target, "default", defaults)

	count := reflect.TypeOf(defaults).Elem().NumField()
	for i := 0; i < count; i++ {
		field := reflect.TypeOf(defaults).Elem().Field(i)
		envName := field.Tag.Get("env")

		// Fields without env tag are skipped.
		if envName == "" {
			continue
		}

		// Initialize value with default
		var value string
		var sliceValue []string

		valueField := reflect.ValueOf(defaults).Elem().FieldByName(field.Name)
		switch valueField.Kind() {
		case reflect.Bool:
			value = strconv.FormatBool(valueField.Bool())

		case reflect.Int:
			value = fmt.Sprintf("%v", valueField.Int())

		case reflect.Ptr:
			// Handle pointer types - if not nil, get the underlying value
			if !valueField.IsNil() {
				switch valueField.Elem().Kind() {
				case reflect.Int:
					value = fmt.Sprintf("%v", valueField.Elem().Int())
				default:
					configparserLog.Info(
						"Skipping unsupported pointer type while parsing default configuration",
						"field", field.Name, "kind", valueField.Elem().Kind())
					continue
				}
			}
			// If nil, value stays empty, which means we'll only set it if env/data provides a value

		case reflect.Slice:
			if valueField.Type().Elem().Kind() != reflect.String {
				configparserLog.Info(
					"Skipping invalid slice type while parsing default configuration",
					"field", field.Name, "value", value)
			} else {
				sliceValue = valueField.Interface().([]string)
			}

		default:
			value = valueField.String()
		}
		// If the key is present in the environment, use its value
		if envValue := os.Getenv(envName); envValue != "" {
			value = envValue
		}
		// If the key is present in the passed data, use its value
		if mapValue, ok := data[envName]; ok {
			value = mapValue
		}

		switch t := field.Type; t.Kind() {
		case reflect.Bool:
			boolValue, err := strconv.ParseBool(value)
			if err != nil {
				configparserLog.Info(
					"Skipping invalid boolean value parsing configuration",
					"field", field.Name, "value", value)
				continue
			}
			reflect.ValueOf(target).Elem().FieldByName(field.Name).SetBool(boolValue)
		case reflect.Int:
			intValue, err := strconv.ParseInt(value, 10, 0)
			if err != nil {
				configparserLog.Info(
					"Skipping invalid integer value parsing configuration",
					"field", field.Name, "value", value)
				continue
			}
			reflect.ValueOf(target).Elem().FieldByName(field.Name).SetInt(intValue)
		case reflect.Ptr:
			// Handle pointer types
			if value == "" {
				// If no value is provided, leave the pointer as nil
				continue
			}
			switch t.Elem().Kind() {
			case reflect.Int:
				intValue, err := strconv.ParseInt(value, 10, 0)
				if err != nil {
					configparserLog.Info(
						"Skipping invalid integer pointer value parsing configuration",
						"field", field.Name, "value", value)
					continue
				}
				intVal := int(intValue)
				reflect.ValueOf(target).Elem().FieldByName(field.Name).Set(reflect.ValueOf(&intVal))
			default:
				configparserLog.Info(
					"Skipping unsupported pointer type while parsing configuration",
					"field", field.Name, "kind", t.Elem().Kind())
				continue
			}
		case reflect.String:
			reflect.ValueOf(target).Elem().FieldByName(field.Name).SetString(value)
		case reflect.Slice:
			var targetValue []string
			if value != "" {
				targetValue = splitAndTrim(value)
			} else {
				targetValue = sliceValue
			}

			reflect.ValueOf(target).Elem().FieldByName(field.Name).Set(reflect.ValueOf(targetValue))
		default:
			errMsg := fmt.Sprintf(
				"field: %s, type: %s, kind: %s is not being handled",
				field.Name, t.String(), t.Kind())
			panic(errMsg)
		}
	}
}

func ensurePointerToStruct(name string, data any) {
	errMsg := fmt.Sprintf(
		"%v: expecting pointer to struct (found %v)",
		name,
		reflect.TypeOf(data).String())

	if reflect.TypeOf(data).Kind() != reflect.Ptr {
		panic(errMsg)
	}

	if reflect.TypeOf(data).Elem().Kind() != reflect.Struct {
		panic(errMsg)
	}
}

func ensurePointerToCompatibleStruct(
	firstName string, firstValue any, secondName string, secondValue any,
) {
	errMsg := fmt.Sprintf(
		"%v and %v are different structs",
		reflect.TypeOf(firstValue).String(),
		reflect.TypeOf(secondValue).String())
	ensurePointerToStruct(firstName, firstValue)
	ensurePointerToStruct(secondName, secondValue)

	if !reflect.TypeOf(firstValue).Elem().AssignableTo(reflect.TypeOf(secondValue).Elem()) {
		panic(errMsg)
	}
}

// splitAndTrim slices a string into all substrings after each comma and
// returns a slice of those space-trimmed substrings
func splitAndTrim(commaSeparatedList string) []string {
	list := strings.Split(commaSeparatedList, ",")
	for i := range list {
		list[i] = strings.TrimSpace(list[i])
	}
	return list
}

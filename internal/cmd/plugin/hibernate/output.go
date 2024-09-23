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

package hibernate

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/cheynewallace/tabby"
	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/logrusorgru/aurora/v4"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

const (
	// statusClusterManifestNotFound is an error message reported when no cluster manifest is not found
	statusClusterManifestNotFound = "Cluster manifest not found"
)

// statusOutputManager is a type capable of executing a status output request
type statusOutputManager interface {
	addHibernationSummaryInformation(level statusLevel, statusMessage, clusterName string)
	addClusterManifestInformation(cluster *apiv1.Cluster)
	addPVCGroupInformation(pvcs []corev1.PersistentVolumeClaim)
	// execute renders the output
	execute() error
}

type textStatusOutputManager struct {
	textPrinter  *tabby.Tabby
	stdoutBuffer *bytes.Buffer
}

func newTextStatusOutputManager() *textStatusOutputManager {
	buffer := &bytes.Buffer{}
	return &textStatusOutputManager{
		textPrinter:  tabby.NewCustom(tabwriter.NewWriter(buffer, 0, 0, 4, ' ', 0)),
		stdoutBuffer: buffer,
	}
}

func (t *textStatusOutputManager) getColor(level statusLevel) aurora.Color {
	switch level {
	case warningLevel:
		return aurora.YellowFg
	case errorLevel:
		return aurora.RedFg
	default:
		return aurora.GreenFg
	}
}

func (t *textStatusOutputManager) addHibernationSummaryInformation(
	level statusLevel,
	statusMessage string,
	clusterName string,
) {
	t.textPrinter.AddHeader(aurora.Colorize("Hibernation Summary", t.getColor(level)))
	t.textPrinter.AddLine("Hibernation status", statusMessage)
	t.textPrinter.AddLine("Cluster Name", clusterName)
	t.textPrinter.AddLine("Cluster Namespace", plugin.Namespace)
	t.textPrinter.AddLine()
}

func (t *textStatusOutputManager) addClusterManifestInformation(
	cluster *apiv1.Cluster,
) {
	if cluster == nil {
		t.textPrinter.AddHeader(aurora.Red("Cluster Spec Information"))
		t.textPrinter.AddLine(aurora.Red(statusClusterManifestNotFound))
		return
	}

	t.textPrinter.AddHeader(aurora.Green("Cluster Spec Information"))
	bytesArray, err := yaml.Marshal(cluster.Spec)
	if err != nil {
		const message = "Could not serialize the cluster manifest"
		t.textPrinter.AddLine(aurora.Red(message))
		return
	}

	t.textPrinter.AddLine(string(bytesArray))
	t.textPrinter.AddLine()
}

func (t *textStatusOutputManager) addPVCGroupInformation(
	pvcs []corev1.PersistentVolumeClaim,
) {
	if len(pvcs) == 0 {
		return
	}

	// there is no need to iterate the pvc group, it is either all valid or none
	value, ok := pvcs[0].Annotations[utils.HibernatePgControlDataAnnotationName]
	if !ok {
		return
	}

	t.textPrinter.AddHeader(aurora.Green("PostgreSQL instance control information"))
	t.textPrinter.AddLine(value)
}

func (t *textStatusOutputManager) execute() error {
	// do not remove this is to flush the writer cache into the buffer
	t.textPrinter.Print()
	fmt.Print(t.stdoutBuffer)
	return nil
}

type structuredStatusOutputManager struct {
	mapToSerialize map[string]interface{}
	format         plugin.OutputFormat
	ctx            context.Context
}

func newStructuredOutputManager(ctx context.Context, format plugin.OutputFormat) *structuredStatusOutputManager {
	return &structuredStatusOutputManager{
		mapToSerialize: map[string]interface{}{},
		format:         format,
		ctx:            ctx,
	}
}

func (t *structuredStatusOutputManager) addHibernationSummaryInformation(
	level statusLevel,
	statusMessage string,
	clusterName string,
) {
	t.mapToSerialize["summary"] = map[string]string{
		"status":      statusMessage,
		"clusterName": clusterName,
		"namespace":   plugin.Namespace,
		"level":       string(level),
	}
}

func (t *structuredStatusOutputManager) addClusterManifestInformation(
	cluster *apiv1.Cluster,
) {
	tmpMap := map[string]interface{}{}
	defer func() {
		t.mapToSerialize["cluster"] = tmpMap
	}()

	if cluster == nil {
		tmpMap["error"] = statusClusterManifestNotFound
		return
	}

	tmpMap["spec"] = cluster.Spec
}

func (t *structuredStatusOutputManager) addPVCGroupInformation(
	pvcs []corev1.PersistentVolumeClaim,
) {
	contextLogger := log.FromContext(t.ctx)

	// there is no need to iterate the pvc group, it is either all valid or none
	value, ok := pvcs[0].Annotations[utils.HibernatePgControlDataAnnotationName]
	if !ok {
		return
	}

	tmp := map[string]string{}
	rows := strings.Split(value, "\n")
	for _, row := range rows {
		// skip empty rows
		if strings.Trim(row, " ") == "" {
			continue
		}

		res := strings.SplitN(row, ":", 2)
		if len(res) != 2 {
			// bad row parsing, we skip it
			contextLogger.Warning("skipping row because it was malformed", "row", row)
			tmp["error"] = "one or more rows could not be parsed"
			continue
		}
		tmp[res[0]] = strings.Trim(res[1], " ")
	}

	t.mapToSerialize["pgControlData"] = tmp
}

func (t *structuredStatusOutputManager) execute() error {
	return plugin.Print(t.mapToSerialize, t.format, os.Stdout)
}

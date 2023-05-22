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

// Package report implements the kubectl-cnpg report command
package report

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	v12 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
)

// operatorReport contains the data to be printed by the `report operator` plugin
type operatorReport struct {
	deployment              appsv1.Deployment
	operatorPods            []corev1.Pod
	secrets                 []namedObject
	configs                 []namedObject
	events                  corev1.EventList
	webhookService          corev1.Service
	mutatingWebhookConfig   *v12.MutatingWebhookConfigurationList
	validatingWebhookConfig *v12.ValidatingWebhookConfigurationList
}

// writeToZip makes a new section in the ZIP file, and adds in it various
// Kubernetes object manifests
func (or operatorReport) writeToZip(zipper *zip.Writer, format plugin.OutputFormat, folder string) error {
	singleObjects := []struct {
		content interface{}
		name    string
	}{
		{content: or.deployment, name: "deployment"},
		{content: or.operatorPods, name: "operator-pods"},
		{content: or.events, name: "events"},
		{content: or.validatingWebhookConfig, name: "validating-webhook-configuration"},
		{content: or.mutatingWebhookConfig, name: "mutating-webhook-configuration"},
		{content: or.webhookService, name: "webhook-service"},
	}

	newFolder := filepath.Join(folder, "manifests")
	_, err := zipper.Create(newFolder + "/")
	if err != nil {
		return err
	}

	for _, object := range singleObjects {
		err := addContentToZip(object.content, object.name, newFolder, format, zipper)
		if err != nil {
			return err
		}
	}

	multiObjects := [][]namedObject{or.configs, or.secrets}
	for _, obj := range multiObjects {
		err := addObjectsToZip(obj, newFolder, format, zipper)
		if err != nil {
			return err
		}
	}
	return nil
}

// operator implements the "report operator" subcommand
// Produces a zip file containing
//   - operator deployment
//   - operator pod definition
//   - operator configuration Configmap and Secret key (if any)
//   - events in the operator namespace
//   - operator's Validating/MutatingWebhookConfiguration and their associated services
//   - operator pod's logs (if `includeLogs` is true)
func operator(ctx context.Context, format plugin.OutputFormat,
	file string, stopRedaction, includeLogs, logTimeStamp bool, now time.Time,
) error {
	secretRedactor := redactSecret
	configMapRedactor := redactConfigMap
	if stopRedaction {
		secretRedactor = passSecret
		configMapRedactor = passConfigMap
		fmt.Println("WARNING: secret Redaction is OFF. Use it with caution")
	}

	operatorDeployment, err := getOperatorDeployment(ctx)
	if errors.Is(err, errNoOperatorDeployment) {
		// Try to be helpful to the user
		return fmt.Errorf("%w\n"+
			"HINT: Operator might be installed in another namespace."+
			"Specify a namespace using the '-n' option", err)
	} else if err != nil {
		return fmt.Errorf("could not get operator deployment: %w", err)
	}

	operatorPods, err := getOperatorPods(ctx)
	if err != nil {
		return fmt.Errorf("could not get operator pod: %w", err)
	}

	operatorSecrets, err := getOperatorSecrets(ctx, operatorDeployment)
	if err != nil {
		return fmt.Errorf("could not get operator secrets: %w", err)
	}
	secrets := make([]namedObject, 0, len(operatorSecrets))
	for _, ss := range operatorSecrets {
		secrets = append(secrets, namedObject{Name: ss.Name + "(secret)", Object: secretRedactor(ss)})
	}

	operatorConfigMaps, err := getOperatorConfigMaps(ctx, operatorDeployment)
	if err != nil {
		return fmt.Errorf("could not get operator configmap: %w", err)
	}
	configs := make([]namedObject, 0, len(operatorConfigMaps))
	for _, cm := range operatorConfigMaps {
		configs = append(configs, namedObject{Name: cm.Name, Object: configMapRedactor(cm)})
	}

	var events corev1.EventList
	err = plugin.Client.List(ctx, &events, client.InNamespace(operatorPods[0].Namespace))
	if err != nil {
		return fmt.Errorf("could not get events: %w", err)
	}

	mutatingWebhook, validatingWebhook, err := getWebhooks(ctx, stopRedaction)
	if err != nil {
		return fmt.Errorf("could not get webhooks: %w", err)
	}

	webhookService, err := getWebhookService(ctx, mutatingWebhook.Items[0].Webhooks[0].ClientConfig)
	if err != nil {
		return fmt.Errorf("could not get webhook service: %w", err)
	}

	rep := operatorReport{
		deployment:              operatorDeployment,
		operatorPods:            operatorPods,
		secrets:                 secrets,
		configs:                 configs,
		events:                  events,
		mutatingWebhookConfig:   mutatingWebhook,
		validatingWebhookConfig: validatingWebhook,
		webhookService:          webhookService,
	}

	reportZipper := func(zipper *zip.Writer, dirname string) error {
		return rep.writeToZip(zipper, format, dirname)
	}

	sections := []zipFileWriter{reportZipper}

	if includeLogs {
		logZipper := func(zipper *zip.Writer, dirname string) error {
			return streamOperatorLogsToZip(ctx, operatorPods, dirname, "operator-logs", logTimeStamp, zipper)
		}
		sections = append(sections, logZipper)
	}

	err = writeZippedReport(sections, file, reportName("operator", now))
	if err != nil {
		return fmt.Errorf("could not write report: %w", err)
	}

	fmt.Printf("Successfully written report to \"%s\" (format: \"%s\")\n", file, format)

	return nil
}

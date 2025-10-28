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

// Package report implements the kubectl-cnpg report command
package report

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// operatorReport contains the data to be printed by the `report operator` plugin
type operatorReport struct {
	deployment              appsv1.Deployment
	operatorPods            []corev1.Pod
	secrets                 []namedObject
	configs                 []namedObject
	events                  corev1.EventList
	webhookService          corev1.Service
	mutatingWebhookConfig   *admissionregistrationv1.MutatingWebhookConfigurationList
	validatingWebhookConfig *admissionregistrationv1.ValidatingWebhookConfigurationList
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
	// Configure redactors
	secretRedactor, configMapRedactor := configureRedactors(stopRedaction)

	// Collect required namespace-scoped resources (these must succeed)
	deployment, pods, err := collectRequiredResources(ctx)
	if err != nil {
		return err
	}

	// Collect configurations with redaction
	secrets, configs, err := collectConfigurations(ctx, deployment, secretRedactor, configMapRedactor)
	if err != nil {
		return err
	}

	// Collect events
	events, err := collectEvents(ctx, pods[0].Namespace)
	if err != nil {
		return err
	}

	// Collect optional cluster-scoped resources (these can fail gracefully)
	mutatingWebhook, validatingWebhook := tryCollectWebhooks(ctx, stopRedaction)
	webhookService := tryCollectWebhookService(ctx, mutatingWebhook)

	// Build the report structure
	rep := operatorReport{
		deployment:              deployment,
		operatorPods:            pods,
		secrets:                 secrets,
		configs:                 configs,
		events:                  events,
		mutatingWebhookConfig:   mutatingWebhook,
		validatingWebhookConfig: validatingWebhook,
		webhookService:          webhookService,
	}

	// Assemble all sections for the ZIP file
	sections := assembleSections(ctx, rep, pods, format, includeLogs, logTimeStamp)

	// Write the final report
	if err := writeZippedReport(sections, file, reportName("operator", now)); err != nil {
		return fmt.Errorf("could not write report: %w", err)
	}

	fmt.Printf("Successfully written report to \"%s\" (format: \"%s\")\n", file, format)
	return nil
}

// configureRedactors sets up the appropriate redaction functions based on user preference
func configureRedactors(stopRedaction bool) (
	func(corev1.Secret) corev1.Secret, func(corev1.ConfigMap) corev1.ConfigMap,
) {
	if stopRedaction {
		fmt.Println("WARNING: secret Redaction is OFF. Use it with caution")
		return passSecret, passConfigMap
	}
	return redactSecret, redactConfigMap
}

// collectRequiredResources gathers deployment and pods which are required for the report
func collectRequiredResources(ctx context.Context) (appsv1.Deployment, []corev1.Pod, error) {
	deployment, err := getOperatorDeployment(ctx)
	if errors.Is(err, errNoOperatorDeployment) {
		return appsv1.Deployment{}, nil, fmt.Errorf("%w\n"+
			"HINT: Operator might be installed in another namespace."+
			"Specify a namespace using the '-n' option", err)
	}
	if err != nil {
		return appsv1.Deployment{}, nil, fmt.Errorf("could not get operator deployment: %w", err)
	}

	pods, err := getOperatorPods(ctx)
	if err != nil {
		return appsv1.Deployment{}, nil, fmt.Errorf("could not get operator pod: %w", err)
	}

	return deployment, pods, nil
}

// collectConfigurations gathers secrets and configmaps with appropriate redaction
func collectConfigurations(
	ctx context.Context,
	deployment appsv1.Deployment,
	secretRedactor func(corev1.Secret) corev1.Secret,
	configMapRedactor func(corev1.ConfigMap) corev1.ConfigMap,
) ([]namedObject, []namedObject, error) {
	operatorSecrets, err := getOperatorSecrets(ctx, deployment)
	if err != nil {
		return nil, nil, fmt.Errorf("could not get operator secrets: %w", err)
	}

	secrets := make([]namedObject, 0, len(operatorSecrets))
	for _, ss := range operatorSecrets {
		secrets = append(secrets, namedObject{
			Name:   ss.Name + "(secret)",
			Object: secretRedactor(ss),
		})
	}

	operatorConfigMaps, err := getOperatorConfigMaps(ctx, deployment)
	if err != nil {
		return nil, nil, fmt.Errorf("could not get operator configmap: %w", err)
	}

	configs := make([]namedObject, 0, len(operatorConfigMaps))
	for _, cm := range operatorConfigMaps {
		configs = append(configs, namedObject{
			Name:   cm.Name,
			Object: configMapRedactor(cm),
		})
	}

	return secrets, configs, nil
}

// collectEvents gathers events from the specified namespace
func collectEvents(ctx context.Context, namespace string) (corev1.EventList, error) {
	var events corev1.EventList
	err := plugin.Client.List(ctx, &events, client.InNamespace(namespace))
	if err != nil {
		return corev1.EventList{}, fmt.Errorf("could not get events: %w", err)
	}
	return events, nil
}

// tryCollectWebhooks attempts to collect webhook configurations, logging warnings on failure
func tryCollectWebhooks(
	ctx context.Context,
	stopRedaction bool,
) (*admissionregistrationv1.MutatingWebhookConfigurationList,
	*admissionregistrationv1.ValidatingWebhookConfigurationList,
) {
	mutatingWebhook, validatingWebhook, err := getWebhooks(ctx, stopRedaction)
	if err != nil {
		logWarning("could not get webhooks", err,
			"Continuing without webhook information. This is expected if you don't have cluster-level permissions.")
	}
	return mutatingWebhook, validatingWebhook
}

// tryCollectWebhookService attempts to collect the webhook service, logging warnings on failure
func tryCollectWebhookService(
	ctx context.Context,
	mutatingWebhook *admissionregistrationv1.MutatingWebhookConfigurationList,
) corev1.Service {
	webhookService, err := getWebhookService(ctx, mutatingWebhook)
	if err != nil {
		logWarning("could not get webhook service", err,
			"Continuing without webhook service information.")
	}
	return webhookService
}

// assembleSections creates all ZIP file sections including manifests, logs, and OLM data
func assembleSections(
	ctx context.Context,
	rep operatorReport,
	pods []corev1.Pod,
	format plugin.OutputFormat,
	includeLogs bool,
	logTimeStamp bool,
) []zipFileWriter {
	// Main report section
	sections := []zipFileWriter{
		func(zipper *zip.Writer, dirname string) error {
			return rep.writeToZip(zipper, format, dirname)
		},
	}

	// Optional logs section
	if includeLogs {
		sections = append(sections, func(zipper *zip.Writer, dirname string) error {
			return streamOperatorLogsToZip(ctx, pods, dirname, "operator-logs", logTimeStamp, zipper)
		})
	}

	// Optional OLM section
	if olmZipper := tryGetOLMReport(ctx, format); olmZipper != nil {
		sections = append(sections, olmZipper)
	}

	return sections
}

// logWarning prints a formatted warning message with additional context
func logWarning(message string, err error, additionalInfo string) {
	fmt.Printf("WARNING: %s: %v\n", message, err)
	fmt.Printf("         %s\n", additionalInfo)
}

// tryGetOLMReport attempts to detect and collect OLM information,
// returning a zipper function if successful, or nil if OLM is not available or permissions are insufficient
func tryGetOLMReport(ctx context.Context, format plugin.OutputFormat) zipFileWriter {
	discoveryClient, err := utils.GetDiscoveryClient()
	if err != nil {
		logWarning("could not get discovery client", err, "Continuing without OLM detection.")
		return nil
	}

	if err = utils.DetectOLM(discoveryClient); err != nil {
		logWarning("unable to look for OLM resources", err,
			"Continuing without OLM information. This is expected if you don't have cluster-level permissions.")
		return nil
	}

	if !utils.RunningOnOLM() {
		return nil
	}

	olmReport, err := getOlmReport(ctx, plugin.Namespace)
	if err != nil {
		logWarning("could not get openshift operator report", err,
			"Continuing without OLM report. This is expected if you don't have cluster-level permissions.")
		return nil
	}

	return func(zipper *zip.Writer, dirname string) error {
		return olmReport.writeToZip(zipper, format, dirname)
	}
}

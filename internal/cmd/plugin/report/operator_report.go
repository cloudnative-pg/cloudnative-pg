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
	newFolder := filepath.Join(folder, "manifests")
	_, err := zipper.Create(newFolder + "/")
	if err != nil {
		return err
	}

	// Always include deployment (required resource)
	if err := addContentToZip(or.deployment, "deployment", newFolder, format, zipper); err != nil {
		return err
	}

	// Include pods only if collected
	if len(or.operatorPods) > 0 {
		if err := addContentToZip(or.operatorPods, "operator-pods", newFolder, format, zipper); err != nil {
			return err
		}
	}

	// Include events only if collected
	if len(or.events.Items) > 0 {
		if err := addContentToZip(or.events, "events", newFolder, format, zipper); err != nil {
			return err
		}
	}

	// Include validating webhook config only if collected
	if or.validatingWebhookConfig != nil && len(or.validatingWebhookConfig.Items) > 0 {
		err := addContentToZip(or.validatingWebhookConfig, "validating-webhook-configuration",
			newFolder, format, zipper)
		if err != nil {
			return err
		}
	}

	// Include mutating webhook config only if collected
	if or.mutatingWebhookConfig != nil && len(or.mutatingWebhookConfig.Items) > 0 {
		err := addContentToZip(or.mutatingWebhookConfig, "mutating-webhook-configuration",
			newFolder, format, zipper)
		if err != nil {
			return err
		}
	}

	// Include webhook service only if collected (check for non-empty name)
	if or.webhookService.Name != "" {
		if err := addContentToZip(or.webhookService, "webhook-service", newFolder, format, zipper); err != nil {
			return err
		}
	}

	// Include configs and secrets only if collected
	if len(or.configs) > 0 {
		if err := addObjectsToZip(or.configs, newFolder, format, zipper); err != nil {
			return err
		}
	}

	if len(or.secrets) > 0 {
		if err := addObjectsToZip(or.secrets, newFolder, format, zipper); err != nil {
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
//
// operator implements the "report operator" subcommand
// Produces a zip file containing
//   - operator deployment (required - cannot generate report without identifying the operator)
//   - operator pod definition (optional - gracefully skipped if no permissions)
//   - operator configuration Configmap and Secret key (optional - gracefully skipped if no permissions)
//   - events in the operator namespace (optional - gracefully skipped if no permissions)
//   - operator's Validating/MutatingWebhookConfiguration and their associated services (optional)
//   - operator pod's logs (optional, if `includeLogs` is true)
func operator(ctx context.Context, format plugin.OutputFormat,
	file string, stopRedaction, includeLogs, logTimeStamp bool, now time.Time,
) error {
	// Configure redactors
	secretRedactor, configMapRedactor := configureRedactors(stopRedaction)

	// Collect deployment (the only truly required resource)
	deployment, err := getOperatorDeployment(ctx)
	if errors.Is(err, errNoOperatorDeployment) {
		return fmt.Errorf("%w\n"+
			"HINT: Operator might be installed in another namespace."+
			"Specify a namespace using the '-n' option", err)
	}
	if err != nil {
		return fmt.Errorf("could not get operator deployment: %w", err)
	}

	// Collect all optional resources (all can fail gracefully)
	pods := tryCollectPods(ctx)
	secrets, configs := tryCollectConfigurations(ctx, deployment, secretRedactor, configMapRedactor)
	events := tryCollectEvents(ctx, deployment.Namespace)
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

// tryCollectPods attempts to collect operator pods, logging warnings on failure
func tryCollectPods(ctx context.Context) []corev1.Pod {
	pods, err := getOperatorPods(ctx)
	if err != nil {
		logWarning("could not get operator pods", err,
			"Continuing without pod information. This is expected if you don't have permissions to list pods.")
		return nil
	}
	return pods
}

// tryCollectConfigurations attempts to collect secrets and configmaps, logging warnings on failure
func tryCollectConfigurations(
	ctx context.Context,
	deployment appsv1.Deployment,
	secretRedactor func(corev1.Secret) corev1.Secret,
	configMapRedactor func(corev1.ConfigMap) corev1.ConfigMap,
) ([]namedObject, []namedObject) {
	var secrets []namedObject
	var configs []namedObject

	// Try to collect secrets
	operatorSecrets, err := getOperatorSecrets(ctx, deployment)
	if err != nil {
		logWarning("could not get operator secrets", err,
			"Continuing without secrets information. This is expected if you don't have permissions to list secrets.")
	} else {
		secrets = make([]namedObject, 0, len(operatorSecrets))
		for _, ss := range operatorSecrets {
			secrets = append(secrets, namedObject{
				Name:   ss.Name + "(secret)",
				Object: secretRedactor(ss),
			})
		}
	}

	// Try to collect configmaps
	operatorConfigMaps, err := getOperatorConfigMaps(ctx, deployment)
	if err != nil {
		logWarning("could not get operator configmaps", err,
			"Continuing without configmap information. This is expected if you don't have permissions to list configmaps.")
	} else {
		configs = make([]namedObject, 0, len(operatorConfigMaps))
		for _, cm := range operatorConfigMaps {
			configs = append(configs, namedObject{
				Name:   cm.Name,
				Object: configMapRedactor(cm),
			})
		}
	}

	return secrets, configs
}

// tryCollectEvents attempts to collect events, logging warnings on failure
func tryCollectEvents(ctx context.Context, namespace string) corev1.EventList {
	var events corev1.EventList
	err := plugin.Client.List(ctx, &events, client.InNamespace(namespace))
	if err != nil {
		logWarning("could not get events", err,
			"Continuing without events information. This is expected if you don't have permissions to list events.")
		return corev1.EventList{}
	}
	return events
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

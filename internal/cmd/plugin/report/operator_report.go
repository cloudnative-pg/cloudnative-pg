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

// Package report implements the kubectl-cnp report command
package report

import (
	"archive/zip"
	"context"
	"fmt"
	"path/filepath"

	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/plugin"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/plugin/report/deployments"

	v12 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// operatorReport contains the data to be printed by the `report operator` plugin
type operatorReport struct {
	deployment              appsv1.Deployment
	operatorPod             corev1.Pod
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
		{content: or.operatorPod, name: "operator-pod"},
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

// Operator implements the "report operator" subcommand
// Produces a zip file containing
//  - operator deployment
//  - operator pod definition
//  - operator configuration Configmap and Secret key (if any)
//  - events in the operator namespace
//  - operator's Validating/MutatingWebhookConfiguration and their associated services
//  - operator pod's logs (if `includeLogs` is true)
func Operator(ctx context.Context, format plugin.OutputFormat,
	file string, stopRedaction, includeLogs bool,
) error {
	secretRedactor := redactSecret
	configMapRedactor := redactConfigMap
	if stopRedaction {
		secretRedactor = passSecret
		configMapRedactor = passConfigMap
		fmt.Println("WARNING: secret Redaction is OFF. Use it with caution")
	}

	operatorDeployment, err := deployments.GetOperatorDeployment(ctx)
	if err != nil {
		return fmt.Errorf("could not get operator deployment: %w", err)
	}

	operatorPod, err := deployments.GetOperatorPod(ctx)
	if err != nil {
		return fmt.Errorf("could not get operator pod: %w", err)
	}

	// TODO: parse configmap and secrets names from the deployment, as the client
	// may have overridden the defaults.
	// Currently we're getting the defaults only
	defaultSecrets := []string{
		"postgresql-operator-ca-secret",
		"postgresql-operator-webhook-cert",
		"postgresql-operator-controller-manager-config",
	}
	secrets := make([]namedObject, 0, len(defaultSecrets))
	for _, ss := range defaultSecrets {
		var secret corev1.Secret

		err := plugin.Client.Get(ctx, types.NamespacedName{Name: ss, Namespace: operatorPod.Namespace}, &secret)
		if err != nil {
			e1, ok := err.(*errors.StatusError)
			if ok && metav1.StatusReasonNotFound == e1.ErrStatus.Reason {
				continue
			}
			return fmt.Errorf("could not get secret '%s': %v", ss, err)
		}
		secrets = append(secrets, namedObject{Name: ss, Object: secretRedactor(secret)})
	}

	configMaps := []string{"postgresql-operator-controller-manager-config"}
	configs := make([]namedObject, 0, len(configMaps))
	for _, cm := range configMaps {
		var config corev1.ConfigMap

		err := plugin.Client.Get(ctx, types.NamespacedName{Name: cm, Namespace: operatorPod.Namespace}, &config)
		if err != nil {
			e1, ok := err.(*errors.StatusError)
			if ok && metav1.StatusReasonNotFound == e1.ErrStatus.Reason {
				continue
			}
			return fmt.Errorf("could not get config '%s': %v", cm, err)
		}

		configs = append(configs, namedObject{Name: cm, Object: configMapRedactor(config)})
	}

	var events corev1.EventList
	err = plugin.Client.List(ctx, &events, client.InNamespace(operatorPod.Namespace))
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
		operatorPod:             operatorPod,
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
			return streamPodLogsToZip(ctx, operatorPod, dirname, "operator-logs", zipper)
		}
		sections = append(sections, logZipper)
	}

	err = writeZippedReport(sections, file, reportName("operator"))
	if err != nil {
		return fmt.Errorf("could not write report: %w", err)
	}

	fmt.Printf("Successfully written report to \"%s\" (format: \"%s\")\n", file, format)

	return nil
}

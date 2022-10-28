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

package install

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"

	"github.com/spf13/cobra"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	machineryYaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
)

type generateExecutor struct {
	ctx                  context.Context
	watchNamespaces      string
	namespace            string
	replicas             int32
	userRequestedVersion string
}

func newGenerateCmd() *cobra.Command {
	var version, watchNamespaces string
	var replicas int32
	cmd := &cobra.Command{
		Use:   "generate",
		Short: "generates the manifests needed for the CNPG operator installation",
		RunE: func(cmd *cobra.Command, args []string) error {
			command := generateExecutor{
				userRequestedVersion: version,
				namespace:            plugin.Namespace,
				watchNamespaces:      watchNamespaces,
				replicas:             replicas,
				ctx:                  cmd.Context(),
			}
			return command.execute()
		},
	}

	cmd.Flags().StringVar(
		&version,
		"version",
		"",
		"The operator version to install. If not passed defaults to the latest version",
	)
	cmd.Flags().StringVar(
		&watchNamespaces,
		"watch-namespace",
		"",
		"makes the operator watch a single namespace. If empty watches all the namespaces",
	)
	cmd.Flags().Int32Var(
		&replicas,
		"replicas",
		0,
		"Overrides the operator deployment replicas. If zero applies the defaults value from the installation manifest",
	)

	return cmd
}

func (cmd *generateExecutor) execute() error {
	irs, err := cmd.getInstallationResources()
	if err != nil {
		return err
	}

	cmd.reconcileNamespace(irs)

	if err := reconcileResource(irs, cmd.reconcileOperatorDeployment); err != nil {
		return err
	}

	if err := reconcileResource(irs, cmd.reconcileOperatorConfig); err != nil {
		return err
	}

	return cmd.printResources(irs)
}

func (cmd *generateExecutor) printResources(irs []installResource) error {
	for _, ir := range irs {
		b, err := yaml.Marshal(ir.obj)
		if err != nil {
			return err
		}
		fmt.Println(string(b))
		fmt.Println("-----")
	}
	return nil
}

func (cmd *generateExecutor) parseInstallationResources(manifests []byte) ([]installResource, error) {
	var irs []installResource

	contextLogger := log.FromContext(cmd.ctx).WithName("parseInstallationResources")
	reader := bufio.NewReader(bytes.NewReader(manifests))
	yamlReader := machineryYaml.NewYAMLReader(reader)
	for {
		rawObj, err := yamlReader.Read()
		if errors.Is(err, io.EOF) {
			return irs, nil
		}

		if err != nil {
			contextLogger.Info("encountered an error while reading",
				"err", err,
			)
			break
		}

		ir, err := cmd.getInstallResource(rawObj)
		if err != nil {
			return nil, err
		}
		irs = append(irs, ir)
	}

	return irs, nil
}

func (cmd *generateExecutor) getInstallationResources() ([]installResource, error) {
	contextLogger := log.FromContext(cmd.ctx)

	version, err := cmd.getVersion()
	if err != nil {
		return nil, err
	}
	contextLogger.Info("fetching installation manifests", "branch", version)

	manifestURL := fmt.Sprintf(
		"https://raw.githubusercontent.com/cloudnative-pg/artifacts/%s/manifests/operator-manifest.yaml",
		version,
	)

	resp, err := http.Get(manifestURL) //nolint:gosec
	if err != nil {
		log.Error(err, "Error while requesting instance status")
		return nil, err
	}

	defer func() {
		err = resp.Body.Close()
		if err != nil {
			contextLogger.Error(err, "Can't close the connection",
				"manifestURL", manifestURL,
				"statusCode", resp.StatusCode,
			)
		}
	}()

	manifests, err := io.ReadAll(resp.Body)
	if err != nil {
		contextLogger.Error(err, "Error while reading status response body",
			"manifestURL", manifestURL,
			"statusCode", resp.StatusCode,
		)
		return nil, err
	}

	irs, err := cmd.parseInstallationResources(manifests)
	if err != nil {
		return nil, err
	}

	return irs, nil
}

func (cmd *generateExecutor) getInstallResource(rawObj []byte) (installResource, error) {
	for _, ir := range cmd.getSupportedInstallResources() {
		ir := ir
		err := machineryYaml.UnmarshalStrict(rawObj, ir.obj)
		if err != nil {
			continue
		}
		return ir, nil
	}

	return installResource{}, fmt.Errorf("could not parse the raw object: %s", string(rawObj))
}

type installResource struct {
	obj           client.Object
	isClusterWide bool
}

func (cmd *generateExecutor) getSupportedInstallResources() []installResource {
	return []installResource{
		{obj: &corev1.Namespace{}, isClusterWide: true},
		{obj: &appsv1.Deployment{}},
		{obj: &corev1.ServiceAccount{}},
		{obj: &corev1.ConfigMap{}},
		{obj: &apiextensionsv1.CustomResourceDefinition{}},
		{obj: &rbacv1.ClusterRole{}, isClusterWide: true},
		{obj: &rbacv1.ClusterRoleBinding{}, isClusterWide: true},
		{obj: &rbacv1.Role{}},
		{obj: &rbacv1.RoleBinding{}},
		{obj: &corev1.Service{}},
		{obj: &admissionregistrationv1.MutatingWebhookConfiguration{}},
		{obj: &admissionregistrationv1.ValidatingWebhookConfiguration{}},
	}
}

func (cmd *generateExecutor) reconcileOperatorDeployment(dep *appsv1.Deployment) error {
	if cmd.replicas == 0 {
		return nil
	}
	dep.Spec.Replicas = &cmd.replicas
	return nil
}

func (cmd *generateExecutor) reconcileOperatorConfig(cm *corev1.ConfigMap) error {
	if cmd.watchNamespaces == "" {
		return nil
	}
	// means it's not the operator configuration configmap
	if cm.Data["POSTGRES_IMAGE_NAME"] == "" {
		return nil
	}

	cm.Data["WATCH_NAMESPACES"] = cmd.watchNamespaces

	return nil
}

func (cmd *generateExecutor) reconcileNamespace(irs []installResource) {
	if cmd.namespace == "" {
		return
	}

	for _, ir := range irs {
		if ir.isClusterWide {
			continue
		}
		ir.obj.SetNamespace(cmd.namespace)
	}
}

func (cmd *generateExecutor) getVersion() (string, error) {
	if cmd.userRequestedVersion != "" {
		return fmt.Sprintf("release-%s", cmd.userRequestedVersion), nil
	}

	return cmd.getLatestOperatorVersion()
}

// Branch is an object returned by github query
type Branch struct {
	Name string `json:"name,omitempty"`
}

func (cmd *generateExecutor) getLatestOperatorVersion() (string, error) {
	contextLogger := log.FromContext(cmd.ctx)
	url := "https://api.github.com/repos/cloudnative-pg/artifacts/branches"
	resp, err := http.Get(url)
	defer func() {
		err = resp.Body.Close()
		if err != nil {
			contextLogger.Error(err, "Can't close the connection",
				"url", url,
				"statusCode", resp.StatusCode,
			)
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		contextLogger.Error(err, "Error while reading status response body",
			"url", url,
			"statusCode", resp.StatusCode,
		)
		return "", err
	}

	var tags []Branch
	if err := json.Unmarshal(body, &tags); err != nil {
		return "", err
	}
	if len(tags) == 0 {
		return "", fmt.Errorf("no branches found")
	}

	sort.Slice(tags, func(i, j int) bool {
		return tags[i].Name > tags[j].Name
	})

	return tags[0].Name, nil
}

type reconcileResourceCallback[T client.Object] func(obj T) error

func reconcileResource[T client.Object](
	irs []installResource,
	reconciler reconcileResourceCallback[T],
) error {
	for _, ir := range irs {
		if t, ok := ir.obj.(T); ok {
			if err := reconciler(t); err != nil {
				return err
			}
		}
	}
	return nil
}

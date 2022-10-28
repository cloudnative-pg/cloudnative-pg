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
	"errors"
	"fmt"
	"io"
	"net/http"

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

type options struct {
	watchNamespaces string
	namespace       string
	replicas        int32
	version         string
}

// NewCmd returns the installation root cmd
func NewCmd() *cobra.Command {
	var version, watchNamespaces string
	var replicas int32
	cmd := &cobra.Command{
		Use:   "install",
		Short: "generates an install manifest per cnpg",
		RunE: func(cmd *cobra.Command, args []string) error {
			return execute(
				cmd.Context(),
				options{
					version:         version,
					namespace:       plugin.Namespace,
					watchNamespaces: watchNamespaces,
					replicas:        replicas,
				})
		},
	}

	// TODO improve
	// TODO remove default 1.17 and fetch it dinamically?
	cmd.Flags().StringVar(&version, "version", "1.17", "version to fetch")
	cmd.Flags().StringVar(&watchNamespaces, "watch-namespaces", "", "")
	cmd.Flags().Int32Var(&replicas, "replicas", 0, "")

	return cmd
}

func execute(ctx context.Context, opt options) error {
	manifests, err := getInstallationManifest(ctx, opt.version)
	if err != nil {
		return err
	}

	irs, err := getInstallResources(ctx, manifests)
	if err != nil {
		return err
	}

	reconcileNamespaces(irs, opt.namespace)

	if err := reconcileResource(ctx, irs, opt, reconcileOperatorDeployment); err != nil {
		return err
	}

	if err := reconcileResource(ctx, irs, opt, reconcileOperatorConfig); err != nil {
		return err
	}

	return printManifest(irs)
}

func printManifest(irs []installResource) error {
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

func getInstallResources(ctx context.Context, manifests []byte) ([]installResource, error) {
	var irs []installResource

	contextLogger := log.FromContext(ctx).WithName("getInstallResources")
	reader := bufio.NewReader(bytes.NewReader(manifests))
	yamlReader := machineryYaml.NewYAMLReader(reader)
	for {
		rawObj, err := yamlReader.Read()
		if errors.Is(err, io.EOF) {
			log.Info("finished parsing manifest")
			return irs, nil
		}

		if err != nil {
			contextLogger.Info("encountered an error while reading",
				"err", err,
			)
			break
		}

		ir, err := getInstallResource(rawObj)
		if err != nil {
			return nil, err
		}
		irs = append(irs, ir)
	}

	return irs, nil
}

func getInstallationManifest(ctx context.Context, version string) ([]byte, error) {
	contextLogger := log.FromContext(ctx)
	manifestURL := buildManifestURL(version)

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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		contextLogger.Error(err, "Error while reading status response body",
			"manifestURL", manifestURL,
			"statusCode", resp.StatusCode,
		)
		return nil, err
	}

	return body, nil
}

func buildManifestURL(version string) string {
	return fmt.Sprintf(
		"https://raw.githubusercontent.com/cloudnative-pg/artifacts/release-%s/manifests/operator-manifest.yaml",
		version,
	)
}

func getInstallResource(rawObj []byte) (installResource, error) {
	for _, ir := range getSupportedInstallResources() {
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

func getSupportedInstallResources() []installResource {
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

type reconcileCallback[T client.Object] func(ctx context.Context, opt options, obj T) error

func reconcileResource[T client.Object](
	ctx context.Context,
	irs []installResource,
	opt options,
	callback reconcileCallback[T],
) error {
	for _, ir := range irs {
		if t, ok := ir.obj.(T); ok {
			if err := callback(ctx, opt, t); err != nil {
				return err
			}
		}
	}
	return nil
}

func reconcileOperatorDeployment(ctx context.Context, opt options, dep *appsv1.Deployment) error {
	if opt.replicas == 0 {
		return nil
	}
	dep.Spec.Replicas = &opt.replicas
	return nil
}

func reconcileOperatorConfig(ctx context.Context, opt options, cm *corev1.ConfigMap) error {
	if opt.watchNamespaces == "" {
		return nil
	}
	// means it's not the operator configuration configmap
	if cm.Data["POSTGRES_IMAGE_NAME"] == "" {
		return nil
	}

	cm.Data["WATCH_NAMESPACES"] = opt.watchNamespaces

	return nil
}

func reconcileNamespaces(irs []installResource, namespace string) {
	if namespace == "" {
		return
	}

	for _, ir := range irs {
		if ir.isClusterWide {
			continue
		}
		ir.obj.SetNamespace(namespace)
	}
}

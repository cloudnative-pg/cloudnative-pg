package postgres

import (
	"iter"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/cloudnative-pg/machinery/pkg/log"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/configfile"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/constants"
)

const (
	// ExtensionControlPath is the postgresql parameter key for extension_control_path
	ExtensionControlPath = "extension_control_path"

	// DynamicLibraryPath is the postgresql parameter key dynamic_library_path
	DynamicLibraryPath = "dynamic_library_path"

	// ExtensionsBaseDirectory is the base directory to store ImageVolume Extensions
	ExtensionsBaseDirectory = "/extensions"
)

// extensionItem is the configuration for an Extension added via ImageVolume
type extensionItem struct {
	// The name of the Extension
	Name string

	// The list of directories that should be added to ExtensionControlPath.
	ExtensionControlPath []string

	// The list of directories that should be added to DynamicLibraryPath.
	DynamicLibraryPath []string
}

type ExtensionsConfiguration struct {
	Configuration map[string]string `json:"configuration,omitempty"`
	Items         []extensionItem   `json:"items,omitempty"`
}

func newAdditionalExtensionsFromCluster(cluster *apiv1.Cluster) ExtensionsConfiguration {
	if len(cluster.Spec.PostgresConfiguration.Extensions) == 0 {
		return ExtensionsConfiguration{}
	}

	additionalExtensions := make([]extensionItem, len(cluster.Spec.PostgresConfiguration.Extensions))

	// Set additional extensions
	for idx, extension := range cluster.Spec.PostgresConfiguration.Extensions {
		additionalExtensions[idx] = extensionItem{
			Name:                 extension.Name,
			ExtensionControlPath: extension.ExtensionControlPath,
			DynamicLibraryPath:   extension.DynamicLibraryPath,
		}
	}

	return ExtensionsConfiguration{
		Items:         additionalExtensions,
		Configuration: cluster.Spec.PostgresConfiguration.ExtensionsConfiguration,
	}
}

// absolutizePaths returns an iterator over the passed paths, absolutized
// using the name of the extension
func (ext *extensionItem) absolutizePaths(paths []string) iter.Seq[string] {
	return func(yield func(string) bool) {
		for _, path := range paths {
			if !yield(filepath.Join(ExtensionsBaseDirectory, ext.Name, path)) {
				break
			}
		}
	}
}

// getRuntimeExtensionControlPath collects the absolute directories to be put
// into the `extension_control_path` GUC to support this additional extension
func (ext *extensionItem) getRuntimeExtensionControlPath() iter.Seq[string] {
	paths := []string{"share"}
	if len(ext.ExtensionControlPath) > 0 {
		paths = ext.ExtensionControlPath
	}

	return ext.absolutizePaths(paths)
}

// getDynamicLibraryPath collects the absolute directories to be put
// into the `dynamic_library_path` GUC to support this additional extension
func (ext *extensionItem) getDynamicLibraryPath() iter.Seq[string] {
	paths := []string{"lib"}
	if len(ext.DynamicLibraryPath) > 0 {
		paths = ext.DynamicLibraryPath
	}

	return ext.absolutizePaths(paths)
}

func configureExtensionsConfFile(
	pgData string,
	cluster *apiv1.Cluster,
) (changed bool, err error) {
	extConf := newAdditionalExtensionsFromCluster(cluster)

	targetFile := path.Join(pgData, constants.PostgresExtensionsConfigurationFile)

	config := make(map[string]string)
	for k, v := range extConf.Configuration {
		config[k] = v
	}
	// Setup additional extensions
	if len(extConf.Items) > 0 {
		setExtensionControlPath(config, extConf.Items)
		setDynamicLibraryPath(config, extConf.Items)
	}

	// Ensure that override.conf file contains just the above options
	changed, err = configfile.WritePostgresConfiguration(targetFile, config)
	if err != nil {
		return false, err
	}

	if changed {
		log.Info("Updated extensions settings", "filename", constants.PostgresExtensionsConfigurationFile)
	}

	return changed, nil
}

// setExtensionControlPath manages the `extension_control_path` GUC, merging
// the paths defined by the user with the ones provided by the
// `.spec.postgresql.extensions` stanza
func setExtensionControlPath(conf map[string]string, extensions []extensionItem) {
	extensionControlPath := []string{"$system"}

	for _, extension := range extensions {
		extensionControlPath = slices.AppendSeq(
			extensionControlPath,
			extension.getRuntimeExtensionControlPath(),
		)
	}

	extensionControlPath = slices.AppendSeq(
		extensionControlPath,
		strings.SplitSeq(conf[ExtensionControlPath], ":"),
	)

	extensionControlPath = slices.DeleteFunc(
		extensionControlPath,
		func(s string) bool { return s == "" },
	)

	conf[ExtensionControlPath] = strings.Join(extensionControlPath, ":")
}

// setDynamicLibraryPath manages the `dynamic_library_path` GUC, merging the
// paths defined by the user with the ones provided by the
// `.spec.postgresql.extensions` stanza
func setDynamicLibraryPath(config map[string]string, extensions []extensionItem) {
	dynamicLibraryPath := []string{"$libdir"}

	for _, extension := range extensions {
		dynamicLibraryPath = slices.AppendSeq(
			dynamicLibraryPath,
			extension.getDynamicLibraryPath())
	}

	dynamicLibraryPath = slices.AppendSeq(
		dynamicLibraryPath,
		strings.SplitSeq(config[DynamicLibraryPath], ":"))

	dynamicLibraryPath = slices.DeleteFunc(
		dynamicLibraryPath,
		func(s string) bool { return s == "" },
	)

	config[DynamicLibraryPath] = strings.Join(dynamicLibraryPath, ":")
}

// RefreshExtensionsConfiguration writes the PostgreSQL correct
// extensions configuration for the cluster, depending on the
// cluster extensions configuration
func (instance *Instance) RefreshExtensionsConfiguration(
	cluster *apiv1.Cluster,
) (changed bool, err error) {
	// Configure the extensions.conf file
	return configureExtensionsConfFile(instance.PgData, cluster)
}

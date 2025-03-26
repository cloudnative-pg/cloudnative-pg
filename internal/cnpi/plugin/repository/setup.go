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

package repository

import (
	"context"
	"crypto/tls"
	"os"
	"path"
	"sync"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/jackc/puddle/v2"
	"go.uber.org/multierr"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/connection"
)

// Interface is the interface to use a plugin repository
type Interface interface {
	// ForgetPlugin closes every connection to the plugin with the passed name
	// and forgets its discovery info.
	// This operation is synchronous and blocks until every connection is closed.
	// If the plugin was not available in the repository, this is a no-op.
	ForgetPlugin(name string)

	// RegisterRemotePlugin registers a plugin available on a remote
	// TCP entrypoint.
	RegisterRemotePlugin(name string, address string, tlsConfig *tls.Config) error

	// RegisterUnixSocketPluginsInPath scans the passed directory
	// for plugins that are deployed with unix sockets.
	// Return the list of loaded plugin names
	RegisterUnixSocketPluginsInPath(pluginsPath string) ([]string, error)

	// GetConnection gets a connection to the plugin with specified name
	GetConnection(ctx context.Context, name string) (connection.Interface, error)

	// Close closes all the connections to the plugins
	Close()
}

// data is the implementation of the plugin repository
// this structure implements the Interface interface
type data struct {
	mux                  sync.Mutex
	pluginConnectionPool map[string]*puddle.Pool[connection.Interface]
}

// pluginSetupOptions are the options to be used when setting up
// a plugin connection
type pluginSetupOptions struct {
	// forceRegistration forces the creation of a new plugin connection
	// even if one already exists. The existing connection will be closed.
	forceRegistration bool
}

// maxPoolSize is the maximum number of connections in a plugin's connection
// pool
const maxPoolSize = 5

func pluginConnectionConstructor(name string, protocol connection.Protocol) puddle.Constructor[connection.Interface] {
	return func(ctx context.Context) (connection.Interface, error) {
		logger := log.
			FromContext(ctx).
			WithName("setPluginProtocol").
			WithValues("pluginName", name)
		ctx = log.IntoContext(ctx, logger)

		logger.Trace("Connecting to plugin")
		var (
			result  connection.Interface
			handler connection.Handler
			err     error
		)

		if handler, err = protocol.Dial(ctx); err != nil {
			logger.Error(err, "Error while connecting to plugin (physical)")
			return nil, err
		}

		if result, err = connection.LoadPlugin(ctx, handler); err != nil {
			logger.Error(err, "Error while connecting to plugin (logical)")
			_ = handler.Close()
			return nil, err
		}

		return result, err
	}
}

func pluginConnectionDestructor(res connection.Interface) {
	logger := log.FromContext(context.Background()).
		WithName("pluginConnectionDestructor").
		WithValues("pluginName", res.Name())

	logger.Trace("Released physical plugin connection")

	err := res.Close()
	if err != nil {
		logger.Warning("Error while closing plugin connection", "err", err)
	}
}

func (r *data) setPluginProtocol(name string, protocol connection.Protocol, opts pluginSetupOptions) error {
	r.mux.Lock()
	defer r.mux.Unlock()

	if r.pluginConnectionPool == nil {
		r.pluginConnectionPool = make(map[string]*puddle.Pool[connection.Interface])
	}

	if oldPool, alreadyRegistered := r.pluginConnectionPool[name]; alreadyRegistered {
		if opts.forceRegistration {
			oldPool.Close()
		} else {
			return &ErrPluginAlreadyRegistered{
				Name: name,
			}
		}
	}

	var err error
	r.pluginConnectionPool[name], err = puddle.NewPool(
		&puddle.Config[connection.Interface]{
			Constructor: pluginConnectionConstructor(name, protocol),
			Destructor:  pluginConnectionDestructor,
			MaxSize:     maxPoolSize,
		},
	)
	if err != nil {
		return err
	}
	return nil
}

func (r *data) ForgetPlugin(name string) {
	r.mux.Lock()
	defer r.mux.Unlock()

	pool, ok := r.pluginConnectionPool[name]
	if !ok {
		return
	}

	pool.Close()
	delete(r.pluginConnectionPool, name)
}

// registerUnixSocketPlugin registers a plugin available at the passed
// unix socket path
func (r *data) registerUnixSocketPlugin(name, path string) error {
	return r.setPluginProtocol(name, connection.ProtocolUnix(path), pluginSetupOptions{
		// Forcing the registration of a Unix socket plugin has no meaning
		// because they can be installed and started only when the Pod is created.
		forceRegistration: false,
	})
}

func (r *data) RegisterRemotePlugin(name string, address string, tlsConfig *tls.Config) error {
	protocol := &connection.ProtocolTCP{
		TLSConfig: tlsConfig,
		Address:   address,
	}

	// The RegisterRemotePlugin function is called when the plugin is registered for
	// the first time and when the certificates of an existing plugin get refreshed.
	// In the second case, the plugin loading will be forced and all existing
	// connections will be dropped and recreated.
	opts := pluginSetupOptions{
		forceRegistration: true,
	}
	return r.setPluginProtocol(name, protocol, opts)
}

func (r *data) RegisterUnixSocketPluginsInPath(pluginsPath string) ([]string, error) {
	entries, err := os.ReadDir(pluginsPath)
	if err != nil {
		// There's no need to complain if the plugin folder doesn't exist
		if os.IsNotExist(err) {
			return nil, nil
		}

		// Otherwise, this means we can't read that folder and
		// is a real problem
		return nil, err
	}

	pluginsNames := make([]string, 0, len(entries))
	var errors error
	for _, entry := range entries {
		name := entry.Name()
		if err := r.registerUnixSocketPlugin(
			name,
			path.Join(pluginsPath, name),
		); err != nil {
			errors = multierr.Append(errors, err)
		} else {
			pluginsNames = append(pluginsNames, name)
		}
	}

	return pluginsNames, errors
}

// New creates a new plugin repository
func New() Interface {
	return &data{}
}

// Close closes all the connections to the plugins
func (r *data) Close() {
	r.mux.Lock()
	defer r.mux.Unlock()

	for _, pool := range r.pluginConnectionPool {
		pool.Close()
	}
}

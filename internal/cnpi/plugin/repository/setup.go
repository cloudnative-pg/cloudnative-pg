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
	// If the plug in was not available in the repository, this is a no-op
	ForgetPlugin(name string)

	// RegisterRemotePlugin registers a plugin available on a remote
	// TCP entrypoint
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

const maxPoolSize = 5

func (r *data) setPluginProtocol(name string, protocol connection.Protocol) error {
	r.mux.Lock()
	defer r.mux.Unlock()

	if r.pluginConnectionPool == nil {
		r.pluginConnectionPool = make(map[string]*puddle.Pool[connection.Interface])
	}

	_, ok := r.pluginConnectionPool[name]
	if ok {
		return &ErrPluginAlreadyRegistered{
			Name: name,
		}
	}

	constructor := func(ctx context.Context) (res connection.Interface, err error) {
		var handler connection.Handler

		defer func() {
			if err != nil && handler != nil {
				_ = handler.Close()
			}
		}()

		constructorLogger := log.
			FromContext(ctx).
			WithName("setPluginProtocol").
			WithValues("pluginName", name)
		ctx = log.IntoContext(ctx, constructorLogger)

		if handler, err = protocol.Dial(ctx); err != nil {
			constructorLogger.Error(err, "Got error while connecting to plugin")
			return nil, err
		}

		return connection.LoadPlugin(ctx, handler)
	}

	destructor := func(res connection.Interface) {
		err := res.Close()
		if err != nil {
			destructorLogger := log.FromContext(context.Background()).
				WithName("setPluginProtocol").
				WithValues("pluginName", res.Name())
			destructorLogger.Warning("Error while closing plugin connection", "err", err)
		}
	}

	var err error
	r.pluginConnectionPool[name], err = puddle.NewPool(
		&puddle.Config[connection.Interface]{
			Constructor: constructor,
			Destructor:  destructor,
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

	// TODO(leonardoce): should we really wait for all the plugin connections
	// to be closed?
	pool.Close()
}

// registerUnixSocketPlugin registers a plugin available at the passed
// unix socket path
func (r *data) registerUnixSocketPlugin(name, path string) error {
	return r.setPluginProtocol(name, connection.ProtocolUnix(path))
}

func (r *data) RegisterRemotePlugin(name string, address string, tlsConfig *tls.Config) error {
	return r.setPluginProtocol(name, &connection.ProtocolTCP{
		TLSConfig: tlsConfig,
		Address:   address,
	})
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

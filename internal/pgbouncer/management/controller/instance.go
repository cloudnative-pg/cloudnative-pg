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

package controller

import (
	"errors"
	"fmt"
	"os"
	"sync"

	"k8s.io/client-go/util/retry"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/pgbouncer/config"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/pool"
)

// PgBouncerInstanceInterface the public interface for a PgBouncer instance,
// implementations should be thread safe
type PgBouncerInstanceInterface interface {
	Paused() bool
	Pause() error
	Resume() error
	Reload() error
}

// NewPgBouncerInstance initializes a new pgBouncerInstance
func NewPgBouncerInstance() PgBouncerInstanceInterface {
	dsn := fmt.Sprintf(
		"host=%s port=%v user=%s sslmode=disable",
		config.PgBouncerSocketDir,
		config.PgBouncerPort,
		config.PgBouncerAdminUser,
	)

	return &pgBouncerInstance{
		mu:     &sync.RWMutex{},
		paused: false,
		pool:   pool.NewConnectionPool(dsn),
	}
}

type pgBouncerInstance struct {
	// The following two fields are used to keep track of
	// pgbouncer being paused or not
	mu     *sync.RWMutex
	paused bool

	// This is the connection pool used to connect to pgbouncer
	// using the administrative user and the administrative database
	pool *pool.ConnectionPool
}

// Paused returns whether the pgbouncerInstance is paused or not, thread safe
func (p *pgBouncerInstance) Paused() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.paused
}

// Pause the instance, thread safe
func (p *pgBouncerInstance) Pause() error {
	// First step: connect to the pgbouncer administrative database
	db, err := p.pool.Connection("pgbouncer")
	if err != nil {
		return fmt.Errorf("while connecting to pgbouncer database locally: %w", err)
	}

	// Second step: pause pgbouncer
	//
	// We are retrying the PAUSE query since we need to wait for
	// pgbouncer to be really up and the user could have created
	// a pooler which is paused from the start.
	err = retry.OnError(retry.DefaultBackoff, func(err error) bool {
		if errors.Is(err, os.ErrNotExist) {
			return true
		}
		return true
	}, func() error {
		_, err = db.Exec("PAUSE")
		return err
	})
	if err != nil {
		return err
	}

	// Third step: keep track of pgbouncer being paused
	p.mu.Lock()
	defer p.mu.Unlock()
	p.paused = true

	return nil
}

// Resume the instance, thread safe
func (p *pgBouncerInstance) Resume() error {
	// First step: connect to the pgbouncer administrative database
	db, err := p.pool.Connection("pgbouncer")
	if err != nil {
		return fmt.Errorf("while connecting to pgbouncer database locally: %w", err)
	}

	// Second step: resume pgbouncer
	_, err = db.Exec("RESUME")
	if err != nil {
		return fmt.Errorf("while resuming instance: %w", err)
	}

	// Third step: keep track of pgbouncer being resumed
	p.mu.Lock()
	defer p.mu.Unlock()
	p.paused = false

	return nil
}

// Reload issues a RELOAD command to the PgBouncer instance, returning any error
func (p *pgBouncerInstance) Reload() error {
	// First step: connect to the pgbouncer administrative database
	db, err := p.pool.Connection("pgbouncer")
	if err != nil {
		return fmt.Errorf("while connecting to pgbouncer database locally: %w", err)
	}

	// Second step: resume pgbouncer
	_, err = db.Exec("RELOAD")
	if err != nil {
		return fmt.Errorf("while reloading configuration: %w", err)
	}

	return nil
}

// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package loader_test

import (
	"context"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/altessa-s/go-nomos/loader"
	"github.com/altessa-s/go-nomos/loader/backend/yaml3"
)

// blockingManager blocks every lookup until its context ends, then reports
// why; started is closed on the first lookup.
type blockingManager struct {
	started chan struct{}
}

func (m *blockingManager) Resolve(ctx context.Context, _ string) (string, error) {
	select {
	case <-m.started:
	default:
		close(m.started)
	}
	<-ctx.Done()
	return "", ctx.Err()
}

type nameConfig struct {
	Name string `yaml:"name"`
}

// An ended context fails the load before any source or secret is read.
// It mutates the process environment via t.Setenv, so it is not parallel.
func TestLoadContext_EndedContext(t *testing.T) {
	t.Setenv("LOADCTXENDEDTEST_NAME", "$__secret{app:name}")
	manager := newCountingManager(map[string]string{"app:name": "x"})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := loader.New(&yaml3.Backend{}, loader.WithEnvPrefix("LOADCTXENDEDTEST"), loader.WithSecretResolver(manager)).
		LoadContext(ctx, &nameConfig{})
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, manager.count("app:name"), "no secret is looked up under an ended context")
}

// Canceling the context stops a secret lookup in progress, long before the
// per-lookup timeout.
// It mutates the process environment via t.Setenv, so it is not parallel.
func TestLoadContext_CancelStopsSecretLookup(t *testing.T) {
	t.Setenv("LOADCTXCANCELTEST_NAME", "$__secret{app:name}")
	manager := &blockingManager{started: make(chan struct{})}
	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		<-manager.started
		cancel()
	}()

	start := time.Now()
	_, err := loader.New(&yaml3.Backend{}, loader.WithEnvPrefix("LOADCTXCANCELTEST"), loader.WithSecretResolver(manager)).
		LoadContext(ctx, &nameConfig{})
	require.Error(t, err)
	require.Less(t, time.Since(start), 2*time.Second, "the lookup must end with the caller's context, not the 5s cap")
}

// Load keeps its behavior: a lookup without a caller context is capped at
// the per-lookup timeout and the load succeeds with a responsive manager.
// It mutates the process environment via t.Setenv, so it is not parallel.
func TestLoad_UsesBackgroundContext(t *testing.T) {
	t.Setenv("LOADCTXBGTEST_NAME", "$__secret{app:name}")
	manager := newCountingManager(map[string]string{"app:name": "resolved"})
	cfg := &nameConfig{}
	_, err := loader.New(&yaml3.Backend{}, loader.WithEnvPrefix("LOADCTXBGTEST"), loader.WithSecretResolver(manager)).Load(cfg)
	require.NoError(t, err)
	require.Equal(t, "resolved", cfg.Name)
}

// A load queued behind another load of the same loader ends with its own
// context instead of waiting for the one in progress, and never installs its
// struct as the loader's configuration.
// It mutates the process environment via t.Setenv, so it is not parallel.
func TestLoadContext_QueuedLoadHonorsItsContext(t *testing.T) {
	t.Setenv("LOADCTXQUEUETEST_NAME", "$__secret{app:name}")
	manager := &blockingManager{started: make(chan struct{})}
	l := loader.New(&yaml3.Backend{}, loader.WithEnvPrefix("LOADCTXQUEUETEST"), loader.WithSecretResolver(manager))

	firstCtx, cancelFirst := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, err := l.LoadContext(firstCtx, &nameConfig{})
		done <- err
	}()
	<-manager.started

	queuedCtx, cancelQueued := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancelQueued()
	start := time.Now()
	_, err := l.LoadContext(queuedCtx, &nameConfig{Name: "queued"})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Less(t, time.Since(start), 2*time.Second, "the queued load must not wait for the one in progress")

	cancelFirst()
	require.Error(t, <-done)
	if got, ok := l.Config().(*nameConfig); ok {
		require.NotEqual(t, "queued", got.Name, "the queued load must not replace the configuration")
	}
}

// cancelingConfig cancels the load's context from its Validate.
type cancelingConfig struct {
	Name   string `yaml:"name"`
	cancel context.CancelFunc
}

func (c *cancelingConfig) Validate() error {
	c.cancel()
	return nil
}

// A context that ends during the last phases (validation) fails the load
// instead of reporting success.
func TestLoadContext_EndsDuringValidation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	_, err := loader.New(&yaml3.Backend{}, loader.WithEnvPrefix("LOADCTXVALIDATETEST")).
		LoadContext(ctx, &cancelingConfig{cancel: cancel})
	require.ErrorIs(t, err, context.Canceled)
}

// cancelOnLookupManager serves a value successfully and cancels the load's
// context while doing so.
type cancelOnLookupManager struct {
	cancel context.CancelFunc
}

func (m *cancelOnLookupManager) Resolve(_ context.Context, key string) (string, error) {
	m.cancel()
	return "resolved", nil
}

// defaultingConfig records whether its Default hook ran.
type defaultingConfig struct {
	Name      string `yaml:"name"`
	defaulted *bool
}

func (c *defaultingConfig) Default() { *c.defaulted = true }

// A context canceled while the last environment secret is served stops the
// load before the defaults phase: Default is never invoked.
// It mutates the process environment via t.Setenv, so it is not parallel.
func TestLoadContext_CanceledBeforeDefaults(t *testing.T) {
	t.Setenv("LOADCTXDEFAULTSTEST_NAME", "$__secret{app:name}")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	defaulted := false

	_, err := loader.New(&yaml3.Backend{}, loader.WithEnvPrefix("LOADCTXDEFAULTSTEST"),
		loader.WithSecretResolver(&cancelOnLookupManager{cancel: cancel})).
		LoadContext(ctx, &defaultingConfig{defaulted: &defaulted})
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, defaulted, "Default must not run after the context ended")
}

// deadlineManager records the deadline of every lookup; the first lookup
// takes a while.
type deadlineManager struct {
	mu        sync.Mutex
	deadlines []time.Time
}

func (m *deadlineManager) Resolve(ctx context.Context, key string) (string, error) {
	deadline, _ := ctx.Deadline()
	m.mu.Lock()
	first := len(m.deadlines) == 0
	m.deadlines = append(m.deadlines, deadline)
	m.mu.Unlock()
	if first {
		time.Sleep(100 * time.Millisecond)
	}
	return "v", nil
}

// Every secret lookup gets its own timeout: the placeholders of one value
// do not share a single budget. The bubble's fake clock makes the first
// lookup's 100ms pause exact.
func TestLoadContext_SecretLookupTimeoutPerLookup(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		manager := &deadlineManager{}
		cfg := &struct {
			Token string `yaml:"token"`
		}{}
		err := loadFiles(t, nil, cfg, map[string]string{"a.yaml": "token: $__secret{app:a}-$__secret{app:b}\n"},
			loader.WithSecretResolver(manager))
		require.NoError(t, err)
		require.Len(t, manager.deadlines, 2)
		require.False(t, manager.deadlines[0].IsZero(), "a lookup has a timeout")
		require.Equal(t, 100*time.Millisecond, manager.deadlines[1].Sub(manager.deadlines[0]),
			"the second lookup's timeout starts with it")
	})
}

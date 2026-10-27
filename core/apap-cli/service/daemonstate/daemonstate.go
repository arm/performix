// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// Package daemonstate records and coordinates the current user's managed
// engine daemon. daemon.json contains the daemon's connection details; it is
// written during startup and removed after shutdown or by later stale-state
// cleanup. It may remain after a crash, so metadata is considered live only
// while the lifetime lock is held. Fixed-port daemons started outside the
// managed workflow are not recorded.
//
// The state lock serializes CLI decisions that read, replace, or remove the
// metadata, including the decision to start a daemon. For detached startup,
// the parent holds it until the child is ready or startup has failed. The child
// acquires the lifetime lock before publishing metadata and holds it until the
// daemon stops. During startup, the child publishes while the parent owns the
// state lock. After normal shutdown, it releases the lifetime lock before
// acquiring the state lock to clean up its metadata. On startup failure, the
// parent removes the metadata after the child exits.
//
// Code needing both locks must acquire the state lock first, then inspect or
// acquire the lifetime lock. A lifetime-lock owner must release it before
// acquiring the state lock. Lock files are persistent implementation details;
// only successfully holding a lock has meaning.
package daemonstate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"

	"github.com/Arm-Debug/apap-cli/apap-engine/userdirs"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

const (
	fileName         = "daemon.json"
	stateLockName    = "daemon.state.lock"
	lifetimeLockName = "daemon.running.lock"
	filePerm         = 0o600
	stateDirPerm     = 0o700
	lockPollInterval = 100 * time.Millisecond
)

var (
	ErrInvalidMetadata  = errors.New("invalid daemon metadata")
	ErrNoRunningDaemon  = errors.New("no running daemon")
	errLifetimeLockHeld = errors.New("daemon is already running")
)

type Daemon struct {
	Host       string `json:"host"`
	ServerPort int    `json:"server_port"`
}

type Store struct {
	path string
}

func NewStore() (*Store, error) {
	stateDir, err := userdirs.StateDir()
	if err != nil {
		return nil, err
	}
	return &Store{path: filepath.Join(stateDir, fileName)}, nil
}

func (s *Store) Read() (*Daemon, error) {
	daemon, err := util.ReadJSONFile[Daemon](s.path)
	if err != nil {
		var syntaxErr *json.SyntaxError
		var typeErr *json.UnmarshalTypeError
		if errors.As(err, &syntaxErr) || errors.As(err, &typeErr) {
			return nil, fmt.Errorf("%w: %v", ErrInvalidMetadata, err)
		}
		return nil, err
	}
	if err := daemon.validate(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidMetadata, err)
	}
	return daemon, nil
}

func (s *Store) Write(daemon Daemon) error {
	if err := daemon.validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidMetadata, err)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), stateDirPerm); err != nil {
		return err
	}
	return util.WriteJSONFileAtomic(s.path, &daemon, filePerm)
}

func (s *Store) Remove() error {
	err := os.Remove(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// FindRunningDaemon returns the daemon recorded in daemon.json when its
// lifetime lock is held. Missing, invalid, and stale metadata is cleaned up
// only when no daemon holds the lifetime lock.
func FindRunningDaemon(ctx context.Context) (*Daemon, error) {
	lock, err := AcquireStateLock(ctx)
	if err != nil {
		return nil, err
	}
	store, err := NewStore()
	if err != nil {
		_ = lock.Release()
		return nil, err
	}
	lifetimeLockHeld, err := LifetimeLockHeld()
	if err != nil {
		_ = lock.Release()
		return nil, err
	}
	daemon, readErr := store.Read()
	if readErr != nil {
		if !errors.Is(readErr, os.ErrNotExist) && !errors.Is(readErr, ErrInvalidMetadata) {
			_ = lock.Release()
			return nil, readErr
		}
		if lifetimeLockHeld {
			return nil, errors.Join(
				fmt.Errorf("daemon is running but its metadata is unavailable: %w", readErr),
				lock.Release(),
			)
		}
		removeErr := store.Remove()
		releaseErr := lock.Release()
		if removeErr != nil || releaseErr != nil {
			return nil, errors.Join(readErr, removeErr, releaseErr)
		}
		return nil, ErrNoRunningDaemon
	}
	if !lifetimeLockHeld {
		removeErr := store.Remove()
		releaseErr := lock.Release()
		if removeErr != nil || releaseErr != nil {
			return nil, errors.Join(removeErr, releaseErr)
		}
		return nil, ErrNoRunningDaemon
	}
	if err := lock.Release(); err != nil {
		return nil, err
	}
	return daemon, nil
}

// RemoveIfStopped waits for the daemon to exit, then removes daemon.json if it
// still identifies the daemon being stopped.
func RemoveIfStopped(ctx context.Context, stoppedDaemon Daemon) error {
	store, err := NewStore()
	if err != nil {
		return err
	}

	ticker := time.NewTicker(lockPollInterval)
	defer ticker.Stop()

	for {
		stateLock, err := AcquireStateLock(ctx)
		if err != nil {
			return err
		}

		current, readErr := store.Read()
		switch {
		case errors.Is(readErr, os.ErrNotExist):
			return stateLock.Release()
		case readErr != nil:
			return errors.Join(readErr, stateLock.Release())
		case *current != stoppedDaemon:
			return stateLock.Release()
		}

		lifetimeLock, err := AcquireLifetimeLock()
		switch {
		case err == nil:
			return errors.Join(store.Remove(), lifetimeLock.Release(), stateLock.Release())
		case errors.Is(err, errLifetimeLockHeld):
			if err := stateLock.Release(); err != nil {
				return err
			}
		default:
			return errors.Join(err, stateLock.Release())
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (e Daemon) validate() error {
	if e.Host == "" {
		return fmt.Errorf("host must not be empty")
	}
	if e.ServerPort < 1 || e.ServerPort > 65535 {
		return fmt.Errorf("server port must be between 1 and 65535")
	}
	return nil
}

type Lock struct {
	file *flock.Flock
}

func AcquireStateLock(ctx context.Context) (*Lock, error) {
	file, err := newLock(stateLockName)
	if err != nil {
		return nil, err
	}
	locked, err := file.TryLockContext(ctx, lockPollInterval)
	if err != nil {
		return nil, err
	}
	if !locked {
		return nil, ctx.Err()
	}
	return &Lock{file: file}, nil
}

// AcquireLifetimeLock acquires the lock held while the daemon recorded in
// daemon.json is running.
func AcquireLifetimeLock() (*Lock, error) {
	file, err := newLock(lifetimeLockName)
	if err != nil {
		return nil, err
	}
	locked, err := file.TryLock()
	if err != nil {
		return nil, err
	}
	if !locked {
		return nil, errLifetimeLockHeld
	}
	return &Lock{file: file}, nil
}

// LifetimeLockHeld reports whether the daemon recorded in daemon.json holds
// its lifetime lock.
func LifetimeLockHeld() (bool, error) {
	lock, err := AcquireLifetimeLock()
	if errors.Is(err, errLifetimeLockHeld) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return false, lock.Release()
}

func newLock(name string) (*flock.Flock, error) {
	stateDir, err := userdirs.StateDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(stateDir, stateDirPerm); err != nil {
		return nil, err
	}
	path := filepath.Join(stateDir, name)
	lockFile, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, filePerm)
	if err != nil {
		return nil, err
	}
	if err := lockFile.Close(); err != nil {
		return nil, err
	}
	if err := os.Chmod(path, filePerm); err != nil {
		return nil, err
	}
	return flock.New(path), nil
}

func (l *Lock) Release() error {
	return l.file.Unlock()
}

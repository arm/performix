// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package privilege

import (
	"encoding/base64"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokenStorage_HappyPath(t *testing.T) {
	t.Run("successfully generates a token", func(t *testing.T) {
		cfg := WithDefaultTokenStorageConfig()

		ts, err := NewTokenStorage(cfg)
		if err != nil {
			t.Fatalf("failed to create token storage: %v", err)
		}

		token, err := ts.Generate()
		require.NoError(t, err)
		require.NotEmpty(t, token)

		// should be base64 encoded
		decoded, err := base64.RawURLEncoding.DecodeString(token)
		require.NoError(t, err)
		assert.Len(t, decoded, cfg.KeyLength)
	})

	t.Run("successfully generates unique tokens", func(t *testing.T) {
		cfg := WithDefaultTokenStorageConfig()

		ts, err := NewTokenStorage(cfg)
		require.NoError(t, err)

		const numTokens = 64
		tokens := make(map[string]struct{})

		for i := 0; i < numTokens; i++ {
			token, err := ts.Generate()
			require.NoError(t, err)
			require.NotEmpty(t, token)

			if _, exists := tokens[token]; exists {
				t.Fatalf("token collision on token: %s", token)
			}
			tokens[token] = struct{}{}
		}

		assert.Len(t, tokens, numTokens)
	})

	t.Run("successfully validates a token", func(t *testing.T) {
		cfg := WithDefaultTokenStorageConfig()

		ts, err := NewTokenStorage(cfg)
		require.NoError(t, err)

		token, err := ts.Generate()
		require.NoError(t, err)
		require.NotEmpty(t, token)

		valid := ts.Validate(token)
		assert.True(t, valid)
	})

	t.Run("successfully validates non-existent token as invalid", func(t *testing.T) {
		cfg := WithDefaultTokenStorageConfig()

		ts, err := NewTokenStorage(cfg)
		require.NoError(t, err)

		valid := ts.Validate("non-existent-token")
		assert.False(t, valid)
	})

	t.Run("successfully validates expired token as invalid", func(t *testing.T) {
		cfg := WithDefaultTokenStorageConfig()
		cfg.TTL = time.Millisecond * 10

		ts, err := NewTokenStorage(cfg)
		require.NoError(t, err)

		token, err := ts.Generate()
		require.NoError(t, err)
		require.NotEmpty(t, token)

		// Wait for token to expire
		time.Sleep(cfg.TTL * 10)

		valid := ts.Validate(token)
		assert.False(t, valid)
	})

	t.Run("successfully refreshes a token", func(t *testing.T) {
		cfg := WithDefaultTokenStorageConfig()

		ts, err := NewTokenStorage(cfg)
		require.NoError(t, err)

		token, err := ts.Generate()
		require.NoError(t, err)
		require.NotEmpty(t, token)

		err = ts.Refresh(token)
		require.NoError(t, err)

		assert.True(t, ts.Validate(token))
	})

	t.Run("successfully revokes a token", func(t *testing.T) {
		cfg := WithDefaultTokenStorageConfig()

		ts, err := NewTokenStorage(cfg)
		require.NoError(t, err)

		token, err := ts.Generate()
		require.NoError(t, err)
		require.NotEmpty(t, token)

		ts.Revoke(token)

		assert.False(t, ts.Validate(token))
	})

	t.Run("successfully revokes all tokens", func(t *testing.T) {
		cfg := WithDefaultTokenStorageConfig()

		ts, err := NewTokenStorage(cfg)
		require.NoError(t, err)

		const numTokens = 10
		tokens := make([]string, 0, numTokens)
		for i := 0; i < numTokens; i++ {
			token, err := ts.Generate()
			require.NoError(t, err)
			require.NotEmpty(t, token)
			tokens = append(tokens, token)
		}

		ts.RevokeAll()

		for _, token := range tokens {
			assert.False(t, ts.Validate(token))
		}
	})
}

func TestTokenStorage_Failures(t *testing.T) {
	t.Run("fails to creeate token storage if config is invalid", func(t *testing.T) {
		cfg := WithDefaultTokenStorageConfig()
		cfg.KeyLength = maxKeyLength + 1

		_, err := NewTokenStorage(cfg)
		require.Error(t, err)
		assert.Equal(t, ErrInvalidKeyLength, err)

		cfg = WithDefaultTokenStorageConfig()
		cfg.TTL = maxTTL + 1

		_, err = NewTokenStorage(cfg)
		require.Error(t, err)
		assert.Equal(t, ErrInvalidTTL, err)

		cfg = WithDefaultTokenStorageConfig()
		cfg.GCPeriod = maxGCPeriod + 1

		_, err = NewTokenStorage(cfg)
		require.Error(t, err)
		assert.Equal(t, ErrInvalidGCPeriod, err)

		cfg = WithDefaultTokenStorageConfig()
		cfg.AbsoluteTTL = maxTTL + 1

		_, err = NewTokenStorage(cfg)
		require.Error(t, err)
		assert.Equal(t, ErrInvalidAbsoluteTTL, err)
	})

	t.Run("fails to refresh non-existent token", func(t *testing.T) {
		cfg := WithDefaultTokenStorageConfig()

		ts, err := NewTokenStorage(cfg)
		require.NoError(t, err)

		err = ts.Refresh("non-existent-token")
		require.Error(t, err)
		assert.Equal(t, ErrTokenNotFound, err)
	})

	t.Run("fails to refresh expired token", func(t *testing.T) {
		cfg := WithDefaultTokenStorageConfig()
		cfg.TTL = time.Millisecond * 10

		ts, err := NewTokenStorage(cfg)
		require.NoError(t, err)

		token, err := ts.Generate()
		require.NoError(t, err)
		require.NotEmpty(t, token)

		// Wait for token to expire
		time.Sleep(cfg.TTL * 10)

		err = ts.Refresh(token)
		require.Error(t, err)
		assert.Equal(t, ErrTokenExpired, err)
	})

	t.Run("fails to release non-existent token", func(t *testing.T) {
		cfg := WithDefaultTokenStorageConfig()
		ts, err := NewTokenStorage(cfg)
		require.NoError(t, err)

		err = ts.Release("i-dont-exist", true)
		require.Error(t, err)
		assert.Equal(t, ErrTokenNotFound, err)
	})
}

func TestTokenStorage_GarbageCollection(t *testing.T) {
	t.Run("successfully revokes time expired tokens", func(t *testing.T) {
		cfg := WithDefaultTokenStorageConfig()
		cfg.TTL = time.Millisecond * 10
		cfg.GCPeriod = time.Millisecond * 20

		ts, err := NewTokenStorage(cfg)
		require.NoError(t, err)

		token, err := ts.Generate()
		require.NoError(t, err)
		require.NotEmpty(t, token)

		// Ensure token is valid initially
		assert.True(t, ts.Validate(token))

		// Wait for token to expire and GC to run
		time.Sleep((cfg.TTL + cfg.GCPeriod) * 10)

		// Token should be invalid after GC
		assert.False(t, ts.Validate(token))
	})

	t.Run("successfully retains valid tokens", func(t *testing.T) {
		cfg := WithDefaultTokenStorageConfig()
		cfg.TTL = time.Minute * 1
		cfg.GCPeriod = time.Millisecond * 20

		ts, err := NewTokenStorage(cfg)
		require.NoError(t, err)

		token, err := ts.Generate()
		require.NoError(t, err)
		require.NotEmpty(t, token)

		// Ensure token is valid initially
		assert.True(t, ts.Validate(token))

		// Wait for GC to run a few times
		time.Sleep(cfg.GCPeriod * 10)

		// Token should still be valid
		assert.True(t, ts.Validate(token))
	})

	t.Run("successfully invokes OnEmpty callback when token storage becomes empty", func(t *testing.T) {
		cfg := WithDefaultTokenStorageConfig()
		cfg.TTL = time.Millisecond * 10
		cfg.GCPeriod = time.Millisecond * 20

		// Cause the token storage to become empty 4 times
		// OnEmpty should be invoked exactly 4 times
		const iters = 4
		var onEmptyFinished sync.WaitGroup
		onEmptyFinished.Add(iters)
		cfg.OnEmpty = func() {
			onEmptyFinished.Done()
		}

		ts, err := NewTokenStorage(cfg)
		require.NoError(t, err)

		for i := 0; i < iters; i++ {
			token, err := ts.Generate()
			require.NoError(t, err)
			require.NotEmpty(t, token)

			// Wait for token to expire and GC to run
			time.Sleep((cfg.TTL + cfg.GCPeriod) * 10)

			// Token should be invalid after GC
			require.False(t, ts.Validate(token))
		}

		doneCh := make(chan struct{})
		go func() {
			onEmptyFinished.Wait()
			close(doneCh)
		}()

		select {
		case <-doneCh:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for OnEmpty callback to be invoked")
		}
	})
}

func TestTokenStorage_Concurrency(t *testing.T) {
	cfg := WithDefaultTokenStorageConfig()

	// Keep the garbage collector busy
	// While we stress the token store
	cfg.GCPeriod = 1 * time.Millisecond

	ts, err := NewTokenStorage(cfg)
	require.NoError(t, err)

	const goroutines = 64

	tokens := make([]string, goroutines)
	for i := 0; i < goroutines; i++ {
		tok, genErr := ts.Generate()
		require.NoError(t, genErr)
		tokens[i] = tok
	}

	var wg sync.WaitGroup
	errCh := make(chan error, goroutines)

	// Each goroutine validates -> refreshes -> re-validates -> revokes
	// None of them should collide with each other AND with the garbage collector
	wg.Add(goroutines)
	for _, tok := range tokens {
		go func(tok string) {
			defer wg.Done()

			if ok := ts.Validate(tok); !ok {
				errCh <- fmt.Errorf("validate failed for token %q", tok)
				return
			}

			if refreshErr := ts.Refresh(tok); refreshErr != nil {
				errCh <- fmt.Errorf("refresh failed for token %q: %w", tok, refreshErr)
				return
			}

			if ok := ts.Validate(tok); !ok {
				errCh <- fmt.Errorf("re-validate failed for token %q", tok)
				return
			}

			ts.Revoke(tok)
			if ok := ts.Validate(tok); ok {
				errCh <- fmt.Errorf("revoke failed for token %q", tok)
			}
		}(tok)
	}

	doneCh := make(chan struct{})
	go func() {
		wg.Wait()
		close(doneCh)
	}()

	select {
	case <-doneCh:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for goroutines to finish")
	}
	close(errCh)

	for err := range errCh {
		t.Error(err)
	}
}

// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package privilege

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
)

// Defaults
const (
	defaultKeyLength = 32 // bytes (256 bits)
	defaultTTL       = 5 * time.Minute
	defaultGCPeriod  = 10 * time.Second

	maxKeyLength = 1024 // bytes (8192 bits)
	maxTTL       = 24 * time.Hour
	maxGCPeriod  = 1 * time.Hour
)

// TokenStorage defines the interface to manage a collection of tokens.
// Tokens are non-forgable unique random array of characters.
// Their lifetime is managed by the functions in this interface.
type TokenStorage interface {
	// Generate generates a new token.
	Generate() (string, error)

	// Validate checks if the provided token is valid.
	Validate(id string) bool

	// Refresh resets the lifetime of the provided token.
	Refresh(id string) error

	// Revoke invalidates the provided token.
	Revoke(id string)

	// RevokeAll invalidates all tokens.
	RevokeAll()

	// Acquire increases the provided token's reference count.
	Acquire(id string) error

	// Release decreases the provided token's reference count.
	Release(id string, shouldRefresh bool) error
}

var (
	ErrInvalidTTL         = fmt.Errorf("TTL out of range (0 < TTL <= %s)", maxTTL)
	ErrInvalidAbsoluteTTL = fmt.Errorf("AbsoluteTTL out of range (0 < AbsoluteTTL <= %s)", maxTTL)
	ErrInvalidGCPeriod    = fmt.Errorf("GCPeriod out of range (0 < GCPeriod <= %s)", maxGCPeriod)
	ErrInvalidKeyLength   = fmt.Errorf("KeyLength out of range (0 < KeyLength <= %d)", maxKeyLength)

	ErrTokenNotFound = errors.New("token not found")
	ErrTokenExpired  = errors.New("token expired")
	ErrTokenClash    = errors.New("token clash")
)

// TokenStorageConfig holds the configuration for a TokenStorage implementation.
type TokenStorageConfig struct {
	// TTL (time-to-live) is the lifetime of a token.
	// After TTL is exceeded, the token will be invalid.
	TTL time.Duration

	// AbsoluteTTL is the maximum lifetime of a token.
	// After AbsoluteTTL is exceeded, the token will be invalid regardless of refreshes.
	AbsoluteTTL time.Duration

	// GCPeriod (garbage collection period) is the interval at which
	// tokens are checked for validty and then revoked if invalid.
	GCPeriod time.Duration

	// KeyLength is the length of a generated token in bytes.
	KeyLength int

	// OnEmpty is an optional callback function that is called
	// when the token storage becomes empty. It will be invoked
	// in a separate goroutine to avoid long storage locks.
	OnEmpty func()
}

// Token represents a single token metadata.
type Token struct {
	// createdAt is the time the token was created.
	createdAt time.Time

	// modifiedAt is the time the token was last modified (i.e., created or refreshed).
	modifiedAt time.Time

	// expiresAt is the time the token will expire based on TTL.
	expiresAt time.Time

	// absoluteExpAt is the time token will expire based on AbsoluteTTL.
	absoluteExpAt time.Time

	// refCount is the number of active references to this token.
	refCount int
}

// isExpired checks if the token is expired based on the given time.
func (t Token) isExpired(now time.Time) bool {
	return now.After(t.expiresAt) || now.After(t.absoluteExpAt)
}

// shouldRevoke checks if the token should be revoked.
// Skip tokens that are in use (refCount > 0)
// Tokens whose absolute TTL has expired are revoked no matter what.
func (t Token) shouldRevoke(now time.Time) bool {
	return t.isExpired(now) && (t.refCount == 0 || now.After(t.absoluteExpAt))
}

// TokenStorageImpl is the in-memory implementation of TokenStorage.
type TokenStorageImpl struct {
	config TokenStorageConfig

	// gcTicker is the ticker for garbage collection routine triggered every GCPeriod.
	gcTicker time.Ticker

	tokens map[string]Token
	mu     sync.RWMutex
}

func WithDefaultTokenStorageConfig() TokenStorageConfig {
	return TokenStorageConfig{
		TTL:         defaultTTL,
		AbsoluteTTL: maxTTL,
		KeyLength:   defaultKeyLength,
		GCPeriod:    defaultGCPeriod,
	}
}

// NewTokenStorage creates a new TokenStorage instance with the given configuration.
// It starts a gorotuine (called garbage collector) that checks the validity of tokens
// and then revokes them if they are invalid.
// The interval of the garbege collecter is configured by the given TokenStorageConfig.
func NewTokenStorage(config TokenStorageConfig) (TokenStorage, error) {
	// Validate config
	if config.TTL <= 0 || config.TTL > maxTTL {
		return nil, ErrInvalidTTL
	}
	if config.AbsoluteTTL <= 0 || config.AbsoluteTTL > maxTTL {
		return nil, ErrInvalidAbsoluteTTL
	}
	if config.GCPeriod <= 0 || config.GCPeriod > maxGCPeriod {
		return nil, ErrInvalidGCPeriod
	}
	if config.KeyLength <= 0 || config.KeyLength > maxKeyLength {
		return nil, ErrInvalidKeyLength
	}

	ts := &TokenStorageImpl{
		config: config,
		tokens: make(map[string]Token),
	}

	// Start expired token collector routine
	go ts.gc(config.GCPeriod)

	return ts, nil
}

// gc is the internal garbage collector routine.
func (ts *TokenStorageImpl) gc(period time.Duration) {
	ts.gcTicker = *time.NewTicker(period)
	defer ts.gcTicker.Stop()

	for range ts.gcTicker.C {
		now := time.Now()
		var expired []string

		// Mark tokens for revocation
		// Once marked, there is no way to unmark them -- refresh won't work
		ts.mu.RLock()
		for id, tok := range ts.tokens {
			if tok.shouldRevoke(now) {
				expired = append(expired, id)
			}
		}
		ts.mu.RUnlock()

		if len(expired) == 0 {
			continue
		}

		// Revoke in batch
		ts.mu.Lock()
		for _, id := range expired {
			delete(ts.tokens, id)
		}

		// Invoke OnEmpty callback as we have just removed some tokens
		if ts.config.OnEmpty != nil && len(ts.tokens) == 0 {
			go ts.config.OnEmpty()
		}
		ts.mu.Unlock()
	}
}

// Generate generates a new token and stores it in the tokens map.
// The token is a base64 encoded random array of characters.
// The size of the token is KeyLength * 4/3 bytes rounded up (due to base64 encoding).
// The lifetime of the token is tied to the TTL in the config.
func (ts *TokenStorageImpl) Generate() (string, error) {
	// Generate a cryptographically secure random number
	buf := make([]byte, ts.config.KeyLength)
	_, err := rand.Read(buf)
	if err != nil {
		return "", message.New(message.AgentElevatePrivilegesGenerateTokenFailed).
			WithCause(err)
	}

	// Encode to base64 to get a fixed length string
	// We use RawURLEncoding to avoid padding and to get a fixed-length string
	id := base64.RawURLEncoding.EncodeToString(buf)

	ts.mu.Lock()
	defer ts.mu.Unlock()

	now := time.Now()

	// Handle clashes (even though practically impossible)
	if _, exists := ts.tokens[id]; exists {
		return "", message.New(message.AgentElevatePrivilegesGenerateTokenFailed).
			WithCause(ErrTokenClash)
	}

	ts.tokens[id] = Token{
		createdAt:     now,
		modifiedAt:    now,
		expiresAt:     now.Add(ts.config.TTL),
		absoluteExpAt: now.Add(ts.config.AbsoluteTTL),
	}

	return id, nil
}

// Validate checks if the provided token is valid.
// A token is valid if it exists and has not expired.
// A token is considered expired if either (i) its TTL has exceeded or (ii) its context is done.
func (ts *TokenStorageImpl) Validate(id string) bool {
	ts.mu.RLock()
	token, exists := ts.tokens[id]
	ts.mu.RUnlock()

	if !exists {
		return false
	}

	// We don't revoke it if expired as it'll be cleaned up by gc
	if token.isExpired(time.Now()) {
		return false
	}

	return true
}

// Refresh resets the TTL of the provided token.
func (ts *TokenStorageImpl) Refresh(id string) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	token, exists := ts.tokens[id]
	if !exists {
		return ErrTokenNotFound
	}

	// Prevent refreshing expired tokens
	now := time.Now()
	if token.isExpired(now) {
		return ErrTokenExpired
	}

	token.modifiedAt = now
	token.expiresAt = now.Add(ts.config.TTL)
	ts.tokens[id] = token

	return nil
}

// Revoke removes the provided token from the tokens map.
// Doing so invalidates the token as it no longer exists in the map.
func (ts *TokenStorageImpl) Revoke(id string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	delete(ts.tokens, id)
}

// RevokeAll removes all tokens by reinitializing the map.
func (ts *TokenStorageImpl) RevokeAll() {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	// Go's garbage collector will clean up the old one
	ts.tokens = make(map[string]Token)
}

// Acquire increases the reference count of the provided token.
// Tokens whose reference count > 0 are considered in-use and won't be revoked
// by the garbage collector. Manual revocation via Revoke or RevokeAll still works.
func (ts *TokenStorageImpl) Acquire(id string) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	token, exists := ts.tokens[id]
	if !exists {
		return ErrTokenNotFound
	}

	// Prevent acquiring expired tokens
	now := time.Now()
	if token.isExpired(now) {
		return ErrTokenExpired
	}

	token.refCount++
	ts.tokens[id] = token

	return nil
}

// Release decreases the reference count of the provided token.
func (ts *TokenStorageImpl) Release(id string, shouldRefresh bool) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	token, exists := ts.tokens[id]
	if !exists {
		return ErrTokenNotFound
	}

	if shouldRefresh {
		now := time.Now()
		token.modifiedAt = now
		token.expiresAt = now.Add(ts.config.TTL)
	}

	if token.refCount > 0 {
		token.refCount--
		ts.tokens[id] = token
	}

	return nil
}

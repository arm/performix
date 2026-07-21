// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"fmt"
	"sync"

	log "github.com/sirupsen/logrus"
)

// SessionAccess wraps a session and provides synchronization for concurrent access.
type SessionAccess struct {
	S Session
	m sync.RWMutex
}

// SessionStorage is a container for render sessions with thread-safe access.
type SessionStorage struct {
	sessions    map[string]*SessionAccess
	storageLock sync.Mutex
}

func NewSessionStorage() SessionStorage {
	return SessionStorage{sessions: map[string]*SessionAccess{}}
}

// Done unlocks the session access for writing or closing.
// It should be called when the session access is done.
func (sl *SessionAccess) Done() {
	sl.m.RUnlock()
}

// getRawSessionByID retrieves a session by its ID without locking it.
// This method is not thread-safe and should only be used internally within the storage methods.
func (s *SessionStorage) getRawSessionByID(id string) *SessionAccess {
	s.storageLock.Lock()
	defer s.storageLock.Unlock()

	sessionAccess, exists := s.sessions[id]
	if !exists {
		return nil
	}
	return sessionAccess
}

// AddRenderSession adds a new render session to the storage.
// Returns an error if a session with the same ID already exists.
func (s *SessionStorage) AddRenderSession(session Session) error {
	s.storageLock.Lock()
	defer s.storageLock.Unlock()

	if _, exists := s.sessions[session.ID()]; exists {
		return fmt.Errorf("session with id '%s' already exists in storage", session.ID())
	}
	s.sessions[session.ID()] = &SessionAccess{S: session}
	return nil
}

// CloseRenderSession closes a render session by its ID.
// It waits for any ongoing operations on the session to complete before closing it.
func (s *SessionStorage) CloseRenderSession(sessionID string) {
	var sessionAccess = s.getRawSessionByID(sessionID)

	if sessionAccess != nil {
		sessionAccess.m.Lock()
		defer sessionAccess.m.Unlock()

		log.WithField("Id", sessionID).Info("closing render session")

		sessionAccess.S.Close()

		s.storageLock.Lock()
		defer s.storageLock.Unlock()
		delete(s.sessions, sessionID)
	}
}

// CloseAllRenderSessions closes all render sessions in the storage.
// It waits for any ongoing operations on each session to complete before closing them.
func (s *SessionStorage) CloseAllRenderSessions() {
	for _, sessionAccess := range s.sessions {
		s.CloseRenderSession(sessionAccess.S.ID())
	}
}

// SessionRegistered checks if a session with the given ID is registered in the storage.
func (s *SessionStorage) SessionRegistered(id string) bool {
	s.storageLock.Lock()
	defer s.storageLock.Unlock()

	_, exists := s.sessions[id]
	return exists
}

// GetSessionByID retrieves a session by its ID and locks it for thread-safe access.
// The caller must ensure to unlock the session after use by calling Done().
// Returns an error if the session does not exist.
func (s *SessionStorage) GetSessionByID(id string) (*SessionAccess, error) {
	var sessionAccess = s.getRawSessionByID(id)

	if sessionAccess != nil {
		sessionAccess.m.RLock()
		return sessionAccess, nil
	}

	return nil, fmt.Errorf("session with id '%s' does not exist", id)
}

// GetAllSessionIds returns a list of the IDs of all sessions stored in the session storage.
func (s *SessionStorage) GetAllSessionIds() []string {
	s.storageLock.Lock()
	defer s.storageLock.Unlock()

	var sessionIds []string
	for id := range s.sessions {
		sessionIds = append(sessionIds, id)
	}

	return sessionIds
}

// SessionCount returns the number of sessions currently stored in the session storage.
func (s *SessionStorage) SessionCount() int {
	s.storageLock.Lock()
	defer s.storageLock.Unlock()

	return len(s.sessions)
}

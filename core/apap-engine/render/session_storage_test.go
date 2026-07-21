// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddSession(t *testing.T) {
	t.Run("add succeeds", func(t *testing.T) {

		session := MockSession{}
		session.On("ID").Return("sessionID")

		sessionStorage := NewSessionStorage()

		addErr := sessionStorage.AddRenderSession(&session)
		assert.NoError(t, addErr)
		assert.True(t, sessionStorage.SessionRegistered(session.ID()))
	})

	t.Run("duplicate session ID produces error", func(t *testing.T) {

		session := MockSession{}
		session.On("ID").Return("sessionID")

		duplicateSession := MockSession{}
		duplicateSession.On("ID").Return("sessionID")

		sessionStorage := NewSessionStorage()

		addErr := sessionStorage.AddRenderSession(&session)
		assert.NoError(t, addErr)

		addErr = sessionStorage.AddRenderSession(&duplicateSession)
		assert.ErrorContains(t, addErr, "session with id 'sessionID' already exists in storage")
	})
}

func TestCloseSession(t *testing.T) {
	t.Run("session closes", func(t *testing.T) {

		session := MockSession{}
		session.On("Close").Return()
		session.On("ID").Return("sessionID")

		sessionStorage := NewSessionStorage()

		addErr := sessionStorage.AddRenderSession(&session)
		assert.NoError(t, addErr)

		sessionStorage.CloseRenderSession(session.ID())

		session.AssertExpectations(t)
	})
}

func TestCloseAllRenderSessions(t *testing.T) {
	t.Run("all sessions closed", func(t *testing.T) {
		sessionA := MockSession{}
		sessionA.On("Close").Return()
		sessionA.On("ID").Return("sessionA")

		sessionB := MockSession{}
		sessionB.On("Close").Return()
		sessionB.On("ID").Return("sessionB")

		sessionStorage := NewSessionStorage()

		addErr := sessionStorage.AddRenderSession(&sessionA)
		assert.NoError(t, addErr)

		addErr = sessionStorage.AddRenderSession(&sessionB)
		assert.NoError(t, addErr)

		sessionStorage.CloseAllRenderSessions()

		sessionA.AssertExpectations(t)
		sessionB.AssertExpectations(t)
	})
}

func TestGetSessionByID(t *testing.T) {
	sessionStorage := NewSessionStorage()

	session := &MockSession{}
	session.On("ID").Return("asdf")
	session.On("Close").Return()
	require.NoError(t, sessionStorage.AddRenderSession(session))

	t.Run("returns error if wrong ID", func(t *testing.T) {
		sl, err := sessionStorage.GetSessionByID("a")
		assert.Nil(t, sl)
		assert.ErrorContains(t, err, "session with id 'a' does not exist")
	})

	t.Run("returns current session if correct ID", func(t *testing.T) {
		sl, err := sessionStorage.GetSessionByID("asdf")
		defer sl.Done()
		assert.Same(t, sl.S, session)
		assert.NoError(t, err)
	})
}

func TestGetAllSessionIds(t *testing.T) {
	t.Run("returns empty slice if no sessions in storage", func(t *testing.T) {
		sessionStorage := NewSessionStorage()

		ids := sessionStorage.GetAllSessionIds()
		assert.Empty(t, ids)
	})
	t.Run("returns a slice of all session ids in storage", func(t *testing.T) {
		ids := []string{"id1", "id6", "id13"}
		sessionStorage := NewSessionStorage()
		for _, id := range ids {
			sessionStorage.sessions[id] = &SessionAccess{}
		}

		responseIds := sessionStorage.GetAllSessionIds()
		assert.Equal(t, len(ids), len(responseIds))
		for _, id := range responseIds {
			assert.Contains(t, ids, id)
		}
	})
}

func TestGetSessionByID_RLocks(t *testing.T) {
	sessionStorage := NewSessionStorage()

	session := &MockSession{}
	session.On("ID").Return("rlockID")
	session.On("Close").Return()
	require.NoError(t, sessionStorage.AddRenderSession(session))

	const workers = 8
	started := sync.WaitGroup{}
	finished := sync.WaitGroup{}
	release := make(chan struct{})

	// Start multiple readers (RLock holders)
	started.Add(workers)
	finished.Add(workers)
	for range workers {
		go func() {
			access, err := sessionStorage.GetSessionByID("rlockID")
			require.NoError(t, err)
			started.Done()

			// Hold the RLock until further notice
			<-release
			access.Done()
			finished.Done()
		}()
	}
	started.Wait()

	raw := sessionStorage.getRawSessionByID("rlockID")
	lockAcquired := make(chan struct{})

	// Attempt to acquire a write lock while RLocks are held
	go func() {
		raw.m.Lock()
		lockAcquired <- struct{}{}
	}()

	select {
	case <-lockAcquired:
		t.Fatal("Write lock acquired while readers hold RLock")
	case <-time.After(1 * time.Second):
		t.Fatal("Write lock should have been acquired by now")
	default:
	}

	// Release the RLocks
	close(release)
	finished.Wait()

	<-lockAcquired // should unblock now
}

func TestCloseRenderSession_Waits(t *testing.T) {
	sessionStorage := NewSessionStorage()

	session := &MockSession{}
	session.On("ID").Return("blockID")
	session.On("Close").Return()

	require.NoError(t, sessionStorage.AddRenderSession(session))

	access, err := sessionStorage.GetSessionByID("blockID")
	require.NoError(t, err)
	require.NotNil(t, access)

	blocked := make(chan struct{})
	go func() {
		sessionStorage.CloseRenderSession("blockID")
		close(blocked)
	}()

	select {
	case <-blocked:
		t.Fatal("CloseRenderSession should block while RLock is held")
	default:
	}

	access.Done()

	<-blocked // should unblock now
}

func TestConcurrency_SingleSession(t *testing.T) {
	sessionStorage := NewSessionStorage()

	session := &MockSession{}
	session.On("ID").Return("sharedID")
	session.On("Close").Return()
	require.NoError(t, sessionStorage.AddRenderSession(session))

	const workerCount = 8
	started := sync.WaitGroup{}
	finished := sync.WaitGroup{}
	closed := sync.WaitGroup{}
	release := make(chan struct{})

	// Start multiple readers
	started.Add(workerCount)
	finished.Add(workerCount)

	for range workerCount {
		go func() {
			access, err := sessionStorage.GetSessionByID("sharedID")
			require.NoError(t, err)
			started.Done()

			// Hold the RLock until further notice
			<-release
			access.Done()
			finished.Done()
		}()
	}
	started.Wait()

	// Start multiple closers
	closed.Add(workerCount)
	for range workerCount {
		go func() {
			sessionStorage.CloseRenderSession("sharedID")
			closed.Done()
		}()
	}

	close(release)
	finished.Wait()
	closed.Wait()

	assert.Equal(t, 0, sessionStorage.SessionCount())
}

func TestConcurrency_MultipleSessions(t *testing.T) {
	const sessionCount = 4
	const workerCount = 8

	sessionStorage := NewSessionStorage()
	sessionIDs := make([]string, sessionCount)

	// Create and add sessions
	for i := range sessionCount {
		id := fmt.Sprintf("session-%d", i)
		session := &MockSession{}
		session.On("ID").Return(id)
		session.On("Close").Return()
		require.NoError(t, sessionStorage.AddRenderSession(session))
		sessionIDs[i] = id
	}

	started := sync.WaitGroup{}
	finished := sync.WaitGroup{}
	closed := sync.WaitGroup{}
	release := make(chan struct{})

	// Start multiple readers for each session
	started.Add(len(sessionIDs) * workerCount)
	finished.Add(len(sessionIDs) * workerCount)
	for _, id := range sessionIDs {
		for range workerCount {
			go func(sessionID string) {
				access, err := sessionStorage.GetSessionByID(sessionID)
				require.NoError(t, err)
				started.Done()

				// Hold the RLock until further notice
				<-release
				access.Done()
				finished.Done()
			}(id)
		}
	}
	started.Wait()

	// Start multiple closers for each session
	closed.Add(len(sessionIDs) * workerCount)
	for _, id := range sessionIDs {
		for range workerCount {
			go func(sessionID string) {
				sessionStorage.CloseRenderSession(sessionID)
				closed.Done()
			}(id)
		}
	}

	close(release)
	finished.Wait()
	closed.Wait()

	assert.Equal(t, 0, sessionStorage.SessionCount())
}

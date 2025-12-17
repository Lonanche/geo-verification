package verification

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Session struct {
	ID          string    `json:"id"`
	Username    string    `json:"username"`
	Code        string    `json:"-"`
	Verified    bool      `json:"verified"`
	ExpiresAt   time.Time `json:"expires_at"`
	CreatedAt   time.Time `json:"created_at"`
	CallbackURL string    `json:"-"` // Don't include in JSON responses
}

type SessionStore struct {
	sessions map[string]*Session
	mutex    sync.RWMutex
}

func NewSessionStore() *SessionStore {
	store := &SessionStore{
		sessions: make(map[string]*Session),
	}

	go store.cleanupExpired()
	return store
}

func (s *SessionStore) Create(username, callbackURL string, expiryDuration time.Duration) (*Session, error) {
	code, err := generateSecureCode(6)
	if err != nil {
		return nil, err
	}

	session := &Session{
		ID:          uuid.New().String(),
		Username:    username,
		Code:        code,
		Verified:    false,
		ExpiresAt:   time.Now().Add(expiryDuration),
		CreatedAt:   time.Now(),
		CallbackURL: callbackURL,
	}

	s.mutex.Lock()
	s.sessions[session.ID] = session
	s.mutex.Unlock()

	return session, nil
}

func (s *SessionStore) Get(sessionID string) (*Session, bool) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	session, exists := s.sessions[sessionID]
	if !exists || time.Now().After(session.ExpiresAt) {
		return nil, false
	}

	return session, true
}

func (s *SessionStore) Delete(sessionID string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	delete(s.sessions, sessionID)
}

func (s *SessionStore) cleanupExpired() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		s.mutex.Lock()
		now := time.Now()
		for id, session := range s.sessions {
			if now.After(session.ExpiresAt) {
				delete(s.sessions, id)
			}
		}
		s.mutex.Unlock()
	}
}

func generateSecureCode(length int) (string, error) {
	bytes := make([]byte, length/2)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}

	code := hex.EncodeToString(bytes)
	if len(code) > length {
		code = code[:length]
	}

	return code, nil
}

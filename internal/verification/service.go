package verification

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/lonanche/geo-verification/internal/geoguessr"
	"golang.org/x/time/rate"
)

type Service struct {
	sessionStore  *SessionStore
	geoClient     geoguessr.Client
	rateLimiters  map[string]*rate.Limiter
	rateMutex     sync.RWMutex
	rateLimitRate rate.Limit
	expiryTime    time.Duration
	httpClient    *http.Client
	friends       map[string]bool // Track accepted friends locally
	friendsMutex  sync.RWMutex
}

type CallbackPayload struct {
	SessionID string `json:"session_id"`
	UserID    string `json:"user_id"`
	Status    string `json:"status"` // "verified" or "expired"
	Timestamp string `json:"timestamp"`
}

func NewService(geoClient geoguessr.Client, rateLimitPerHour int, expiryMinutes time.Duration) *Service {
	rateLimitRate := rate.Limit(float64(rateLimitPerHour) / 3600.0)

	service := &Service{
		sessionStore:  NewSessionStore(),
		geoClient:     geoClient,
		rateLimiters:  make(map[string]*rate.Limiter),
		rateLimitRate: rateLimitRate,
		expiryTime:    expiryMinutes,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		friends: make(map[string]bool),
	}

	// Start background services
	go service.startFriendRequestAcceptanceService()
	go service.startChatMonitoringService()
	go service.startExpirationMonitoringService()

	return service
}

func (s *Service) StartVerification(userID, callbackURL string) (*Session, error) {
	if !s.checkRateLimit(userID) {
		return nil, fmt.Errorf("rate limit exceeded for user %s", userID)
	}

	// Validate callback URL if provided (localhost only by default)
	if callbackURL != "" {
		if err := s.validateCallbackURL(callbackURL); err != nil {
			return nil, fmt.Errorf("invalid callback URL: %w", err)
		}
	}

	// Check for existing active session and remove it
	if existingSession := s.getActiveSession(userID); existingSession != nil {
		log.Printf("Removing existing active session %s for user %s", existingSession.ID, userID)
		s.sessionStore.Delete(existingSession.ID)

		// Send expiration webhook for the old session if it has a callback URL
		if existingSession.CallbackURL != "" {
			go s.sendWebhook(existingSession, "expired")
		}
	}

	session, err := s.sessionStore.Create(userID, callbackURL, s.expiryTime)
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	// Check if user has added us as friend
	isFriend, err := s.geoClient.IsFriend(userID)
	if err != nil {
		log.Printf("Could not check friend status for %s: %v", userID, err)
		s.sessionStore.Delete(session.ID)
		return nil, fmt.Errorf("failed to check friend status: %w", err)
	}

	if !isFriend {
		// User is not friends yet - return session with code for them to send
		// Background service will auto-accept friend request when they send it
		log.Printf("User %s not friends yet, session created with code for them to send", userID)
		sessionResponse := &Session{
			ID:        session.ID,
			Username:  session.Username,
			Code:      session.Code, // Include code for user to send to bot
			Verified:  session.Verified,
			ExpiresAt: session.ExpiresAt,
			CreatedAt: session.CreatedAt,
		}
		return sessionResponse, nil
	}

	// If we're already friends, user can immediately start sending the code
	log.Printf("User %s is already friends, can start verification immediately", userID)

	sessionResponse := &Session{
		ID:        session.ID,
		Username:  session.Username,
		Code:      session.Code, // Include code for user to send to bot
		Verified:  session.Verified,
		ExpiresAt: session.ExpiresAt,
		CreatedAt: session.CreatedAt,
	}

	return sessionResponse, nil
}

func (s *Service) GetSessionStatus(sessionID string) (*Session, error) {
	session, exists := s.sessionStore.Get(sessionID)
	if !exists {
		return nil, fmt.Errorf("session not found or expired")
	}

	sessionResponse := &Session{
		ID:        session.ID,
		Username:  session.Username,
		Verified:  session.Verified,
		ExpiresAt: session.ExpiresAt,
		CreatedAt: session.CreatedAt,
	}

	return sessionResponse, nil
}

func (s *Service) startFriendRequestAcceptanceService() {
	log.Printf("Starting background friend request acceptance service")

	ticker := time.NewTicker(30 * time.Second) // Poll every 30 seconds
	defer ticker.Stop()

	for range ticker.C {
		s.processPendingFriendRequests()
	}
}

func (s *Service) processPendingFriendRequests() {
	// Get pending friend requests
	pendingRequests, err := s.geoClient.GetPendingFriendRequests()
	if err != nil {
		log.Printf("Error getting pending friend requests: %v", err)
		return
	}

	if len(pendingRequests) == 0 {
		return
	}

	log.Printf("Found %d pending friend requests", len(pendingRequests))

	// Check which users have active verification sessions
	for _, userID := range pendingRequests {
		if s.hasActiveSession(userID) {
			log.Printf("User %s has active verification session, accepting friend request", userID)
			if err := s.geoClient.AcceptFriendRequest(userID); err != nil {
				log.Printf("Error accepting friend request from %s: %v", userID, err)
			} else {
				log.Printf("Successfully accepted friend request from %s", userID)

				// Mark user as friend locally
				s.friendsMutex.Lock()
				s.friends[userID] = true
				s.friendsMutex.Unlock()

				// Friend request accepted - user can now send their verification code
				log.Printf("Friend request accepted for %s, user can now send verification code", userID)
			}
		} else {
			log.Printf("User %s has no active verification session, skipping friend request", userID)
		}
	}
}

func (s *Service) hasActiveSession(userID string) bool {
	s.sessionStore.mutex.RLock()
	defer s.sessionStore.mutex.RUnlock()

	for _, session := range s.sessionStore.sessions {
		if session.Username == userID && time.Now().Before(session.ExpiresAt) {
			return true
		}
	}
	return false
}

func (s *Service) getActiveSession(userID string) *Session {
	s.sessionStore.mutex.RLock()
	defer s.sessionStore.mutex.RUnlock()

	for _, session := range s.sessionStore.sessions {
		if session.Username == userID && time.Now().Before(session.ExpiresAt) {
			return session
		}
	}
	return nil
}

func (s *Service) startChatMonitoringService() {
	log.Printf("Starting background chat monitoring service")

	ticker := time.NewTicker(30 * time.Second) // Poll every 30 seconds
	defer ticker.Stop()

	for range ticker.C {
		s.monitorChatMessages()
	}
}

func (s *Service) monitorChatMessages() {
	// Get all active sessions
	activeSessions := s.getActiveSessions()

	for _, session := range activeSessions {
		if session.Verified {
			continue // Skip already verified sessions
		}

		// Check if user is a friend locally before trying to read chat messages
		if !s.isLocalFriend(session.Username) {
			// User is not a friend yet, skip reading chat messages
			continue
		}

		// Read chat messages from this user
		messages, err := s.geoClient.ReadChatMessages(session.Username)
		if err != nil {
			// Check if it's a 404 error (user might not be friend anymore)
			if strings.Contains(err.Error(), "404") {
				log.Printf("Got 404 reading chat from %s, checking actual friend status via API", session.Username)

				// Check API to see if user is still a friend
				isFriend, apiErr := s.geoClient.IsFriend(session.Username)
				if apiErr != nil {
					log.Printf("Error checking friend status for %s: %v", session.Username, apiErr)
				} else {
					// Update local friend status based on API response
					s.friendsMutex.Lock()
					s.friends[session.Username] = isFriend
					s.friendsMutex.Unlock()

					if !isFriend {
						log.Printf("User %s is no longer a friend, updated local status", session.Username)
					} else {
						log.Printf("User %s is still a friend according to API, but chat read failed", session.Username)
					}
				}
			} else {
				log.Printf("Error reading chat messages from %s: %v", session.Username, err)
			}
			continue
		}

		// Check if any message contains the verification code
		for _, message := range messages {
			// Only check messages from the user to us (not our messages to them)
			if message.SourceID == session.Username && message.TextPayload == session.Code {
				log.Printf("Verification code received from %s: %s", session.Username, session.Code)

				// Mark session as verified
				session.Verified = true
				log.Printf("User %s verified successfully!", session.Username)

				// Send webhook notification
				if session.CallbackURL != "" {
					go s.sendWebhook(session, "verified")
				}
				break
			}
		}
	}
}

func (s *Service) getActiveSessions() []*Session {
	s.sessionStore.mutex.RLock()
	defer s.sessionStore.mutex.RUnlock()

	var activeSessions []*Session
	for _, session := range s.sessionStore.sessions {
		if time.Now().Before(session.ExpiresAt) {
			activeSessions = append(activeSessions, session)
		}
	}
	return activeSessions
}

func (s *Service) isLocalFriend(userID string) bool {
	s.friendsMutex.RLock()
	defer s.friendsMutex.RUnlock()
	return s.friends[userID]
}

func (s *Service) sendWebhook(session *Session, status string) {
	if session.CallbackURL == "" {
		return
	}

	payload := CallbackPayload{
		SessionID: session.ID,
		UserID:    session.Username,
		Status:    status,
		Timestamp: time.Now().Format(time.RFC3339),
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Error marshaling webhook payload for session %s: %v", session.ID, err)
		return
	}

	req, err := http.NewRequest("POST", session.CallbackURL, bytes.NewBuffer(jsonPayload))
	if err != nil {
		log.Printf("Error creating webhook request for session %s: %v", session.ID, err)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "GeoVerification/1.0")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		log.Printf("Error sending webhook for session %s to %s: %v", session.ID, session.CallbackURL, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		log.Printf("Webhook sent successfully for session %s (%s)", session.ID, status)
	} else {
		log.Printf("Webhook failed for session %s: HTTP %d", session.ID, resp.StatusCode)
	}
}

func (s *Service) startExpirationMonitoringService() {
	log.Printf("Starting background session expiration monitoring service")

	ticker := time.NewTicker(30 * time.Second) // Check every 30 seconds
	defer ticker.Stop()

	for range ticker.C {
		s.monitorExpiredSessions()
	}
}

func (s *Service) monitorExpiredSessions() {
	s.sessionStore.mutex.RLock()
	now := time.Now()
	var expiredSessions []*Session

	for _, session := range s.sessionStore.sessions {
		if now.After(session.ExpiresAt) && !session.Verified {
			expiredSessions = append(expiredSessions, session)
		}
	}
	s.sessionStore.mutex.RUnlock()

	// Send webhook notifications for expired sessions
	for _, session := range expiredSessions {
		if session.CallbackURL != "" {
			log.Printf("Sending expiration webhook for session %s", session.ID)
			go s.sendWebhook(session, "expired")
		}
	}
}

func (s *Service) validateCallbackURL(callbackURL string) error {
	parsedURL, err := url.Parse(callbackURL)
	if err != nil {
		return fmt.Errorf("invalid URL format")
	}

	// Only allow HTTP/HTTPS
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return fmt.Errorf("only HTTP/HTTPS allowed")
	}

	host := parsedURL.Hostname()
	if host == "" {
		return fmt.Errorf("hostname required")
	}

	// Allow localhost for development/testing
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return nil
	}

	// For production use, comment out this check to allow external URLs
	// WARNING: This may expose internal services to SSRF attacks
	return fmt.Errorf("only localhost allowed (modify validateCallbackURL() for production)")
}

func (s *Service) checkRateLimit(username string) bool {
	s.rateMutex.Lock()
	defer s.rateMutex.Unlock()

	limiter, exists := s.rateLimiters[username]
	if !exists {
		limiter = rate.NewLimiter(s.rateLimitRate, 3)
		s.rateLimiters[username] = limiter
	}

	return limiter.Allow()
}

package verification

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/lonanche/geo-verification/internal/geoguessr"
	"golang.org/x/time/rate"
)

type Logger interface {
	Printf(format string, v ...interface{})
}

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
	allowedHosts  map[string]bool // Allowed callback hosts
	logger        Logger
}

type CallbackPayload struct {
	SessionID string `json:"session_id"`
	UserID    string `json:"user_id"`
	Status    string `json:"status"` // "verified" or "expired"
	Timestamp string `json:"timestamp"`
}

func NewService(geoClient geoguessr.Client, rateLimitPerHour int, expiryMinutes time.Duration, allowedCallbackHosts string, logger Logger) *Service {
	rateLimitRate := rate.Limit(float64(rateLimitPerHour) / 3600.0)

	// Parse allowed hosts from comma-separated string
	allowedHostsMap := make(map[string]bool)
	if allowedCallbackHosts != "" {
		hosts := strings.Split(allowedCallbackHosts, ",")
		for _, host := range hosts {
			allowedHostsMap[strings.TrimSpace(host)] = true
		}
	}

	service := &Service{
		sessionStore:  NewSessionStore(),
		geoClient:     geoClient,
		rateLimiters:  make(map[string]*rate.Limiter),
		rateLimitRate: rateLimitRate,
		expiryTime:    expiryMinutes,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		friends:      make(map[string]bool),
		allowedHosts: allowedHostsMap,
		logger:       logger,
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
		s.logger.Printf("Removing existing active session %s for user %s", existingSession.ID, userID)
		s.sessionStore.Delete(existingSession.ID)

		// Clean up local friend status for the old session
		s.friendsMutex.Lock()
		delete(s.friends, userID)
		s.friendsMutex.Unlock()

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
		s.logger.Printf("Could not check friend status for %s: %v", userID, err)
		s.sessionStore.Delete(session.ID)
		return nil, fmt.Errorf("failed to check friend status: %w", err)
	}

	if !isFriend {
		// User is not friends yet - return session with code for them to send
		// Background service will auto-accept friend request when they send it
		s.logger.Printf("User %s not friends yet, session created with code for them to send", userID)
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
	s.logger.Printf("User %s is already friends, can start verification immediately", userID)

	// Mark user as friend locally since they're already a friend
	s.friendsMutex.Lock()
	s.friends[userID] = true
	s.friendsMutex.Unlock()

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
	s.logger.Printf("Starting background friend request acceptance service")

	ticker := time.NewTicker(30 * time.Second) // Poll every 30 seconds
	defer ticker.Stop()

	for range ticker.C {
		s.processPendingFriendRequests()
	}
}

func (s *Service) processPendingFriendRequests() {
	// Check if there are any active sessions first
	activeSessions := s.getActiveSessions()
	if len(activeSessions) == 0 {
		// No active verification sessions, skip checking friend requests
		return
	}

	// Get pending friend requests
	pendingRequests, err := s.geoClient.GetPendingFriendRequests()
	if err != nil {
		s.logger.Printf("Error getting pending friend requests: %v", err)
		return
	}

	if len(pendingRequests) == 0 {
		return
	}

	s.logger.Printf("Found %d pending friend requests", len(pendingRequests))

	// Check which users have active verification sessions
	for _, userID := range pendingRequests {
		if s.hasActiveSession(userID) {
			s.logger.Printf("User %s has active verification session, accepting friend request", userID)
			if err := s.geoClient.AcceptFriendRequest(userID); err != nil {
				s.logger.Printf("Error accepting friend request from %s: %v", userID, err)
			} else {
				s.logger.Printf("Successfully accepted friend request from %s", userID)

				// Mark user as friend locally
				s.friendsMutex.Lock()
				s.friends[userID] = true
				s.friendsMutex.Unlock()

				// Friend request accepted - user can now send their verification code
				s.logger.Printf("Friend request accepted for %s, user can now send verification code", userID)
			}
		} else {
			s.logger.Printf("User %s has no active verification session, skipping friend request", userID)
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
	s.logger.Printf("Starting background chat monitoring service")

	ticker := time.NewTicker(30 * time.Second) // Poll every 30 seconds
	defer ticker.Stop()

	for range ticker.C {
		s.monitorChatMessages()
	}
}

func (s *Service) monitorChatMessages() {
	// Get all active sessions
	activeSessions := s.getActiveSessions()

	// If no active sessions, skip chat monitoring
	if len(activeSessions) == 0 {
		return
	}

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
				s.logger.Printf("Got 404 reading chat from %s, checking actual friend status via API", session.Username)

				// Check API to see if user is still a friend
				isFriend, apiErr := s.geoClient.IsFriend(session.Username)
				if apiErr != nil {
					s.logger.Printf("Error checking friend status for %s: %v", session.Username, apiErr)
				} else {
					// Update local friend status based on API response
					s.friendsMutex.Lock()
					s.friends[session.Username] = isFriend
					s.friendsMutex.Unlock()

					if !isFriend {
						s.logger.Printf("User %s is no longer a friend, updated local status", session.Username)
					} else {
						s.logger.Printf("User %s is still a friend according to API, but chat read failed", session.Username)
					}
				}
			} else {
				s.logger.Printf("Error reading chat messages from %s: %v", session.Username, err)
			}
			continue
		}

		// Check if any message contains the verification code
		for _, message := range messages {
			// Only check messages from the user to us (not our messages to them)
			if message.SourceID == session.Username && message.TextPayload == session.Code {
				s.logger.Printf("Verification code received from %s: %s", session.Username, session.Code)

				// Mark session as verified
				session.Verified = true
				s.logger.Printf("User %s verified successfully!", session.Username)

				// Clean up local friend status after successful verification
				s.friendsMutex.Lock()
				delete(s.friends, session.Username)
				s.friendsMutex.Unlock()

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
		s.logger.Printf("Error marshaling webhook payload for session %s: %v", session.ID, err)
		return
	}

	req, err := http.NewRequest("POST", session.CallbackURL, bytes.NewBuffer(jsonPayload))
	if err != nil {
		s.logger.Printf("Error creating webhook request for session %s: %v", session.ID, err)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "GeoVerification/1.0")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		s.logger.Printf("Error sending webhook for session %s to %s: %v", session.ID, session.CallbackURL, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		s.logger.Printf("Webhook sent successfully for session %s (%s)", session.ID, status)
	} else {
		s.logger.Printf("Webhook failed for session %s: HTTP %d", session.ID, resp.StatusCode)
	}
}

func (s *Service) startExpirationMonitoringService() {
	s.logger.Printf("Starting background session expiration monitoring service")

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

	// Send webhook notifications for expired sessions and cleanup local friends
	for _, session := range expiredSessions {
		if session.CallbackURL != "" {
			s.logger.Printf("Sending expiration webhook for session %s", session.ID)
			go s.sendWebhook(session, "expired")
		}

		// Clean up local friend status for expired sessions
		s.friendsMutex.Lock()
		delete(s.friends, session.Username)
		s.friendsMutex.Unlock()
		s.logger.Printf("Cleaned up local friend status for expired session user %s", session.Username)
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

	// Check if host is in allowed list
	if s.allowedHosts[host] {
		return nil
	}

	return fmt.Errorf("callback host '%s' not allowed. Configure ALLOWED_CALLBACK_HOSTS environment variable", host)
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

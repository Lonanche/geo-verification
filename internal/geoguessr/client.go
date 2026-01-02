package geoguessr

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Logger interface {
	Printf(format string, v ...interface{})
}

type Client interface {
	Login() error
	IsFriend(userID string) (bool, error)
	GetPendingFriendRequests() ([]string, error)
	AcceptFriendRequest(userID string) error
	ReadChatMessages(userID string) ([]ChatMessage, error)
	IsLoggedIn() bool
}

type HTTPClient struct {
	httpClient *http.Client
	baseURL    string
	ncfaToken  string
	logger     Logger
}

func NewClient(ncfaToken string, logger Logger) *HTTPClient {
	return &HTTPClient{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		baseURL:   "https://www.geoguessr.com/api/v3",
		ncfaToken: ncfaToken,
		logger:    logger,
	}
}

func (c *HTTPClient) Login() error {
	// No login needed when using NCFA token
	c.logger.Printf("Using NCFA token authentication")
	return nil
}

func (c *HTTPClient) IsFriend(userID string) (bool, error) {
	c.logger.Printf("Checking if user %s is a friend", userID)

	const pageSize = 50
	page := 0

	for {
		url := fmt.Sprintf("%s/social/friends?count=%d&page=%d", c.baseURL, pageSize, page)
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return false, fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Accept", "*/*")
		req.Header.Set("Origin", "https://www.geoguessr.com")
		req.Header.Set("Referer", "https://www.geoguessr.com/")
		req.Header.Set("x-client", "web")
		req.Header.Set("Cookie", fmt.Sprintf("_ncfa=%s", c.ncfaToken))

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return false, fmt.Errorf("failed to get friends list: %w", err)
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		c.logger.Printf("Friends list response (page %d): %d - %s", page, resp.StatusCode, string(body))

		if resp.StatusCode != http.StatusOK {
			return false, fmt.Errorf("failed to get friends list with status %d: %s", resp.StatusCode, string(body))
		}

		var friends []Friend
		if err := json.Unmarshal(body, &friends); err != nil {
			return false, fmt.Errorf("failed to parse friends list: %w", err)
		}

		for _, friend := range friends {
			if friend.UserID == userID {
				return true, nil
			}
		}

		if len(friends) < pageSize {
			break
		}
		time.Sleep(time.Second)
		page++
	}

	return false, nil
}

func (c *HTTPClient) GetPendingFriendRequests() ([]string, error) {
	c.logger.Printf("Getting pending friend requests")

	url := fmt.Sprintf("%s/social/friends/received", c.baseURL)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Origin", "https://www.geoguessr.com")
	req.Header.Set("Referer", "https://www.geoguessr.com/")
	req.Header.Set("x-client", "web")
	req.Header.Set("Cookie", fmt.Sprintf("_ncfa=%s", c.ncfaToken))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get pending friend requests: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	c.logger.Printf("Pending friend requests response: %d - %s", resp.StatusCode, string(body))

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get pending friend requests with status %d: %s", resp.StatusCode, string(body))
	}

	// Parse JSON response properly
	var pendingRequests []PendingFriendRequest
	if err := json.Unmarshal(body, &pendingRequests); err != nil {
		return nil, fmt.Errorf("failed to parse pending friend requests: %w", err)
	}

	var userIDs []string
	for _, request := range pendingRequests {
		userIDs = append(userIDs, request.UserID)
	}

	c.logger.Printf("Found %d pending friend requests: %v", len(userIDs), userIDs)
	return userIDs, nil
}

func (c *HTTPClient) AcceptFriendRequest(userID string) error {
	c.logger.Printf("Accepting friend request from user: %s", userID)

	// Correct endpoint for accepting friend requests
	url := fmt.Sprintf("%s/social/friends/%s?context=", c.baseURL, userID)

	req, err := http.NewRequest("PUT", url, bytes.NewBuffer([]byte("{}")))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Origin", "https://www.geoguessr.com")
	req.Header.Set("Referer", "https://www.geoguessr.com/")
	req.Header.Set("x-client", "web")
	req.Header.Set("Cookie", fmt.Sprintf("_ncfa=%s", c.ncfaToken))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to accept friend request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	c.logger.Printf("Accept friend request response: %d - %s", resp.StatusCode, string(body))

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("failed to accept friend request with status %d: %s", resp.StatusCode, string(body))
	}

	c.logger.Printf("Successfully accepted friend request from %s", userID)
	return nil
}

func (c *HTTPClient) ReadChatMessages(userID string) ([]ChatMessage, error) {
	c.logger.Printf("Reading chat messages from user: %s", userID)

	// Use v4 chat API
	url := fmt.Sprintf("https://www.geoguessr.com/api/v4/chat/%s", userID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://www.geoguessr.com")
	req.Header.Set("Referer", "https://www.geoguessr.com/")
	req.Header.Set("x-client", "web")
	req.Header.Set("Cookie", fmt.Sprintf("_ncfa=%s", c.ncfaToken))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to read chat messages: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	c.logger.Printf("Chat messages response: %d - %s", resp.StatusCode, string(body))

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to read chat messages with status %d: %s", resp.StatusCode, string(body))
	}

	// Parse JSON response
	var chatResponse ChatResponse
	if err := json.Unmarshal(body, &chatResponse); err != nil {
		return nil, fmt.Errorf("failed to parse chat messages: %w", err)
	}

	return chatResponse.Messages, nil
}

func (c *HTTPClient) IsLoggedIn() bool {
	// Always logged in with NCFA token
	return c.ncfaToken != ""
}

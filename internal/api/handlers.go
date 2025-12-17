package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/lonanche/geo-verification/internal/verification"
)

type Handler struct {
	verificationService *verification.Service
}

func NewHandler(verificationService *verification.Service) *Handler {
	return &Handler{
		verificationService: verificationService,
	}
}

type StartVerificationRequest struct {
	UserID      string `json:"user_id" binding:"required"`
	CallbackURL string `json:"callback_url,omitempty"`
}

type StartVerificationResponse struct {
	SessionID        string `json:"session_id"`
	VerificationCode string `json:"verification_code"`
	ExpiresAt        string `json:"expires_at"`
	Message          string `json:"message"`
}


type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}

func (h *Handler) StartVerification(c *gin.Context) {
	var req StartVerificationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "invalid_request",
			Message: err.Error(),
		})
		return
	}

	session, err := h.verificationService.StartVerification(req.UserID, req.CallbackURL)
	if err != nil {
		statusCode := http.StatusInternalServerError
		errorType := "internal_error"

		if err.Error() == "rate limit exceeded for user "+req.UserID {
			statusCode = http.StatusTooManyRequests
			errorType = "rate_limit_exceeded"
		} else if err.Error() == "user must add the bot account as a friend first before verification can proceed" {
			statusCode = http.StatusBadRequest
			errorType = "friend_required"
		}

		c.JSON(statusCode, ErrorResponse{
			Error:   errorType,
			Message: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, StartVerificationResponse{
		SessionID:        session.ID,
		VerificationCode: session.Code,
		ExpiresAt:        session.ExpiresAt.Format("2006-01-02T15:04:05Z07:00"),
		Message:          "Send this verification code to the bot via GeoGuessr messages after adding as friend.",
	})
}


func (h *Handler) GetVerificationStatus(c *gin.Context) {
	sessionID := c.Param("session_id")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "invalid_request",
			Message: "session_id is required",
		})
		return
	}

	session, err := h.verificationService.GetSessionStatus(sessionID)
	if err != nil {
		c.JSON(http.StatusNotFound, ErrorResponse{
			Error:   "session_not_found",
			Message: "Session not found or expired",
		})
		return
	}

	response := map[string]interface{}{
		"session_id": session.ID,
		"username":   session.Username,
		"verified":   session.Verified,
		"expires_at": session.ExpiresAt.Format("2006-01-02T15:04:05Z07:00"),
		"created_at": session.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}

	c.JSON(http.StatusOK, response)
}


func (h *Handler) HealthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":    "healthy",
		"service":   "geo-verification",
		"timestamp": "2024-12-16T12:00:00Z",
	})
}
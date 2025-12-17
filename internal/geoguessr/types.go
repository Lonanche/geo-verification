package geoguessr


type Friend struct {
	UserID string `json:"userId"`
	Nick   string `json:"nick"`
	URL    string `json:"url"`
}

type PendingFriendRequest struct {
	UserID string `json:"userId"`
	Nick   string `json:"nick"`
	URL    string `json:"url"`
}

type ChatMessage struct {
	ID           string `json:"id"`
	PayloadType  string `json:"payloadType"`
	TextPayload  string `json:"textPayload"`
	SourceType   string `json:"sourceType"`
	SourceID     string `json:"sourceId"`
	RecipientID  string `json:"recipientId"`
	SentAt       string `json:"sentAt"`
	RoomID       string `json:"roomId"`
}

type ChatResponse struct {
	RoomID   string        `json:"roomId"`
	Messages []ChatMessage `json:"messages"`
}
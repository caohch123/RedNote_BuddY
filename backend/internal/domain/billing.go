package domain

type UsageRecord struct {
	UserID          string      `json:"user_id,omitempty"`
	Username        string      `json:"username,omitempty"`
	ID              string      `json:"id"`
	CreatedAt       string      `json:"created_at"`
	Capability      string      `json:"capability"`
	Operation       string      `json:"operation"`
	JobID           string      `json:"job_id,omitempty"`
	EntityID        string      `json:"entity_id,omitempty"`
	Provider        string      `json:"provider"`
	Model           string      `json:"model"`
	CredentialID    string      `json:"credential_id"`
	Status          string      `json:"status"`
	InputTokens     *int64      `json:"input_tokens"`
	OutputTokens    *int64      `json:"output_tokens"`
	TotalTokens     *int64      `json:"total_tokens"`
	CachedTokens    *int64      `json:"cached_tokens"`
	ReasoningTokens *int64      `json:"reasoning_tokens"`
	ImageCount      *int64      `json:"image_count,omitempty"`
	VideoSeconds    *float64    `json:"video_seconds,omitempty"`
	Price           *TokenPrice `json:"price,omitempty"`
	EstimatedCNY    *float64    `json:"estimated_cny"`
}

type TokenPrice struct {
	Provider         string   `json:"provider"`
	Model            string   `json:"model"`
	InputPerMillion  float64  `json:"input_per_million"`
	OutputPerMillion float64  `json:"output_per_million"`
	CachedPerMillion *float64 `json:"cached_per_million,omitempty"`
}

type UsageFilter struct {
	From, To          int64
	Model, Capability string
}

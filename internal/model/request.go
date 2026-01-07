package model

import (
	"time"
)

// Request represents an HTTP request
type Request struct {
	ID        string            `json:"id"`
	Timestamp time.Time         `json:"timestamp"`
	Method    string            `json:"method"`
	URL       string            `json:"url"`
	Headers   map[string]string `json:"headers"`
	Body      string            `json:"body"`
	Response  *Response         `json:"response,omitempty"`
}

// Response represents an HTTP response
type Response struct {
	StatusCode int               `json:"status_code"`
	Status     string            `json:"status"`
	Headers    map[string]string `json:"headers"`
	Body       string            `json:"body"`
	DurationMs int64             `json:"duration_ms"`
}

// SavedRequest represents a request saved in a collection (without response)
type SavedRequest struct {
	Name    string            `json:"name"`
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

// Collection represents a group of saved requests
type Collection struct {
	Name     string         `json:"name"`
	Requests []SavedRequest `json:"requests"`
}

// History represents the request history storage
type History struct {
	Requests []Request `json:"requests"`
}

// Collections represents all collections storage
type Collections struct {
	Collections map[string]Collection `json:"collections"`
}

// Aliases represents all URL aliases storage
type Aliases struct {
	Aliases map[string]string `json:"aliases"` // name -> base URL
}

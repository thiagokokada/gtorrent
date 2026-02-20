package domain

import "time"

// Torrent represents a normalized torrent view returned to the UI.
type Torrent struct {
	Hash      string    `json:"hash"`
	Name      string    `json:"name"`
	SizeBytes int64     `json:"sizeBytes"`
	DoneBytes int64     `json:"doneBytes"`
	Progress  float64   `json:"progress"`
	State     string    `json:"state"`
	DownRate  int64     `json:"downRate"`
	UpRate    int64     `json:"upRate"`
	AddedAt   time.Time `json:"addedAt,omitempty"`
	Message   string    `json:"message,omitempty"`
}

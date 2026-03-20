package entities

import (
	"encoding/json"
	"time"
)

type Event struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Description    string    `json:"description"`
	DateTime       time.Time `json:"dateTime"`
	Venue          Venue     `json:"venue"`
	Performer      Performer `json:"performer"`
	Capacity       int       `json:"capacity"`
	AvailableCount int       `json:"availableCount"`
	Tickets        []Ticket  `json:"tickets,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
}

type Venue struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Address   string          `json:"address"`
	Capacity  int             `json:"capacity"`
	SeatMap   *json.RawMessage `json:"seatMap,omitempty"`
	CreatedAt time.Time       `json:"createdAt"`
}

type Performer struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"createdAt"`
}

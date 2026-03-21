package entities

import (
	"encoding/json"
	"time"
)

type Event struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Description     string    `json:"description"`
	DateTime        time.Time `json:"dateTime"`
	Venue Venue `json:"venue"`
	// AvailableCount is computed when listing events (venue capacity minus active bookings); it is not a DB column.
	AvailableCount int `json:"availableCount"`
	CreatedAt       time.Time `json:"createdAt"`
}

type Venue struct {
	ID        string           `json:"id"`
	Name      string           `json:"name"`
	Address   string           `json:"address"`
	Capacity  int              `json:"capacity"`
	SeatMap   *json.RawMessage `json:"seatMap,omitempty"`
	CreatedAt time.Time        `json:"createdAt"`
}






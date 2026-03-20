package entities

import "time"

type TicketStatus string

const (
	TicketStatusAvailable TicketStatus = "AVAILABLE"
	TicketStatusBooked    TicketStatus = "BOOKED"
)

type Ticket struct {
	ID         string       `json:"id"`
	EventID    string       `json:"eventId"`
	SeatNumber string       `json:"seatNumber"`
	Row        string       `json:"row"`
	Section    string       `json:"section"`
	Price      float64      `json:"price"`
	Status     TicketStatus `json:"status"`
	BookingID  *string      `json:"bookingId,omitempty"`
	CreatedAt  time.Time    `json:"createdAt"`
}

package entities

import "time"

type BookingStatus string

const (
	BookingStatusReserved  BookingStatus = "RESERVED"
	BookingStatusConfirmed BookingStatus = "CONFIRMED"
	BookingStatusCancelled BookingStatus = "CANCELLED"
)

type Booking struct {
	ID         string        `json:"id"`
	UserID     string        `json:"userId"`
	EventID    string        `json:"eventId"`
	TicketIDs  []string      `json:"ticketIds"`
	TotalPrice float64       `json:"totalPrice"`
	Status     BookingStatus `json:"status"`
	ExpiresAt  *time.Time    `json:"expiresAt,omitempty"` // set for RESERVED bookings only
	CreatedAt  time.Time     `json:"createdAt"`
	UpdatedAt  time.Time     `json:"updatedAt"`
}

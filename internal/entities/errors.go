package entities

import "errors"

var (
	ErrNotFound             = errors.New("not found")
	ErrInsufficientCapacity = errors.New("insufficient capacity")
	ErrInvalidQuantity      = errors.New("invalid quantity")
	ErrTicketUnavailable    = errors.New("ticket unavailable")
	ErrUnauthorized         = errors.New("unauthorized")
	ErrConflict             = errors.New("conflict")
)

package entities

import "errors"

var (
	ErrNotFound          = errors.New("not found")
	ErrTicketUnavailable = errors.New("ticket unavailable")
	ErrUnauthorized      = errors.New("unauthorized")
	ErrConflict          = errors.New("conflict")
)

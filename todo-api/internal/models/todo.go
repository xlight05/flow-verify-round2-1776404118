package models

import "time"

type Todo struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Done        bool      `json:"done"`
	CreatedAt   time.Time `json:"created_at"`
}

type TodoCreate struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

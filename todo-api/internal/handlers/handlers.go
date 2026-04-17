package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/flow-verify-round2/todo-api/internal/middleware"
	"github.com/flow-verify-round2/todo-api/internal/models"
	"github.com/flow-verify-round2/todo-api/internal/storage"
)

const (
	contentTypeJSON = "application/json; charset=utf-8"
	maxBodyBytes    = 1 << 20
)

type Handler struct {
	store *storage.Store
}

func New(store *storage.Store) *Handler {
	return &Handler{store: store}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/health", h.health)
	mux.HandleFunc("/todos", h.todosCollection)
	mux.HandleFunc("/todos/", h.todosItem)
}

func writeJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", contentTypeJSON)
	w.WriteHeader(status)
	if body == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Printf("failed to encode response: %v", err)
	}
}

func writeAPIError(w http.ResponseWriter, status int, code, message, field string) {
	writeJSON(w, status, models.APIError{Code: code, Message: message, Field: field})
}

func writeInternalError(w http.ResponseWriter, err error) {
	log.Printf("internal error: %v", err)
	w.Header().Set("Content-Type", contentTypeJSON)
	w.WriteHeader(http.StatusInternalServerError)
	_, _ = w.Write([]byte(`{"code":"internal_error","message":"internal server error"}`))
}

func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) todosCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listTodos(w, r)
	case http.MethodPost:
		h.createTodo(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", "")
	}
}

func (h *Handler) todosItem(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/todos/"), "/")
	if id == "" || strings.Contains(id, "/") {
		writeAPIError(w, http.StatusNotFound, "not_found", "todo not found", "")
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.getTodo(w, r, id)
	case http.MethodPatch:
		h.patchTodo(w, r, id)
	default:
		w.Header().Set("Allow", "GET, PATCH")
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", "")
	}
}

func (h *Handler) listTodos(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		writeAPIError(w, http.StatusUnauthorized, "unauthenticated", "authentication required", "")
		return
	}
	writeJSON(w, http.StatusOK, h.store.List(user))
}

func (h *Handler) createTodo(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		writeAPIError(w, http.StatusUnauthorized, "unauthenticated", "authentication required", "")
		return
	}

	if !hasJSONContentType(r) {
		writeAPIError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json", "")
		return
	}

	var payload models.TodoCreate
	if err := decodeStrict(r, &payload); err != nil {
		writeDecodeError(w, err)
		return
	}

	if payload.Title == nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_type", "title is required", "title")
		return
	}
	title := strings.TrimSpace(*payload.Title)
	if title == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_value", "title must not be empty", "title")
		return
	}

	todo, err := h.store.Create(title, user)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	log.Printf("event=todo_created todo_id=%s actor=%s", todo.ID, user)
	writeJSON(w, http.StatusCreated, todo)
}

func (h *Handler) getTodo(w http.ResponseWriter, r *http.Request, id string) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		writeAPIError(w, http.StatusUnauthorized, "unauthenticated", "authentication required", "")
		return
	}
	todo, err := h.store.Get(id, user)
	if err != nil {
		switch {
		case errors.Is(err, storage.ErrNotFound):
			writeAPIError(w, http.StatusNotFound, "not_found", "todo not found", "")
		case errors.Is(err, storage.ErrForbidden):
			writeAPIError(w, http.StatusForbidden, "forbidden", "you do not have access to this todo", "")
		default:
			writeInternalError(w, err)
		}
		return
	}
	writeJSON(w, http.StatusOK, todo)
}

func (h *Handler) patchTodo(w http.ResponseWriter, r *http.Request, id string) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		writeAPIError(w, http.StatusUnauthorized, "unauthenticated", "authentication required", "")
		return
	}

	if !hasJSONContentType(r) {
		log.Printf("event=todo_patch_failed todo_id=%s actor=%s reason=unsupported_media_type", id, user)
		writeAPIError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json", "")
		return
	}

	// Accept an optional "completed" field as *bool so we can distinguish
	// missing from false. DisallowUnknownFields rejects anything else.
	var payload struct {
		Completed *bool `json:"completed"`
	}
	if err := decodeStrict(r, &payload); err != nil {
		log.Printf("event=todo_patch_failed todo_id=%s actor=%s reason=%s", id, user, err.Error())
		writeDecodeError(w, err)
		return
	}
	if payload.Completed == nil {
		log.Printf("event=todo_patch_failed todo_id=%s actor=%s reason=missing_completed", id, user)
		writeAPIError(w, http.StatusBadRequest, "invalid_type", "completed is required and must be a boolean", "completed")
		return
	}

	updated, err := h.store.SetCompleted(id, user, *payload.Completed)
	if err != nil {
		switch {
		case errors.Is(err, storage.ErrNotFound):
			log.Printf("event=todo_patch_failed todo_id=%s actor=%s reason=not_found", id, user)
			writeAPIError(w, http.StatusNotFound, "not_found", "todo not found", "")
		case errors.Is(err, storage.ErrForbidden):
			log.Printf("event=todo_patch_failed todo_id=%s actor=%s reason=forbidden", id, user)
			writeAPIError(w, http.StatusForbidden, "forbidden", "you do not have access to this todo", "")
		default:
			writeInternalError(w, err)
		}
		return
	}

	log.Printf("event=todo_patched todo_id=%s actor=%s completed=%t", updated.ID, user, updated.Completed)
	writeJSON(w, http.StatusOK, updated)
}

func hasJSONContentType(r *http.Request) bool {
	ct := r.Header.Get("Content-Type")
	if ct == "" {
		return false
	}
	// Strip parameters like "; charset=utf-8".
	if i := strings.Index(ct, ";"); i >= 0 {
		ct = ct[:i]
	}
	return strings.EqualFold(strings.TrimSpace(ct), "application/json")
}

// decodeErrKind classifies body-decoding failures so handlers can map them to
// the right status/code pair.
type decodeErrKind int

const (
	decodeErrMalformed decodeErrKind = iota
	decodeErrUnknownField
	decodeErrInvalidType
	decodeErrEmpty
	decodeErrTrailingData
	decodeErrTooLarge
)

type decodeError struct {
	kind  decodeErrKind
	field string
	msg   string
}

func (e *decodeError) Error() string { return e.msg }

func decodeStrict(r *http.Request, v interface{}) error {
	if r.Body == nil {
		return &decodeError{kind: decodeErrEmpty, msg: "request body is required"}
	}
	body := http.MaxBytesReader(nil, r.Body, maxBodyBytes)
	defer body.Close()

	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return classifyDecodeError(err)
	}
	// Reject extra data after the first JSON value.
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return &decodeError{kind: decodeErrTrailingData, msg: "request body must contain a single JSON object"}
		}
		return classifyDecodeError(err)
	}
	return nil
}

func classifyDecodeError(err error) error {
	if err == io.EOF {
		return &decodeError{kind: decodeErrEmpty, msg: "request body is required"}
	}
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		return &decodeError{kind: decodeErrTooLarge, msg: "request body too large"}
	}
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		return &decodeError{kind: decodeErrInvalidType, field: typeErr.Field, msg: "invalid type for field"}
	}
	msg := err.Error()
	const unknown = "json: unknown field "
	if strings.HasPrefix(msg, unknown) {
		field := strings.Trim(strings.TrimPrefix(msg, unknown), `"`)
		return &decodeError{kind: decodeErrUnknownField, field: field, msg: "unknown field"}
	}
	return &decodeError{kind: decodeErrMalformed, msg: "malformed JSON"}
}

func writeDecodeError(w http.ResponseWriter, err error) {
	var de *decodeError
	if !errors.As(err, &de) {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "malformed JSON", "")
		return
	}
	switch de.kind {
	case decodeErrUnknownField:
		writeAPIError(w, http.StatusBadRequest, "invalid_field", "unknown field", de.field)
	case decodeErrInvalidType:
		writeAPIError(w, http.StatusBadRequest, "invalid_type", "invalid type for field", de.field)
	case decodeErrEmpty:
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "request body is required", "")
	case decodeErrTrailingData:
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "request body must contain a single JSON object", "")
	case decodeErrTooLarge:
		writeAPIError(w, http.StatusRequestEntityTooLarge, "payload_too_large", "request body too large", "")
	default:
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "malformed JSON", "")
	}
}

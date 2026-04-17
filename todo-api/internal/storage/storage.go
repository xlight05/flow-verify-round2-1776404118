package storage

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/flow-verify-round2/todo-api/internal/models"
)

var (
	ErrNotFound  = errors.New("todo not found")
	ErrForbidden = errors.New("forbidden")
)

type Store struct {
	mu        sync.RWMutex
	todos     map[string]*models.Todo
	itemLocks map[string]*sync.Mutex
}

func New() *Store {
	s := &Store{
		todos:     make(map[string]*models.Todo),
		itemLocks: make(map[string]*sync.Mutex),
	}
	s.seed()
	return s
}

func (s *Store) seed() {
	now := time.Now().UTC()
	samples := []models.Todo{
		{ID: "todo-1", Title: "Write design doc", Completed: false, CreatedAt: now.Add(-72 * time.Hour), Owner: "user-1"},
		{ID: "todo-2", Title: "Review pull requests", Completed: false, CreatedAt: now.Add(-48 * time.Hour), Owner: "user-1"},
		{ID: "todo-3", Title: "Ship release notes", Completed: false, CreatedAt: now.Add(-24 * time.Hour), Owner: "user-1"},
	}
	for i := range samples {
		t := samples[i]
		s.todos[t.ID] = &t
		s.itemLocks[t.ID] = &sync.Mutex{}
	}
}

func generateID() (string, error) {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "todo-" + hex.EncodeToString(buf), nil
}

func (s *Store) lockFor(id string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	lock, ok := s.itemLocks[id]
	if !ok {
		lock = &sync.Mutex{}
		s.itemLocks[id] = lock
	}
	return lock
}

func (s *Store) Create(title, owner string) (models.Todo, error) {
	id, err := generateID()
	if err != nil {
		return models.Todo{}, err
	}
	todo := &models.Todo{
		ID:        id,
		Title:     title,
		Completed: false,
		CreatedAt: time.Now().UTC(),
		Owner:     owner,
	}

	s.mu.Lock()
	s.todos[id] = todo
	s.itemLocks[id] = &sync.Mutex{}
	s.mu.Unlock()

	return *todo, nil
}

func (s *Store) List(owner string) []models.Todo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]models.Todo, 0, len(s.todos))
	for _, t := range s.todos {
		if t.Owner == owner {
			result = append(result, *t)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	return result
}

func (s *Store) Get(id, owner string) (models.Todo, error) {
	s.mu.RLock()
	todo, ok := s.todos[id]
	s.mu.RUnlock()
	if !ok {
		return models.Todo{}, ErrNotFound
	}
	if todo.Owner != owner {
		return models.Todo{}, ErrForbidden
	}
	return *todo, nil
}

// SetCompleted performs an idempotent completion state transition under a per-resource lock.
// Returns ErrNotFound if the todo doesn't exist and ErrForbidden if the caller doesn't own it.
func (s *Store) SetCompleted(id, owner string, completed bool) (models.Todo, error) {
	s.mu.RLock()
	_, exists := s.todos[id]
	s.mu.RUnlock()
	if !exists {
		return models.Todo{}, ErrNotFound
	}

	lock := s.lockFor(id)
	lock.Lock()
	defer lock.Unlock()

	s.mu.Lock()
	defer s.mu.Unlock()

	todo, ok := s.todos[id]
	if !ok {
		return models.Todo{}, ErrNotFound
	}
	if todo.Owner != owner {
		return models.Todo{}, ErrForbidden
	}

	if completed {
		todo.Completed = true
		if todo.CompletedAt == nil {
			now := time.Now().UTC()
			todo.CompletedAt = &now
		}
	} else {
		todo.Completed = false
		todo.CompletedAt = nil
	}

	return *todo, nil
}

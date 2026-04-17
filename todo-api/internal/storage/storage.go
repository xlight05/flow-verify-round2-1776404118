package storage

import (
	"crypto/rand"
	"encoding/hex"
	"sort"
	"sync"
	"time"

	"github.com/flow-verify-round2/todo-api/internal/models"
)

type Store struct {
	mu       sync.RWMutex
	todos    map[string]models.Todo
	usedIDs  map[string]struct{}
}

func New() *Store {
	return &Store{
		todos:   make(map[string]models.Todo),
		usedIDs: make(map[string]struct{}),
	}
}

func (s *Store) generateID() (string, error) {
	buf := make([]byte, 16)
	for attempt := 0; attempt < 10; attempt++ {
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		id := hex.EncodeToString(buf)
		if _, taken := s.usedIDs[id]; !taken {
			return id, nil
		}
	}
	// Fall back to appending a counter-like timestamp suffix for extreme edge cases.
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf) + "-" + hex.EncodeToString([]byte(time.Now().UTC().Format(time.RFC3339Nano))), nil
}

func (s *Store) Create(title, description string) (models.Todo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id, err := s.generateID()
	if err != nil {
		return models.Todo{}, err
	}

	todo := models.Todo{
		ID:          id,
		Title:       title,
		Description: description,
		Done:        false,
		CreatedAt:   time.Now().UTC(),
	}
	s.todos[id] = todo
	s.usedIDs[id] = struct{}{}
	return todo, nil
}

func (s *Store) List() []models.Todo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]models.Todo, 0, len(s.todos))
	for _, t := range s.todos {
		result = append(result, t)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	return result
}

func (s *Store) Delete(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.todos[id]; !ok {
		return false
	}
	delete(s.todos, id)
	return true
}

package storage

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/vedsharma/apicli/internal/model"
)

const (
	historyFile     = "history.json"
	collectionsFile = "collections.json"
)

// Storage handles JSON file persistence
type Storage struct {
	dataDir string
}

// NewStorage creates a new storage instance
func NewStorage() (*Storage, error) {
	// Use ~/.apicli as default data directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	dataDir := filepath.Join(homeDir, ".apicli")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, err
	}

	return &Storage{dataDir: dataDir}, nil
}

// historyPath returns the path to the history file
func (s *Storage) historyPath() string {
	return filepath.Join(s.dataDir, historyFile)
}

// collectionsPath returns the path to the collections file
func (s *Storage) collectionsPath() string {
	return filepath.Join(s.dataDir, collectionsFile)
}

// LoadHistory loads the request history from disk
func (s *Storage) LoadHistory() (*model.History, error) {
	history := &model.History{Requests: []model.Request{}}

	data, err := os.ReadFile(s.historyPath())
	if err != nil {
		if os.IsNotExist(err) {
			return history, nil
		}
		return nil, err
	}

	if err := json.Unmarshal(data, history); err != nil {
		return nil, err
	}

	return history, nil
}

// SaveHistory saves the request history to disk
func (s *Storage) SaveHistory(history *model.History) error {
	data, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.historyPath(), data, 0644)
}

// AddToHistory adds a request to history
func (s *Storage) AddToHistory(req model.Request) error {
	history, err := s.LoadHistory()
	if err != nil {
		return err
	}

	// Prepend new request (most recent first)
	history.Requests = append([]model.Request{req}, history.Requests...)

	// Keep only last 100 requests
	if len(history.Requests) > 100 {
		history.Requests = history.Requests[:100]
	}

	return s.SaveHistory(history)
}

// ClearHistory clears all history
func (s *Storage) ClearHistory() error {
	return s.SaveHistory(&model.History{Requests: []model.Request{}})
}

// GetHistoryRequest gets a specific request by ID
func (s *Storage) GetHistoryRequest(id string) (*model.Request, error) {
	history, err := s.LoadHistory()
	if err != nil {
		return nil, err
	}

	for _, req := range history.Requests {
		if req.ID == id {
			return &req, nil
		}
	}

	return nil, nil
}

// LoadCollections loads all collections from disk
func (s *Storage) LoadCollections() (*model.Collections, error) {
	collections := &model.Collections{Collections: make(map[string]model.Collection)}

	data, err := os.ReadFile(s.collectionsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return collections, nil
		}
		return nil, err
	}

	if err := json.Unmarshal(data, collections); err != nil {
		return nil, err
	}

	return collections, nil
}

// SaveCollections saves all collections to disk
func (s *Storage) SaveCollections(collections *model.Collections) error {
	data, err := json.MarshalIndent(collections, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.collectionsPath(), data, 0644)
}

// CreateCollection creates a new collection
func (s *Storage) CreateCollection(name string) error {
	collections, err := s.LoadCollections()
	if err != nil {
		return err
	}

	if _, exists := collections.Collections[name]; exists {
		return nil // Collection already exists
	}

	collections.Collections[name] = model.Collection{
		Name:     name,
		Requests: []model.SavedRequest{},
	}

	return s.SaveCollections(collections)
}

// DeleteCollection deletes a collection
func (s *Storage) DeleteCollection(name string) error {
	collections, err := s.LoadCollections()
	if err != nil {
		return err
	}

	delete(collections.Collections, name)

	return s.SaveCollections(collections)
}

// GetCollection gets a collection by name
func (s *Storage) GetCollection(name string) (*model.Collection, error) {
	collections, err := s.LoadCollections()
	if err != nil {
		return nil, err
	}

	if col, exists := collections.Collections[name]; exists {
		return &col, nil
	}

	return nil, nil
}

// AddToCollection adds a request to a collection
func (s *Storage) AddToCollection(collectionName string, req model.SavedRequest) error {
	collections, err := s.LoadCollections()
	if err != nil {
		return err
	}

	col, exists := collections.Collections[collectionName]
	if !exists {
		col = model.Collection{
			Name:     collectionName,
			Requests: []model.SavedRequest{},
		}
	}

	col.Requests = append(col.Requests, req)
	collections.Collections[collectionName] = col

	return s.SaveCollections(collections)
}

package storage

import (
	"encoding/json"
	"os"
	"path/filepath"

	"api/internal/model"
)

const (
	historyFile     = "history.json"
	collectionsFile = "collections.json"
	aliasesFile     = "aliases.json"

	// Secure file permissions - owner read/write only
	jsonSecureFileMode = 0600 // -rw-------
	jsonSecureDirMode  = 0700 // drwx------
)

// JSONStorage handles JSON file persistence
type JSONStorage struct {
	dataDir string
}

// NewJSONStorage creates a new JSON storage instance
func NewJSONStorage() (*JSONStorage, error) {
	// Use ~/.apicli as default data directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	dataDir := filepath.Join(homeDir, ".apicli")
	if err := os.MkdirAll(dataDir, jsonSecureDirMode); err != nil {
		return nil, err
	}

	return &JSONStorage{dataDir: dataDir}, nil
}

// historyPath returns the path to the history file
func (s *JSONStorage) historyPath() string {
	return filepath.Join(s.dataDir, historyFile)
}

// collectionsPath returns the path to the collections file
func (s *JSONStorage) collectionsPath() string {
	return filepath.Join(s.dataDir, collectionsFile)
}

// aliasesPath returns the path to the aliases file
func (s *JSONStorage) aliasesPath() string {
	return filepath.Join(s.dataDir, aliasesFile)
}

// LoadHistory loads the request history from disk
func (s *JSONStorage) LoadHistory() (*model.History, error) {
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
func (s *JSONStorage) SaveHistory(history *model.History) error {
	data, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.historyPath(), data, jsonSecureFileMode)
}

// AddToHistory adds a request to history
func (s *JSONStorage) AddToHistory(req model.Request) error {
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
func (s *JSONStorage) ClearHistory() error {
	return s.SaveHistory(&model.History{Requests: []model.Request{}})
}

// GetHistoryRequest gets a specific request by ID
func (s *JSONStorage) GetHistoryRequest(id string) (*model.Request, error) {
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
func (s *JSONStorage) LoadCollections() (*model.Collections, error) {
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
func (s *JSONStorage) SaveCollections(collections *model.Collections) error {
	data, err := json.MarshalIndent(collections, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.collectionsPath(), data, jsonSecureFileMode)
}

// CreateCollection creates a new collection
func (s *JSONStorage) CreateCollection(name string) error {
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
func (s *JSONStorage) DeleteCollection(name string) error {
	collections, err := s.LoadCollections()
	if err != nil {
		return err
	}

	delete(collections.Collections, name)

	return s.SaveCollections(collections)
}

// GetCollection gets a collection by name
func (s *JSONStorage) GetCollection(name string) (*model.Collection, error) {
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
func (s *JSONStorage) AddToCollection(collectionName string, req model.SavedRequest) error {
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

// LoadAliases loads all aliases from disk
func (s *JSONStorage) LoadAliases() (*model.Aliases, error) {
	aliases := &model.Aliases{Aliases: make(map[string]string)}

	data, err := os.ReadFile(s.aliasesPath())
	if err != nil {
		if os.IsNotExist(err) {
			return aliases, nil
		}
		return nil, err
	}

	if err := json.Unmarshal(data, aliases); err != nil {
		return nil, err
	}

	return aliases, nil
}

// SaveAliases saves all aliases to disk
func (s *JSONStorage) SaveAliases(aliases *model.Aliases) error {
	data, err := json.MarshalIndent(aliases, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.aliasesPath(), data, jsonSecureFileMode)
}

// CreateAlias creates a new alias
func (s *JSONStorage) CreateAlias(name, url string) error {
	aliases, err := s.LoadAliases()
	if err != nil {
		return err
	}

	aliases.Aliases[name] = url

	return s.SaveAliases(aliases)
}

// DeleteAlias deletes an alias
func (s *JSONStorage) DeleteAlias(name string) error {
	aliases, err := s.LoadAliases()
	if err != nil {
		return err
	}

	delete(aliases.Aliases, name)

	return s.SaveAliases(aliases)
}

// GetAlias gets an alias URL by name
func (s *JSONStorage) GetAlias(name string) (string, bool, error) {
	aliases, err := s.LoadAliases()
	if err != nil {
		return "", false, err
	}

	url, exists := aliases.Aliases[name]
	return url, exists, nil
}

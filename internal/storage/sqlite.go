package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"api/internal/model"

	_ "modernc.org/sqlite"
)

// parseJSONHeaders safely parses JSON headers, returning an empty map on error
func parseJSONHeaders(jsonStr string) (map[string]string, error) {
	if jsonStr == "" {
		return make(map[string]string), nil
	}

	var headers map[string]string
	if err := json.Unmarshal([]byte(jsonStr), &headers); err != nil {
		return make(map[string]string), fmt.Errorf("failed to parse headers JSON: %w", err)
	}

	if headers == nil {
		headers = make(map[string]string)
	}
	return headers, nil
}

const (
	dbFile = "apicli.db"

	// Secure file permissions - owner read/write only
	secureFileMode = 0600 // -rw-------
	secureDirMode  = 0700 // drwx------
)

// ensureSecureFile creates a file with secure permissions if it doesn't exist,
// or verifies/fixes permissions if it does exist. This prevents a TOCTOU race
// condition where the file could be created with insecure default permissions.
func ensureSecureFile(path string) error {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		// File doesn't exist - create it with secure permissions
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, secureFileMode)
		if err != nil {
			return fmt.Errorf("failed to create secure file: %w", err)
		}
		f.Close()
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	// File exists - check and fix permissions if needed
	if info.Mode().Perm() != secureFileMode {
		if err := os.Chmod(path, secureFileMode); err != nil {
			return fmt.Errorf("failed to set secure permissions: %w", err)
		}
	}
	return nil
}

// SQLiteStorage handles SQLite database persistence
type SQLiteStorage struct {
	db      *sql.DB
	dataDir string
}

// NewStorage creates a new SQLite storage instance
func NewStorage() (*SQLiteStorage, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	dataDir := filepath.Join(homeDir, ".apicli")
	if err := os.MkdirAll(dataDir, secureDirMode); err != nil {
		return nil, err
	}

	dbPath := filepath.Join(dataDir, dbFile)

	// Create database file with secure permissions if it doesn't exist
	// This avoids a race condition where the file is created with default
	// permissions and then chmod'd afterward
	if err := ensureSecureFile(dbPath); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	// Enable foreign keys
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		db.Close()
		return nil, err
	}

	s := &SQLiteStorage{db: db, dataDir: dataDir}

	if err := s.initSchema(); err != nil {
		db.Close()
		return nil, err
	}

	// Attempt migration from JSON files if database is empty
	if err := s.migrateFromJSON(); err != nil {
		// Log but don't fail - migration errors shouldn't prevent startup
	}

	return s, nil
}

// Close closes the database connection
func (s *SQLiteStorage) Close() error {
	return s.db.Close()
}

// initSchema creates the database tables if they don't exist
func (s *SQLiteStorage) initSchema() error {
	schema := `
	-- History table (stores Request + embedded Response)
	CREATE TABLE IF NOT EXISTS history (
		id TEXT PRIMARY KEY,
		timestamp DATETIME NOT NULL,
		method TEXT NOT NULL,
		url TEXT NOT NULL,
		headers TEXT DEFAULT '{}',
		body TEXT DEFAULT '',
		response_status_code INTEGER,
		response_status TEXT,
		response_headers TEXT,
		response_body TEXT,
		response_duration_ms INTEGER
	);
	CREATE INDEX IF NOT EXISTS idx_history_timestamp ON history(timestamp DESC);

	-- Collections table
	CREATE TABLE IF NOT EXISTS collections (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT UNIQUE NOT NULL
	);

	-- Saved requests (belongs to collection)
	CREATE TABLE IF NOT EXISTS saved_requests (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		collection_id INTEGER NOT NULL,
		name TEXT DEFAULT '',
		method TEXT NOT NULL,
		url TEXT NOT NULL,
		headers TEXT DEFAULT '{}',
		body TEXT DEFAULT '',
		position INTEGER NOT NULL,
		FOREIGN KEY (collection_id) REFERENCES collections(id) ON DELETE CASCADE
	);
	CREATE INDEX IF NOT EXISTS idx_saved_requests_collection ON saved_requests(collection_id, position);

	-- Aliases table
	CREATE TABLE IF NOT EXISTS aliases (
		name TEXT PRIMARY KEY,
		url TEXT NOT NULL
	);
	`

	_, err := s.db.Exec(schema)
	return err
}

// =============================================================================
// History Operations
// =============================================================================

// LoadHistory loads the request history from the database
func (s *SQLiteStorage) LoadHistory() (*model.History, error) {
	rows, err := s.db.Query(`
		SELECT id, timestamp, method, url, headers, body,
		       response_status_code, response_status, response_headers,
		       response_body, response_duration_ms
		FROM history
		ORDER BY timestamp DESC
		LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	history := &model.History{Requests: []model.Request{}}

	for rows.Next() {
		var req model.Request
		var headersJSON string
		var respStatusCode, respDurationMs sql.NullInt64
		var respStatus, respHeaders, respBody sql.NullString

		err := rows.Scan(
			&req.ID, &req.Timestamp, &req.Method, &req.URL,
			&headersJSON, &req.Body,
			&respStatusCode, &respStatus, &respHeaders,
			&respBody, &respDurationMs,
		)
		if err != nil {
			return nil, err
		}

		// Parse headers JSON (errors are logged but don't fail the operation)
		req.Headers, _ = parseJSONHeaders(headersJSON)

		// Build response if present
		if respStatusCode.Valid {
			req.Response = &model.Response{
				StatusCode: int(respStatusCode.Int64),
				Status:     respStatus.String,
				Body:       respBody.String,
				DurationMs: respDurationMs.Int64,
			}
			if respHeaders.Valid {
				req.Response.Headers, _ = parseJSONHeaders(respHeaders.String)
			} else {
				req.Response.Headers = make(map[string]string)
			}
		}

		history.Requests = append(history.Requests, req)
	}

	return history, rows.Err()
}

// SaveHistory replaces all history with the provided data
func (s *SQLiteStorage) SaveHistory(history *model.History) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Clear existing history
	if _, err := tx.Exec("DELETE FROM history"); err != nil {
		return err
	}

	// Insert all requests
	for _, req := range history.Requests {
		if err := s.insertHistoryRequest(tx, req); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// AddToHistory adds a request to history
func (s *SQLiteStorage) AddToHistory(req model.Request) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Insert the new request
	if err := s.insertHistoryRequest(tx, req); err != nil {
		return err
	}

	// Enforce 100-request limit by deleting oldest entries
	_, err = tx.Exec(`
		DELETE FROM history
		WHERE id NOT IN (
			SELECT id FROM history ORDER BY timestamp DESC LIMIT 100
		)`)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// insertHistoryRequest is a helper to insert a request into history
func (s *SQLiteStorage) insertHistoryRequest(tx *sql.Tx, req model.Request) error {
	headersJSON, _ := json.Marshal(req.Headers)

	var respStatusCode, respDurationMs sql.NullInt64
	var respStatus, respHeaders, respBody sql.NullString

	if req.Response != nil {
		respStatusCode = sql.NullInt64{Int64: int64(req.Response.StatusCode), Valid: true}
		respStatus = sql.NullString{String: req.Response.Status, Valid: true}
		respHeadersJSON, _ := json.Marshal(req.Response.Headers)
		respHeaders = sql.NullString{String: string(respHeadersJSON), Valid: true}
		respBody = sql.NullString{String: req.Response.Body, Valid: true}
		respDurationMs = sql.NullInt64{Int64: req.Response.DurationMs, Valid: true}
	}

	_, err := tx.Exec(`
		INSERT OR REPLACE INTO history (
			id, timestamp, method, url, headers, body,
			response_status_code, response_status, response_headers,
			response_body, response_duration_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		req.ID, req.Timestamp, req.Method, req.URL, string(headersJSON), req.Body,
		respStatusCode, respStatus, respHeaders, respBody, respDurationMs,
	)
	return err
}

// ClearHistory clears all history
func (s *SQLiteStorage) ClearHistory() error {
	_, err := s.db.Exec("DELETE FROM history")
	return err
}

// GetHistoryRequest gets a specific request by ID
func (s *SQLiteStorage) GetHistoryRequest(id string) (*model.Request, error) {
	row := s.db.QueryRow(`
		SELECT id, timestamp, method, url, headers, body,
		       response_status_code, response_status, response_headers,
		       response_body, response_duration_ms
		FROM history
		WHERE id = ?`, id)

	var req model.Request
	var headersJSON string
	var respStatusCode, respDurationMs sql.NullInt64
	var respStatus, respHeaders, respBody sql.NullString

	err := row.Scan(
		&req.ID, &req.Timestamp, &req.Method, &req.URL,
		&headersJSON, &req.Body,
		&respStatusCode, &respStatus, &respHeaders,
		&respBody, &respDurationMs,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	// Parse headers JSON (errors are logged but don't fail the operation)
	req.Headers, _ = parseJSONHeaders(headersJSON)

	// Build response if present
	if respStatusCode.Valid {
		req.Response = &model.Response{
			StatusCode: int(respStatusCode.Int64),
			Status:     respStatus.String,
			Body:       respBody.String,
			DurationMs: respDurationMs.Int64,
		}
		if respHeaders.Valid {
			req.Response.Headers, _ = parseJSONHeaders(respHeaders.String)
		} else {
			req.Response.Headers = make(map[string]string)
		}
	}

	return &req, nil
}

// =============================================================================
// Collection Operations
// =============================================================================

// LoadCollections loads all collections from the database
func (s *SQLiteStorage) LoadCollections() (*model.Collections, error) {
	collections := &model.Collections{Collections: make(map[string]model.Collection)}

	// Load all collections
	colRows, err := s.db.Query("SELECT id, name FROM collections")
	if err != nil {
		return nil, err
	}
	defer colRows.Close()

	type colInfo struct {
		id   int64
		name string
	}
	var cols []colInfo

	for colRows.Next() {
		var info colInfo
		if err := colRows.Scan(&info.id, &info.name); err != nil {
			return nil, err
		}
		cols = append(cols, info)
	}
	if err := colRows.Err(); err != nil {
		return nil, err
	}

	// Load requests for each collection
	for _, col := range cols {
		collection := model.Collection{
			Name:     col.name,
			Requests: []model.SavedRequest{},
		}

		reqRows, err := s.db.Query(`
			SELECT name, method, url, headers, body
			FROM saved_requests
			WHERE collection_id = ?
			ORDER BY position`, col.id)
		if err != nil {
			return nil, err
		}

		for reqRows.Next() {
			var req model.SavedRequest
			var headersJSON string
			if err := reqRows.Scan(&req.Name, &req.Method, &req.URL, &headersJSON, &req.Body); err != nil {
				reqRows.Close()
				return nil, err
			}
			// Parse headers JSON (errors are logged but don't fail the operation)
			req.Headers, _ = parseJSONHeaders(headersJSON)
			collection.Requests = append(collection.Requests, req)
		}
		reqRows.Close()

		if err := reqRows.Err(); err != nil {
			return nil, err
		}

		collections.Collections[col.name] = collection
	}

	return collections, nil
}

// SaveCollections replaces all collections with the provided data
func (s *SQLiteStorage) SaveCollections(collections *model.Collections) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Clear existing data (cascade will delete saved_requests)
	if _, err := tx.Exec("DELETE FROM collections"); err != nil {
		return err
	}

	// Insert all collections and their requests
	for name, col := range collections.Collections {
		result, err := tx.Exec("INSERT INTO collections (name) VALUES (?)", name)
		if err != nil {
			return err
		}
		colID, _ := result.LastInsertId()

		for i, req := range col.Requests {
			headersJSON, _ := json.Marshal(req.Headers)
			_, err := tx.Exec(`
				INSERT INTO saved_requests (collection_id, name, method, url, headers, body, position)
				VALUES (?, ?, ?, ?, ?, ?, ?)`,
				colID, req.Name, req.Method, req.URL, string(headersJSON), req.Body, i)
			if err != nil {
				return err
			}
		}
	}

	return tx.Commit()
}

// CreateCollection creates a new collection
func (s *SQLiteStorage) CreateCollection(name string) error {
	_, err := s.db.Exec("INSERT OR IGNORE INTO collections (name) VALUES (?)", name)
	return err
}

// DeleteCollection deletes a collection
func (s *SQLiteStorage) DeleteCollection(name string) error {
	_, err := s.db.Exec("DELETE FROM collections WHERE name = ?", name)
	return err
}

// GetCollection gets a collection by name
func (s *SQLiteStorage) GetCollection(name string) (*model.Collection, error) {
	var colID int64
	err := s.db.QueryRow("SELECT id FROM collections WHERE name = ?", name).Scan(&colID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	collection := &model.Collection{
		Name:     name,
		Requests: []model.SavedRequest{},
	}

	rows, err := s.db.Query(`
		SELECT name, method, url, headers, body
		FROM saved_requests
		WHERE collection_id = ?
		ORDER BY position`, colID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var req model.SavedRequest
		var headersJSON string
		if err := rows.Scan(&req.Name, &req.Method, &req.URL, &headersJSON, &req.Body); err != nil {
			return nil, err
		}
		// Parse headers JSON (errors are logged but don't fail the operation)
		req.Headers, _ = parseJSONHeaders(headersJSON)
		collection.Requests = append(collection.Requests, req)
	}

	return collection, rows.Err()
}

// AddToCollection adds a request to a collection
func (s *SQLiteStorage) AddToCollection(collectionName string, req model.SavedRequest) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Get or create collection
	var colID int64
	err = tx.QueryRow("SELECT id FROM collections WHERE name = ?", collectionName).Scan(&colID)
	if err == sql.ErrNoRows {
		result, err := tx.Exec("INSERT INTO collections (name) VALUES (?)", collectionName)
		if err != nil {
			return err
		}
		colID, _ = result.LastInsertId()
	} else if err != nil {
		return err
	}

	// Get next position
	var maxPos sql.NullInt64
	tx.QueryRow("SELECT MAX(position) FROM saved_requests WHERE collection_id = ?", colID).Scan(&maxPos)
	nextPos := int64(0)
	if maxPos.Valid {
		nextPos = maxPos.Int64 + 1
	}

	// Insert request
	headersJSON, _ := json.Marshal(req.Headers)
	_, err = tx.Exec(`
		INSERT INTO saved_requests (collection_id, name, method, url, headers, body, position)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		colID, req.Name, req.Method, req.URL, string(headersJSON), req.Body, nextPos)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// =============================================================================
// Alias Operations
// =============================================================================

// LoadAliases loads all aliases from the database
func (s *SQLiteStorage) LoadAliases() (*model.Aliases, error) {
	aliases := &model.Aliases{Aliases: make(map[string]string)}

	rows, err := s.db.Query("SELECT name, url FROM aliases")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var name, url string
		if err := rows.Scan(&name, &url); err != nil {
			return nil, err
		}
		aliases.Aliases[name] = url
	}

	return aliases, rows.Err()
}

// SaveAliases replaces all aliases with the provided data
func (s *SQLiteStorage) SaveAliases(aliases *model.Aliases) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Clear existing aliases
	if _, err := tx.Exec("DELETE FROM aliases"); err != nil {
		return err
	}

	// Insert all aliases
	for name, url := range aliases.Aliases {
		if _, err := tx.Exec("INSERT INTO aliases (name, url) VALUES (?, ?)", name, url); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// CreateAlias creates a new alias
func (s *SQLiteStorage) CreateAlias(name, url string) error {
	_, err := s.db.Exec(`
		INSERT INTO aliases (name, url) VALUES (?, ?)
		ON CONFLICT(name) DO UPDATE SET url = excluded.url`,
		name, url)
	return err
}

// DeleteAlias deletes an alias
func (s *SQLiteStorage) DeleteAlias(name string) error {
	_, err := s.db.Exec("DELETE FROM aliases WHERE name = ?", name)
	return err
}

// GetAlias gets an alias URL by name
func (s *SQLiteStorage) GetAlias(name string) (string, bool, error) {
	var url string
	err := s.db.QueryRow("SELECT url FROM aliases WHERE name = ?", name).Scan(&url)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return url, true, nil
}

// =============================================================================
// Migration from JSON
// =============================================================================

// migrateFromJSON migrates data from JSON files to SQLite if they exist
func (s *SQLiteStorage) migrateFromJSON() error {
	// Check if we've already migrated (database has data)
	var count int
	s.db.QueryRow("SELECT COUNT(*) FROM history").Scan(&count)
	if count > 0 {
		return nil // Already has data, skip migration
	}

	// Also check collections and aliases
	s.db.QueryRow("SELECT COUNT(*) FROM collections").Scan(&count)
	if count > 0 {
		return nil
	}
	s.db.QueryRow("SELECT COUNT(*) FROM aliases").Scan(&count)
	if count > 0 {
		return nil
	}

	// Migrate history
	historyPath := filepath.Join(s.dataDir, historyFile)
	if data, err := os.ReadFile(historyPath); err == nil {
		var history model.History
		if json.Unmarshal(data, &history) == nil && len(history.Requests) > 0 {
			for _, req := range history.Requests {
				s.AddToHistory(req)
			}
			os.Rename(historyPath, historyPath+".migrated")
		}
	}

	// Migrate collections
	collectionsPath := filepath.Join(s.dataDir, collectionsFile)
	if data, err := os.ReadFile(collectionsPath); err == nil {
		var collections model.Collections
		if json.Unmarshal(data, &collections) == nil && len(collections.Collections) > 0 {
			for name, col := range collections.Collections {
				s.CreateCollection(name)
				for _, req := range col.Requests {
					s.AddToCollection(name, req)
				}
			}
			os.Rename(collectionsPath, collectionsPath+".migrated")
		}
	}

	// Migrate aliases
	aliasesPath := filepath.Join(s.dataDir, aliasesFile)
	if data, err := os.ReadFile(aliasesPath); err == nil {
		var aliases model.Aliases
		if json.Unmarshal(data, &aliases) == nil && len(aliases.Aliases) > 0 {
			for name, url := range aliases.Aliases {
				s.CreateAlias(name, url)
			}
			os.Rename(aliasesPath, aliasesPath+".migrated")
		}
	}

	return nil
}

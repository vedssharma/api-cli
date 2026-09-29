package storage

import (
	"database/sql"
	"fmt"
)

const activeEnvSetting = "active_environment"

// CreateEnvironment creates an empty environment. It is not an error if it already exists.
func (s *SQLiteStorage) CreateEnvironment(name string) error {
	_, err := s.db.Exec("INSERT OR IGNORE INTO environments (name) VALUES (?)", name)
	return err
}

// EnvironmentExists reports whether an environment exists
func (s *SQLiteStorage) EnvironmentExists(name string) (bool, error) {
	var n int
	err := s.db.QueryRow("SELECT COUNT(*) FROM environments WHERE name = ?", name).Scan(&n)
	return n > 0, err
}

// ListEnvironments returns all environment names in alphabetical order
func (s *SQLiteStorage) ListEnvironments() ([]string, error) {
	rows, err := s.db.Query("SELECT name FROM environments ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		names = append(names, n)
	}
	return names, rows.Err()
}

// DeleteEnvironment removes an environment and its variables. If it was the
// active environment, no environment is active afterwards.
func (s *SQLiteStorage) DeleteEnvironment(name string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("DELETE FROM env_vars WHERE env = ?", name); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM environments WHERE name = ?", name); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM settings WHERE key = ? AND value = ?", activeEnvSetting, name); err != nil {
		return err
	}
	return tx.Commit()
}

// SetEnvVar sets a variable in an existing environment
func (s *SQLiteStorage) SetEnvVar(env, key, value string) error {
	exists, err := s.EnvironmentExists(env)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("environment '%s' not found", env)
	}
	_, err = s.db.Exec(`
		INSERT INTO env_vars (env, key, value) VALUES (?, ?, ?)
		ON CONFLICT(env, key) DO UPDATE SET value = excluded.value`,
		env, key, value)
	return err
}

// UnsetEnvVar removes a variable from an environment. It reports whether the variable existed.
func (s *SQLiteStorage) UnsetEnvVar(env, key string) (bool, error) {
	res, err := s.db.Exec("DELETE FROM env_vars WHERE env = ? AND key = ?", env, key)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// GetEnvVars returns all variables of an environment
func (s *SQLiteStorage) GetEnvVars(env string) (map[string]string, error) {
	rows, err := s.db.Query("SELECT key, value FROM env_vars WHERE env = ?", env)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	vars := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		vars[k] = v
	}
	return vars, rows.Err()
}

// SetActiveEnvironment makes an existing environment the active one.
// An empty name clears the active environment.
func (s *SQLiteStorage) SetActiveEnvironment(name string) error {
	if name == "" {
		_, err := s.db.Exec("DELETE FROM settings WHERE key = ?", activeEnvSetting)
		return err
	}
	exists, err := s.EnvironmentExists(name)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("environment '%s' not found", name)
	}
	_, err = s.db.Exec(`
		INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		activeEnvSetting, name)
	return err
}

// GetActiveEnvironment returns the active environment name, or "" if none is set
func (s *SQLiteStorage) GetActiveEnvironment() (string, error) {
	var name string
	err := s.db.QueryRow("SELECT value FROM settings WHERE key = ?", activeEnvSetting).Scan(&name)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return name, err
}

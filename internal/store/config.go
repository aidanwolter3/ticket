package store

import "database/sql"

// ConfigSet persists a key/value pair in the config table.
func (s *Store) ConfigSet(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO config (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value)
	return err
}

// ConfigGet returns the value for the given key and whether it was found.
func (s *Store) ConfigGet(key string) (string, bool, error) {
	var value string
	err := s.db.QueryRow(`SELECT value FROM config WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}

// ConfigGetDefault returns the value for key, or defaultVal if not set.
func (s *Store) ConfigGetDefault(key, defaultVal string) (string, error) {
	v, ok, err := s.ConfigGet(key)
	if err != nil {
		return "", err
	}
	if !ok {
		return defaultVal, nil
	}
	return v, nil
}

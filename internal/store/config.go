package store

import (
	"database/sql"
	"sort"
	"strings"
)

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

// ConfigList returns all key/value pairs from the config table.
func (s *Store) ConfigList() (map[string]string, error) {
	rows, err := s.db.Query(`SELECT key, value FROM config`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		result[k] = v
	}
	return result, rows.Err()
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

// NamedConfig manages per-named-config override keys using the prefix
// "named.<name>.<key>" in the shared config table.
type NamedConfig struct {
	s *Store
}

// Named returns the NamedConfig manager for this store.
func (s *Store) Named() *NamedConfig {
	return &NamedConfig{s: s}
}

const namedPrefix = "named."

func namedKey(name, key string) string {
	return namedPrefix + name + "." + key
}

// SetNamed persists an override for the given named config.
func (nc *NamedConfig) SetNamed(name, key, value string) error {
	return nc.s.ConfigSet(namedKey(name, key), value)
}

// GetNamed returns the named override for the given name+key, if set.
func (nc *NamedConfig) GetNamed(name, key string) (string, bool, error) {
	return nc.s.ConfigGet(namedKey(name, key))
}

// ListNamed returns all overridden keys for a named config (strip the prefix).
func (nc *NamedConfig) ListNamed(name string) (map[string]string, error) {
	prefix := namedPrefix + name + "."
	all, err := nc.s.ConfigList()
	if err != nil {
		return nil, err
	}
	result := make(map[string]string)
	for k, v := range all {
		if strings.HasPrefix(k, prefix) {
			result[k[len(prefix):]] = v
		}
	}
	return result, nil
}

// ListAllNamedConfigs returns all distinct named config names (sorted).
func (nc *NamedConfig) ListAllNamedConfigs() ([]string, error) {
	all, err := nc.s.ConfigList()
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	var names []string
	for k := range all {
		if !strings.HasPrefix(k, namedPrefix) {
			continue
		}
		rest := k[len(namedPrefix):]
		dotIdx := strings.Index(rest, ".")
		if dotIdx < 0 {
			continue
		}
		name := rest[:dotIdx]
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names, nil
}

// GetEffective returns the named override for name+key if set, else the global
// value for key, else globalDefault. If name is empty, skips the named lookup.
func (nc *NamedConfig) GetEffective(name, key, globalDefault string) (string, error) {
	if name != "" {
		v, ok, err := nc.GetNamed(name, key)
		if err != nil {
			return "", err
		}
		if ok {
			return v, nil
		}
	}
	return nc.s.ConfigGetDefault(key, globalDefault)
}

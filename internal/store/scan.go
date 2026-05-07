package store

import (
	"database/sql"
	"time"
)

type scanner interface {
	Scan(dest ...interface{}) error
}

func fromMs(ms int64) time.Time {
	return time.UnixMilli(ms)
}

func fromNullMs(ms sql.NullInt64) *time.Time {
	if !ms.Valid {
		return nil
	}
	t := time.UnixMilli(ms.Int64)
	return &t
}

func nullStr(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func nullTime(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return t.UnixMilli()
}

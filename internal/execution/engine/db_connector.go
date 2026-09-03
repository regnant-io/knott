// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"database/sql"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"
)

// callDatabase runs SQL against a relational database. The pilot ships with the
// pure-Go SQLite driver (no CGO, already a dependency). The connection string is
// supplied via the DATABASE_DSN credential (or a 'dsn' input). For Postgres/MySQL
// an operator adds the matching driver and sets driver=postgres|mysql.
//
// Actions:
//
//	query (default): returns rows ([]map) for SELECTs
//	exec:            returns rows_affected for INSERT/UPDATE/DELETE
//
// Inputs:
//
//	sql (required), driver (default sqlite), dsn (or DATABASE_DSN credential),
//	params (list — positional query parameters)
func (e *Executor) callDatabase(connectorID, action string, in map[string]any) (map[string]any, error) {
	driver := strings.ToLower(firstNonEmpty(str(in["driver"]), connectorID))
	switch driver {
	case "database", "sql", "":
		driver = "sqlite"
	case "postgres", "postgresql":
		driver = "postgres"
	case "mysql":
		driver = "mysql"
	case "sqlite", "sqlite3":
		driver = "sqlite"
	}

	dsn := firstNonEmpty(e.resolveSecretRef(in["dsn"]), e.secret("DATABASE_DSN"))
	if dsn == "" {
		return nil, fmt.Errorf("database connector requires a connection string (DATABASE_DSN credential or 'dsn' input)")
	}
	query := strings.TrimSpace(str(in["sql"]))
	if query == "" {
		return nil, fmt.Errorf("database connector requires a 'sql' statement")
	}

	// Only the SQLite driver is registered by default. Other drivers must be
	// compiled in by the operator; we fail clearly rather than silently.
	if driver != "sqlite" && !driverRegistered(driver) {
		return nil, fmt.Errorf("database driver %q is not available in this build; ship a build with the %s driver registered, or use sqlite", driver, driver)
	}

	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("database open failed: %w", err)
	}
	defer db.Close()

	params := toAnyList(in["params"])

	switch defaultAction(action, "query") {
	case "query", "select":
		rows, err := db.Query(query, params...)
		if err != nil {
			return nil, fmt.Errorf("query failed: %w", err)
		}
		defer rows.Close()
		cols, _ := rows.Columns()
		var result []map[string]any
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				return nil, fmt.Errorf("scan failed: %w", err)
			}
			row := map[string]any{}
			for i, c := range cols {
				row[c] = normalizeSQLValue(vals[i])
			}
			result = append(result, row)
		}
		if result == nil {
			result = []map[string]any{}
		}
		return map[string]any{"rows": result, "row_count": len(result)}, nil

	case "exec", "execute", "insert", "update", "delete":
		res, err := db.Exec(query, params...)
		if err != nil {
			return nil, fmt.Errorf("exec failed: %w", err)
		}
		affected, _ := res.RowsAffected()
		lastID, _ := res.LastInsertId()
		return map[string]any{"rows_affected": affected, "last_insert_id": lastID}, nil

	default:
		return nil, fmt.Errorf("database: unknown action %q (supported: query, exec)", action)
	}
}

// normalizeSQLValue makes scanned values JSON-friendly ([]byte → string).
func normalizeSQLValue(v any) any {
	if b, ok := v.([]byte); ok {
		return string(b)
	}
	return v
}

// driverRegistered reports whether a database/sql driver name is registered.
func driverRegistered(name string) bool {
	for _, d := range sql.Drivers() {
		if d == name {
			return true
		}
	}
	return false
}

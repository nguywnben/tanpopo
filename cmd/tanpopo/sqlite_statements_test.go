// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package main

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unsafe"

	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
	"modernc.org/libc"
	modernsqlite "modernc.org/sqlite/lib"
)

func sqliteOpenStatements(conn *sql.Conn) ([]string, error) {
	var statements []string
	err := conn.Raw(func(raw any) error {
		value := reflect.ValueOf(raw)
		if value.Kind() != reflect.Pointer || value.IsNil() || value.Elem().Type().PkgPath() != "modernc.org/sqlite" {
			return fmt.Errorf("unexpected SQLite driver connection type %T", raw)
		}
		value = value.Elem()
		dbField := value.FieldByName("db")
		tlsField := value.FieldByName("tls")
		if !dbField.IsValid() || dbField.Kind() != reflect.Uintptr || !tlsField.IsValid() || tlsField.Kind() != reflect.Pointer {
			return fmt.Errorf("modernc SQLite connection layout changed")
		}
		driverDB := uintptr(dbField.Uint())
		tls := (*libc.TLS)(unsafe.Pointer(tlsField.Pointer()))
		if driverDB == 0 || tls == nil {
			return fmt.Errorf("SQLite connection is already closed")
		}
		var statement uintptr
		for statement = modernsqlite.Xsqlite3_next_stmt(tls, driverDB, statement); statement != 0; statement = modernsqlite.Xsqlite3_next_stmt(tls, driverDB, statement) {
			statements = append(statements, strings.TrimSpace(libc.GoString(modernsqlite.Xsqlite3_sql(tls, statement))))
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("inspect SQLite statements: %w", err)
	}
	return statements, nil
}

func TestSQLiteOpenStatementsDiagnosticListsPreparedSQL(t *testing.T) {
	store, err := sqlite.Open(t.Context(), sqlite.Config{DBPath: filepath.Join(t.TempDir(), "diagnostic.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	conn, err := store.RawDB().Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	rows, err := conn.QueryContext(t.Context(), "SELECT 37")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	statements, err := sqliteOpenStatements(conn)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range statements {
		if statement == "SELECT 37" {
			return
		}
	}
	t.Fatalf("diagnostic missed prepared SQL; got %q", statements)
}

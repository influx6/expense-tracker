package store

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

var (
	ErrorFailedSchemaInit = DbError{Code: "DB_INIT_FAILED", Label: "Failed to initialize schema for database", IsTerminal: true}
	ErrorFailedInsert     = DbError{Code: "FAILED_INSERT", Label: "Failed to insert record into table"}
)

type DbError struct {
	Code       string
	Label      string
	IsTerminal bool
	wrapped    error
}

func (d DbError) Is(other error) bool {
	if otherErr, ok := other.(*DbError); ok {
		return otherErr.Code == d.Code
	}
	return false
}

func (d DbError) Unwrap() error {
	return d.wrapped
}

func (d DbError) withErr(err error) DbError {
	d.wrapped = err
	return d
}

func (d *DbError) Error() string {
	if d.wrapped != nil {
		return fmt.Sprintf("DbError(%s): Failed due to: %q (is_terminal: %v)\n%v", d.Code, d.Label, d.IsTerminal, d.wrapped)
	}
	return fmt.Sprintf("DbError(%s): Failed due to: %q (is_terminal: %v)", d.Code, d.Label, d.IsTerminal)
}

// `ExpenseStore` provides a core store for retrieving
// and storing transactions, account entries and handles
// initializing the database with relevant tables.
type ExpenseStore struct {
	db *sql.DB
}

func SqliteExpenseStore(dbPath string, waitTimeout uint32) (*ExpenseStore, error) {
	database_dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(%d)", dbPath, waitTimeout)

	db, err := sql.Open("sqlite", database_dsn)
	if err != nil {
		return nil, err
	}

	return NewExpenseStore(db)
}

func NewExpenseStore(db *sql.DB) (*ExpenseStore, error) {
	// check db actually is responding, ok to let it be
	// rechecked.
	if err := db.Ping(); err != nil {
		return nil, err
	}
	store = &ExpenseStore{db}

	if

	return store, nil
}

func (es *ExpenseStore) createSchema() error {
	if _, err := es.db.exec(`
		CREATE TABLE IF NOT EXISTS account (

		);
	`); err !=  nil {
		return ErrorFailedSchemaInit.withErr(err)
	}
	return nil
}

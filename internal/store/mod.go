package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"uuid"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"

	"github.com/influx6/expense-tracker/internal/models"
)

var _ error = DbError{}

// DbErrorCode identifies the kind of store failure.
type DbErrorCode string

const (
	CodeSchemaInit      DbErrorCode = "DB_INIT_FAILED"
	CodeInsert          DbErrorCode = "FAILED_INSERT"
	CodeQueryFailed     DbErrorCode = "QUERY_FAILED"
	CodeAccountNotFound DbErrorCode = "ACCOUNT_NOT_FOUND"
	CodeNotImplemented  DbErrorCode = "NOT_IMPLEMENTED"
)

var (
	ErrorSchemaInit      = DbError{Code: CodeSchemaInit, Label: "Failed to initialize schema for database", IsTerminal: true}
	ErrorInsert          = DbError{Code: CodeInsert, Label: "Failed to insert record into table"}
	ErrorQueryFailed     = DbError{Code: CodeQueryFailed, Label: "Failed to query database", IsTerminal: true}
	ErrorAccountNotFound = DbError{Code: CodeAccountNotFound, Label: "Account not found"}
	ErrorNotImplemented  = DbError{Code: CodeNotImplemented, Label: "not implemented", IsTerminal: true}
)

type DbError struct {
	Code       DbErrorCode
	Label      string
	IsTerminal bool
	wrapped    error
}

func (d DbError) Is(other error) bool {
	if otherErr, ok := other.(DbError); ok {
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

func (d DbError) Error() string {
	if d.wrapped != nil {
		return fmt.Sprintf("DbError(%s):  due to: %q (is_terminal: %v)\n%v", d.Code, d.Label, d.IsTerminal, d.wrapped)
	}
	return fmt.Sprintf("DbError(%s):  due to: %q (is_terminal: %v)", d.Code, d.Label, d.IsTerminal)
}

// `ExpenseStore` provides a core store for retrieving
// and storing transactions, account entries and handles
// initializing the database with relevant tables.
type ExpenseStore struct {
	db *sqlx.DB
}

func SqliteExpenseStore(dbPath string, waitTimeout uint32) (*ExpenseStore, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(%d)&_pragma=foreign_keys(1)", dbPath, waitTimeout)

	db, openErr := sqlx.Open("sqlite", dsn)
	if openErr != nil {
		return nil, openErr
	}

	// check db actually is responding
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}

	var store, err = newExpenseStore(db)
	if err != nil {
		_ = db.Close()
		return nil, err
	}

	return store, nil
}

func newExpenseStore(db *sqlx.DB) (*ExpenseStore, error) {
	store := &ExpenseStore{db}
	if err := store.createSchema(); err != nil {
		return nil, err
	}

	return store, nil
}

func (es *ExpenseStore) createSchema() error {
	if _, err := es.db.Exec(`
		CREATE TABLE IF NOT EXISTS accounts (
			account_id TEXT PRIMARY KEY,
			account_name TEXT NOT NULL,
			account_type TEXT NOT NULL CHECK (account_type IN ('ASSET', 'INCOME', 'LIABILITY', 'EXPENSE')),
			user_id TEXT NOT NULL,
			currency TEXT NOT NULL DEFAULT 'HKD' CHECK (currency = 'HKD'),
			created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
			UNIQUE (user_id, account_name, account_type)
		);

		CREATE TABLE IF NOT EXISTS transactions (
			transaction_id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			description TEXT NOT NULL,
			occured_at TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
		);

		CREATE TABLE IF NOT EXISTS entries (
			entry_id TEXT PRIMARY KEY,
			amount TEXT NOT NULL,
			transaction_id TEXT NOT NULL REFERENCES transactions(transaction_id) ON DELETE CASCADE,
			account_id TEXT NOT NULL REFERENCES accounts(account_id) ON DELETE RESTRICT,
			direction TEXT NOT NULL CHECK (direction IN ('SOURCE', 'DESTINATION')),
			created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
			UNIQUE (transaction_id, direction)
		);

		CREATE INDEX IF NOT EXISTS entries__transaction_id_index ON entries(transaction_id);
		CREATE INDEX IF NOT EXISTS entries__account_id_index ON entries(account_id);
		CREATE INDEX IF NOT EXISTS transaction__occured_at_index ON transactions(occured_at);
		CREATE INDEX IF NOT EXISTS transaction__user_id_index ON transactions(user_id);
		CREATE INDEX IF NOT EXISTS accounts__user_id_index ON accounts(user_id);
	`); err != nil {
		return ErrorSchemaInit.withErr(err)
	}
	return nil
}

const defaultCurrency = "HKD"

func (es *ExpenseStore) getOrCreateAccount(accountName string, accountType models.AccountType, userID uuid.UUID) (models.Account, error) {
	accountName = strings.ToLower(accountName)

	found, err := es.findAccount(accountName, accountType, userID)
	if err == nil {
		return found, nil
	}
	if !errors.Is(err, ErrorAccountNotFound) {
		return models.Account{}, err
	}

	account := models.Account{
		AccountID:   uuid.New(),
		AccountName: accountName,
		AccountType: accountType,
		UserID:      userID,
		Currency:    defaultCurrency,
	}
	if err := account.Validate(); err != nil {
		return models.Account{}, err
	}

	// INSERT OR IGNORE covers a concurrent insert that hits the unique key.
	if _, err := es.db.Exec(`
		INSERT OR IGNORE INTO accounts (account_id, account_name, account_type, user_id, currency)
		VALUES (?, ?, ?, ?, ?)`,
		account.AccountID.String(),
		account.AccountName,
		string(account.AccountType),
		account.UserID.String(),
		account.Currency,
	); err != nil {
		return models.Account{}, ErrorInsert.withErr(err)
	}

	return es.findAccount(accountName, accountType, userID)
}

type accountRow struct {
	AccountID   string `db:"account_id"`
	AccountName string `db:"account_name"`
	AccountType string `db:"account_type"`
	Currency    string `db:"currency"`
	CreatedAt   string `db:"created_at"`
}

func (es *ExpenseStore) findAccount(accountName string, accountType models.AccountType, userID uuid.UUID) (models.Account, error) {
	var row accountRow

	if err := es.db.Get(&row, `
		SELECT account_id, account_name, account_type, currency, created_at
		FROM accounts
		WHERE user_id = ? AND account_name = ? AND account_type = ?`,
		userID.String(), accountName, string(accountType),
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.Account{}, ErrorAccountNotFound.withErr(err)
		}
		return models.Account{}, ErrorQueryFailed.withErr(err)
	}

	accountID, err := uuid.Parse(row.AccountID)
	if err != nil {
		return models.Account{}, ErrorQueryFailed.withErr(err)
	}
	createdAt, err := time.Parse(time.RFC3339, row.CreatedAt)
	if err != nil {
		return models.Account{}, ErrorQueryFailed.withErr(err)
	}

	return models.Account{
		AccountID:   accountID,
		AccountName: row.AccountName,
		AccountType: models.AccountType(row.AccountType),
		UserID:      userID,
		Currency:    row.Currency,
		CreatedAt:   createdAt,
	}, nil
}

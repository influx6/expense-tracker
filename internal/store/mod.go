package store

import (
	"database/sql"
	"errors"
	"fmt"
	"iter"
	"time"
	"uuid"

	"github.com/govalues/decimal"
	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"

	"github.com/influx6/expense-tracker/internal/models"
)

var _ error = DbError{}

type DbErrorCode string

const (
	CodeSchemaInit         DbErrorCode = "DB_INIT_FAILED"
	CodeInsert             DbErrorCode = "FAILED_INSERT"
	CodeQueryFailed        DbErrorCode = "QUERY_FAILED"
	CodeAccountNotFound    DbErrorCode = "ACCOUNT_NOT_FOUND"
	CodeTransactionFailed  DbErrorCode = "TRANSACTION_FAILED"
	CodeEntryCountMismatch DbErrorCode = "ENTRY_COUNT_MISMATCH"
	CodeCurrencyMismatch   DbErrorCode = "CURRENCY_MISMATCH"
	CodeNotImplemented     DbErrorCode = "NOT_IMPLEMENTED"
)

var (
	ErrorSchemaInit         = DbError{Code: CodeSchemaInit, Label: "Failed to initialize schema for database", IsTerminal: true}
	ErrorInsert             = DbError{Code: CodeInsert, Label: "Failed to insert record into table"}
	ErrorQueryFailed        = DbError{Code: CodeQueryFailed, Label: "Failed to query database", IsTerminal: true}
	ErrorAccountNotFound    = DbError{Code: CodeAccountNotFound, Label: "Account not found"}
	ErrorTransactionFailed  = DbError{Code: CodeTransactionFailed, Label: "Transaction failed", IsTerminal: true}
	ErrorEntryCountMismatch = DbError{Code: CodeEntryCountMismatch, Label: "Entry count must be twice the transaction count"}
	ErrorCurrencyMismatch   = DbError{Code: CodeCurrencyMismatch, Label: "Entry currency does not match account currency"}
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
			currency TEXT NOT NULL,
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
		CREATE UNIQUE INDEX IF NOT EXISTS accounts__one_asset_per_user
			ON accounts(user_id) WHERE account_type = 'ASSET';
	`); err != nil {
		return ErrorSchemaInit.withErr(err)
	}
	return nil
}

func (es *ExpenseStore) GetOrCreateAccount(accountName string, accountType models.AccountType, userID uuid.UUID, currency string) (models.Account, error) {
	category, err := models.ToAccountCategory(accountName)
	if err != nil {
		return models.Account{}, err
	}

	found, err := es.FindAccount(category, accountType, userID)
	if err == nil {
		return found, nil
	}
	if !errors.Is(err, ErrorAccountNotFound) {
		return models.Account{}, err
	}

	account := models.Account{
		AccountID:       uuid.New(),
		AccountCategory: category,
		AccountType:     accountType,
		UserID:          userID,
		Currency:        currency,
	}
	if err := account.Validate(); err != nil {
		return models.Account{}, err
	}

	// Ignore only a concurrent insert with the same unique key.
	if _, err := es.db.Exec(`
		INSERT INTO accounts (account_id, account_name, account_type, user_id, currency)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (user_id, account_name, account_type) DO NOTHING`,
		account.AccountID.String(),
		string(account.AccountCategory),
		string(account.AccountType),
		account.UserID.String(),
		account.Currency,
	); err != nil {
		return models.Account{}, ErrorInsert.withErr(err)
	}

	return es.FindAccount(category, accountType, userID)
}

type accountRow struct {
	AccountID   string `db:"account_id"`
	AccountName string `db:"account_name"`
	AccountType string `db:"account_type"`
	Currency    string `db:"currency"`
	CreatedAt   string `db:"created_at"`
}

func (r accountRow) toAccount(userID uuid.UUID) (models.Account, error) {
	accountID, err := uuid.Parse(r.AccountID)
	if err != nil {
		return models.Account{}, ErrorQueryFailed.withErr(err)
	}
	category, err := models.ToAccountCategory(r.AccountName)
	if err != nil {
		return models.Account{}, ErrorQueryFailed.withErr(err)
	}
	createdAt, err := time.Parse(time.RFC3339, r.CreatedAt)
	if err != nil {
		return models.Account{}, ErrorQueryFailed.withErr(err)
	}

	return models.Account{
		AccountID:       accountID,
		AccountCategory: category,
		AccountType:     models.AccountType(r.AccountType),
		UserID:          userID,
		Currency:        r.Currency,
		CreatedAt:       createdAt,
	}, nil
}

func (es *ExpenseStore) AllAccounts(userID uuid.UUID, currency string) iter.Seq2[models.Account, error] {
	return func(yield func(models.Account, error) bool) {
		rows, err := es.db.Queryx(`
			SELECT account_id, account_name, account_type, currency, created_at
			FROM accounts
			WHERE user_id = ? AND currency = ?`, userID.String(), currency)
		if err != nil {
			yield(models.Account{}, ErrorQueryFailed.withErr(err))
			return
		}
		defer rows.Close()

		for rows.Next() {
			var row accountRow
			if err := rows.StructScan(&row); err != nil {
				yield(models.Account{}, ErrorQueryFailed.withErr(err))
				return
			}
			account, err := row.toAccount(userID)
			if err != nil {
				yield(models.Account{}, err)
				return
			}
			if !yield(account, nil) {
				return
			}
		}
		if err := rows.Err(); err != nil {
			yield(models.Account{}, ErrorQueryFailed.withErr(err))
		}
	}
}

type entryRow struct {
	EntryID       string `db:"entry_id"`
	Amount        string `db:"amount"`
	TransactionID string `db:"transaction_id"`
	AccountID     string `db:"account_id"`
	Direction     string `db:"direction"`
	Currency      string `db:"currency"`
	CreatedAt     string `db:"created_at"`
}

func (r entryRow) toEntry() (models.Entry, error) {
	entryID, err := uuid.Parse(r.EntryID)
	if err != nil {
		return models.Entry{}, ErrorQueryFailed.withErr(err)
	}
	transactionID, err := uuid.Parse(r.TransactionID)
	if err != nil {
		return models.Entry{}, ErrorQueryFailed.withErr(err)
	}
	accountID, err := uuid.Parse(r.AccountID)
	if err != nil {
		return models.Entry{}, ErrorQueryFailed.withErr(err)
	}
	amount, err := decimal.Parse(r.Amount)
	if err != nil {
		return models.Entry{}, ErrorQueryFailed.withErr(err)
	}
	createdAt, err := time.Parse(time.RFC3339, r.CreatedAt)
	if err != nil {
		return models.Entry{}, ErrorQueryFailed.withErr(err)
	}

	entry := models.Entry{
		EntryID:       entryID,
		TransactionID: transactionID,
		AccountID:     accountID,
		Amount:        amount,
		Currency:      r.Currency,
		Direction:     models.Direction(r.Direction),
		CreatedAt:     createdAt,
	}
	if err := entry.Validate(); err != nil {
		return models.Entry{}, ErrorQueryFailed.withErr(err)
	}
	return entry, nil
}

func (es *ExpenseStore) entriesSeq(query string, args ...any) iter.Seq2[models.Entry, error] {
	return func(yield func(models.Entry, error) bool) {
		rows, err := es.db.Queryx(query, args...)
		if err != nil {
			yield(models.Entry{}, ErrorQueryFailed.withErr(err))
			return
		}
		defer rows.Close()

		for rows.Next() {
			var row entryRow
			if err := rows.StructScan(&row); err != nil {
				yield(models.Entry{}, ErrorQueryFailed.withErr(err))
				return
			}
			entry, err := row.toEntry()
			if err != nil {
				yield(models.Entry{}, err)
				return
			}
			if !yield(entry, nil) {
				return
			}
		}
		if err := rows.Err(); err != nil {
			yield(models.Entry{}, ErrorQueryFailed.withErr(err))
		}
	}
}

const entryColumns = `e.entry_id, e.amount, e.transaction_id, e.account_id, e.direction, e.created_at, a.currency`

func (es *ExpenseStore) AllEntriesByAccount(account models.Account) iter.Seq2[models.Entry, error] {
	return es.entriesSeq(`
		SELECT `+entryColumns+`
		FROM entries e
		JOIN accounts a ON a.account_id = e.account_id
		WHERE e.account_id = ? AND a.currency = ?
		ORDER BY e.created_at, e.entry_id`, account.AccountID.String(), account.Currency)
}

func (es *ExpenseStore) AllEntriesByTransaction(transaction models.Transaction, currency string) iter.Seq2[models.Entry, error] {
	return es.entriesSeq(`
		SELECT `+entryColumns+`
		FROM entries e
		JOIN accounts a ON a.account_id = e.account_id
		WHERE e.transaction_id = ? AND a.currency = ?
		ORDER BY e.created_at, e.entry_id`, transaction.TransactionID.String(), currency)
}

func (es *ExpenseStore) AllEntries(userID uuid.UUID, currency string) iter.Seq2[models.Entry, error] {
	return es.entriesSeq(`
		SELECT `+entryColumns+`
		FROM entries e
		JOIN transactions t ON t.transaction_id = e.transaction_id
		JOIN accounts a ON a.account_id = e.account_id
		WHERE t.user_id = ? AND a.currency = ?
		ORDER BY e.created_at, e.entry_id`, userID.String(), currency)
}

func (es *ExpenseStore) FindAccount(category models.AccountCategory, accountType models.AccountType, userID uuid.UUID) (models.Account, error) {
	var row accountRow

	if err := es.db.Get(&row, `
		SELECT account_id, account_name, account_type, currency, created_at
		FROM accounts
		WHERE user_id = ? AND account_name = ? AND account_type = ?`,
		userID.String(), string(category), string(accountType),
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.Account{}, ErrorAccountNotFound.withErr(err)
		}
		return models.Account{}, ErrorQueryFailed.withErr(err)
	}

	return row.toAccount(userID)
}

func (es *ExpenseStore) InsertAllWith(transactions []models.Transaction, entries []models.Entry, after func(tx *sqlx.Tx) error) error {
	return es.withTx(func(tx *sqlx.Tx) error {
		if err := insertTransactionsAndEntries(tx, transactions, entries); err != nil {
			return err
		}
		if after != nil {
			return after(tx)
		}
		return nil
	})
}

func insertTransactionsAndEntries(tx *sqlx.Tx, transactions []models.Transaction, entries []models.Entry) error {
	if len(entries) != 2*len(transactions) {
		return ErrorEntryCountMismatch.withErr(fmt.Errorf(
			"%d transactions require %d entries, got %d",
			len(transactions), 2*len(transactions), len(entries),
		))
	}

	if err := insertTransactions(tx, transactions); err != nil {
		return err
	}
	return insertEntries(tx, entries)
}

func insertTransactions(tx *sqlx.Tx, transactions []models.Transaction) error {
	stmt, err := tx.Preparex(`
		INSERT INTO transactions (transaction_id, user_id, description, occured_at)
		VALUES (?, ?, ?, ?)`)
	if err != nil {
		return ErrorInsert.withErr(err)
	}
	defer stmt.Close()

	for _, t := range transactions {
		if err := t.Validate(); err != nil {
			return err
		}
		if _, err := stmt.Exec(
			t.TransactionID.String(),
			t.UserID.String(),
			t.Description,
			t.OccuredAt.UTC().Format(time.RFC3339),
		); err != nil {
			return ErrorInsert.withErr(err)
		}
	}
	return nil
}

func insertEntries(tx *sqlx.Tx, entries []models.Entry) error {
	stmt, err := tx.Preparex(`
		INSERT INTO entries (entry_id, amount, transaction_id, account_id, direction)
		VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return ErrorInsert.withErr(err)
	}
	defer stmt.Close()

	for _, e := range entries {
		if err := e.Validate(); err != nil {
			return err
		}

		var accountCurrency string
		if err := tx.Get(&accountCurrency, `SELECT currency FROM accounts WHERE account_id = ?`, e.AccountID.String()); err != nil {
			return ErrorInsert.withErr(err)
		}
		if accountCurrency != e.Currency {
			return ErrorCurrencyMismatch.withErr(fmt.Errorf(
				"entry %s currency %q does not match account %s currency %q",
				e.EntryID, e.Currency, e.AccountID, accountCurrency,
			))
		}

		if _, err := stmt.Exec(
			e.EntryID.String(),
			e.Amount.String(),
			e.TransactionID.String(),
			e.AccountID.String(),
			string(e.Direction),
		); err != nil {
			return ErrorInsert.withErr(err)
		}
	}
	return nil
}

func (es *ExpenseStore) withTx(fn func(*sqlx.Tx) error) error {
	tx, err := es.db.Beginx()
	if err != nil {
		return ErrorTransactionFailed.withErr(err)
	}

	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}

	if err := tx.Commit(); err != nil {
		return ErrorTransactionFailed.withErr(err)
	}
	return nil
}

package store

import (
	"context"
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

func SqliteExpenseStore(ctx context.Context, dbPath string, waitTimeout uint32) (*ExpenseStore, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(%d)&_pragma=foreign_keys(1)", dbPath, waitTimeout)

	db, openErr := sqlx.Open("sqlite", dsn)
	if openErr != nil {
		return nil, openErr
	}

	// check db actually is responding
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}

	var store, err = newExpenseStore(ctx, db)
	if err != nil {
		_ = db.Close()
		return nil, err
	}

	return store, nil
}

func newExpenseStore(ctx context.Context, db *sqlx.DB) (*ExpenseStore, error) {
	store := &ExpenseStore{db}
	if err := store.createSchema(ctx); err != nil {
		return nil, err
	}

	return store, nil
}

func (es *ExpenseStore) createSchema(ctx context.Context) error {
	if _, err := es.db.ExecContext(ctx, `
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

func (es *ExpenseStore) GetOrCreateAccount(ctx context.Context, accountName string, accountType models.AccountType, userID uuid.UUID, currency string) (models.Account, error) {
	category, err := models.ToAccountCategory(accountName)
	if err != nil {
		return models.Account{}, err
	}

	found, err := es.FindAccount(ctx, category, accountType, userID)
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
	if _, err := es.db.ExecContext(ctx, `
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

	return es.FindAccount(ctx, category, accountType, userID)
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

func (es *ExpenseStore) AllAccounts(ctx context.Context, userID uuid.UUID, currency string) iter.Seq2[models.Account, error] {
	return es.AllAccountsIn(ctx, userID, currency, 0, 0)
}

func (es *ExpenseStore) AllAccountsIn(ctx context.Context, userID uuid.UUID, currency string, year, month int) iter.Seq2[models.Account, error] {
	return func(yield func(models.Account, error) bool) {
		query := `
			SELECT a.account_id, a.account_name, a.account_type, a.currency, a.created_at
			FROM accounts a
			WHERE a.user_id = ? AND a.currency = ?`
		args := []any{userID.String(), currency}

		if year > 0 {
			query += `
			AND EXISTS (
				SELECT 1
				FROM entries e
				JOIN transactions t ON t.transaction_id = e.transaction_id
				WHERE e.account_id = a.account_id AND substr(t.occured_at, 1, 4) = ?`
			args = append(args, fmt.Sprintf("%04d", year))

			if month > 0 {
				query += ` AND substr(t.occured_at, 6, 2) = ?`
				args = append(args, fmt.Sprintf("%02d", month))
			}
			query += `)`
		}

		query += ` ORDER BY a.account_name`

		rows, err := es.db.QueryxContext(ctx, query, args...)
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

func (es *ExpenseStore) entriesSeq(ctx context.Context, query string, args ...any) iter.Seq2[models.Entry, error] {
	return func(yield func(models.Entry, error) bool) {
		rows, err := es.db.QueryxContext(ctx, query, args...)
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

func (es *ExpenseStore) AllEntriesByAccount(ctx context.Context, account models.Account) iter.Seq2[models.Entry, error] {
	return es.AllEntriesByAccountIn(ctx, account, 0, 0)
}

func (es *ExpenseStore) AllEntriesByAccountIn(ctx context.Context, account models.Account, year, month int) iter.Seq2[models.Entry, error] {
	query := `
		SELECT ` + entryColumns + `
		FROM entries e
		JOIN accounts a ON a.account_id = e.account_id
		JOIN transactions t ON t.transaction_id = e.transaction_id
		WHERE e.account_id = ? AND a.currency = ?`
	args := []any{account.AccountID.String(), account.Currency}

	if year > 0 {
		query += ` AND substr(t.occured_at, 1, 4) = ?`
		args = append(args, fmt.Sprintf("%04d", year))

		if month > 0 {
			query += ` AND substr(t.occured_at, 6, 2) = ?`
			args = append(args, fmt.Sprintf("%02d", month))
		}
	}

	query += ` ORDER BY e.created_at, e.entry_id`

	return es.entriesSeq(ctx, query, args...)
}

func (es *ExpenseStore) AllEntriesByTransaction(ctx context.Context, transaction models.Transaction, currency string) iter.Seq2[models.Entry, error] {
	return es.entriesSeq(ctx, `
		SELECT `+entryColumns+`
		FROM entries e
		JOIN accounts a ON a.account_id = e.account_id
		WHERE e.transaction_id = ? AND a.currency = ?
		ORDER BY e.created_at, e.entry_id`, transaction.TransactionID.String(), currency)
}

func (es *ExpenseStore) AllEntries(ctx context.Context, userID uuid.UUID, currency string) iter.Seq2[models.Entry, error] {
	return es.entriesSeq(ctx, `
		SELECT `+entryColumns+`
		FROM entries e
		JOIN transactions t ON t.transaction_id = e.transaction_id
		JOIN accounts a ON a.account_id = e.account_id
		WHERE t.user_id = ? AND a.currency = ?
		ORDER BY e.created_at, e.entry_id`, userID.String(), currency)
}

func (es *ExpenseStore) SumEntryBalance(ctx context.Context, queryer sqlx.QueryerContext, userID uuid.UUID, currency string) (decimal.Decimal, error) {
	rows, err := queryer.QueryxContext(ctx, `
		SELECT e.direction, e.amount
		FROM entries e
		JOIN transactions t ON t.transaction_id = e.transaction_id
		JOIN accounts a ON a.account_id = e.account_id
		WHERE t.user_id = ? AND a.currency = ?`, userID.String(), currency)
	if err != nil {
		return decimal.Decimal{}, ErrorQueryFailed.withErr(err)
	}
	defer rows.Close()

	var total decimal.Decimal
	for rows.Next() {
		var direction, amount string
		if err := rows.Scan(&direction, &amount); err != nil {
			return decimal.Decimal{}, ErrorQueryFailed.withErr(err)
		}

		value, err := decimal.Parse(amount)
		if err != nil {
			return decimal.Decimal{}, ErrorQueryFailed.withErr(err)
		}
		switch models.Direction(direction) {
		case models.DestinationDirection:
			total, err = total.Add(value)
		case models.SourceDirection:
			total, err = total.Sub(value)
		default:
			return decimal.Decimal{}, ErrorQueryFailed.withErr(fmt.Errorf("unknown direction %q", direction))
		}
		if err != nil {
			return decimal.Decimal{}, ErrorQueryFailed.withErr(err)
		}
	}
	if err := rows.Err(); err != nil {
		return decimal.Decimal{}, ErrorQueryFailed.withErr(err)
	}
	return total, nil
}

func (es *ExpenseStore) ZeroSum(ctx context.Context, userID uuid.UUID, currency string) (decimal.Decimal, error) {
	return es.SumEntryBalance(ctx, es.db, userID, currency)
}

func (es *ExpenseStore) FindAccount(ctx context.Context, category models.AccountCategory, accountType models.AccountType, userID uuid.UUID) (models.Account, error) {
	var row accountRow

	if err := es.db.GetContext(ctx, &row, `
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

type InsertCounts struct {
	Transactions int
	Entries      int
}

func (es *ExpenseStore) InsertAllWith(ctx context.Context, transactions []models.Transaction, entries []models.Entry, after func(tx *sqlx.Tx) error) (InsertCounts, error) {
	var counts InsertCounts

	err := es.withTx(ctx, func(tx *sqlx.Tx) error {
		insertedTransactions, insertedEntries, err := insertTransactionsAndEntries(ctx, tx, transactions, entries)
		if err != nil {
			return err
		}
		counts = InsertCounts{Transactions: insertedTransactions, Entries: insertedEntries}

		if after != nil {
			return after(tx)
		}
		return nil
	})
	if err != nil {
		return InsertCounts{}, err
	}
	return counts, nil
}

func insertTransactionsAndEntries(ctx context.Context, tx *sqlx.Tx, transactions []models.Transaction, entries []models.Entry) (int, int, error) {
	if len(entries) != 2*len(transactions) {
		return 0, 0, ErrorEntryCountMismatch.withErr(fmt.Errorf(
			"%d transactions require %d entries, got %d",
			len(transactions), 2*len(transactions), len(entries),
		))
	}

	insertedTransactions, err := insertTransactions(ctx, tx, transactions)
	if err != nil {
		return 0, 0, err
	}
	insertedEntries, err := insertEntries(ctx, tx, entries)
	if err != nil {
		return 0, 0, err
	}
	return insertedTransactions, insertedEntries, nil
}

func insertTransactions(ctx context.Context, tx *sqlx.Tx, transactions []models.Transaction) (int, error) {
	stmt, err := tx.PreparexContext(ctx, `
		INSERT INTO transactions (transaction_id, user_id, description, occured_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (transaction_id) DO NOTHING`)
	if err != nil {
		return 0, ErrorInsert.withErr(err)
	}
	defer stmt.Close()

	inserted := 0
	for _, t := range transactions {
		if err := t.Validate(); err != nil {
			return 0, err
		}
		result, err := stmt.Exec(
			t.TransactionID.String(),
			t.UserID.String(),
			t.Description,
			t.OccuredAt.UTC().Format(time.RFC3339),
		)
		if err != nil {
			return 0, ErrorInsert.withErr(err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return 0, ErrorInsert.withErr(err)
		}
		inserted += int(affected)
	}
	return inserted, nil
}

func insertEntries(ctx context.Context, tx *sqlx.Tx, entries []models.Entry) (int, error) {
	stmt, err := tx.PreparexContext(ctx, `
		INSERT INTO entries (entry_id, amount, transaction_id, account_id, direction)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (entry_id) DO NOTHING`)
	if err != nil {
		return 0, ErrorInsert.withErr(err)
	}
	defer stmt.Close()

	inserted := 0
	for _, e := range entries {
		if err := e.Validate(); err != nil {
			return 0, err
		}

		var accountCurrency string
		if err := tx.GetContext(ctx, &accountCurrency, `SELECT currency FROM accounts WHERE account_id = ?`, e.AccountID.String()); err != nil {
			return 0, ErrorInsert.withErr(err)
		}
		if accountCurrency != e.Currency {
			return 0, ErrorCurrencyMismatch.withErr(fmt.Errorf(
				"entry %s currency %q does not match account %s currency %q",
				e.EntryID, e.Currency, e.AccountID, accountCurrency,
			))
		}

		result, err := stmt.Exec(
			e.EntryID.String(),
			e.Amount.String(),
			e.TransactionID.String(),
			e.AccountID.String(),
			string(e.Direction),
		)
		if err != nil {
			return 0, ErrorInsert.withErr(err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return 0, ErrorInsert.withErr(err)
		}
		inserted += int(affected)
	}
	return inserted, nil
}

func (es *ExpenseStore) withTx(ctx context.Context, fn func(*sqlx.Tx) error) error {
	tx, err := es.db.BeginTxx(ctx, nil)
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

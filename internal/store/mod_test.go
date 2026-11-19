package store

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/govalues/decimal"
	"github.com/jmoiron/sqlx"

	"github.com/influx6/expense-tracker/internal/models"
)

func newTestStore(t *testing.T) (*ExpenseStore, uuid.UUID) {
	t.Helper()

	store, err := SqliteExpenseStore(filepath.Join(t.TempDir(), "test.db"), 5000)
	if err != nil {
		t.Fatalf("SqliteExpenseStore: %v", err)
	}
	t.Cleanup(func() { store.db.Close() })

	return store, uuid.New()
}

func TestGetOrCreateAccount(t *testing.T) {
	t.Run("creates an account", func(t *testing.T) {
		store, userID := newTestStore(t)

		account, err := store.GetOrCreateAccount("food", models.ExpenseAccount, userID)
		if err != nil {
			t.Fatalf("GetOrCreateAccount: %v", err)
		}
		if account.AccountType != models.ExpenseAccount || account.Currency != "HKD" || account.UserID != userID {
			t.Errorf("account = %+v", account)
		}
		if account.CreatedAt.IsZero() {
			t.Errorf("account has no created_at")
		}
	})

	t.Run("returns the existing account", func(t *testing.T) {
		store, userID := newTestStore(t)

		first, err := store.GetOrCreateAccount("food", models.ExpenseAccount, userID)
		if err != nil {
			t.Fatalf("GetOrCreateAccount: %v", err)
		}
		second, err := store.GetOrCreateAccount("food", models.ExpenseAccount, userID)
		if err != nil {
			t.Fatalf("GetOrCreateAccount: %v", err)
		}
		if first.AccountID != second.AccountID {
			t.Errorf("second call created a new account: %v != %v", first.AccountID, second.AccountID)
		}

		var count int
		if err := store.db.QueryRow(`SELECT COUNT(*) FROM accounts`).Scan(&count); err != nil {
			t.Fatalf("count accounts: %v", err)
		}
		if count != 1 {
			t.Errorf("accounts table has %d rows, want 1", count)
		}
	})

	t.Run("creates a separate account per type", func(t *testing.T) {
		store, userID := newTestStore(t)

		expense, err := store.GetOrCreateAccount("food", models.ExpenseAccount, userID)
		if err != nil {
			t.Fatalf("GetOrCreateAccount: %v", err)
		}
		income, err := store.GetOrCreateAccount("food", models.IncomeAccount, userID)
		if err != nil {
			t.Fatalf("GetOrCreateAccount: %v", err)
		}
		if expense.AccountID == income.AccountID {
			t.Errorf("different account types share an account ID")
		}
	})

	t.Run("matches names regardless of case", func(t *testing.T) {
		store, userID := newTestStore(t)

		lower, err := store.GetOrCreateAccount("food", models.ExpenseAccount, userID)
		if err != nil {
			t.Fatalf("GetOrCreateAccount: %v", err)
		}
		mixed, err := store.GetOrCreateAccount("Food", models.ExpenseAccount, userID)
		if err != nil {
			t.Fatalf("GetOrCreateAccount: %v", err)
		}
		if lower.AccountID != mixed.AccountID {
			t.Errorf("name case created a new account: %v != %v", lower.AccountID, mixed.AccountID)
		}
	})
}

func TestStoreAccountErrors(t *testing.T) {
	t.Run("query error", func(t *testing.T) {
		store, userID := newTestStore(t)

		if _, err := store.db.Exec(`DROP TABLE accounts`); err != nil {
			t.Fatalf("drop accounts: %v", err)
		}
		if _, err := store.GetOrCreateAccount("food", models.ExpenseAccount, userID); !errors.Is(err, ErrorQueryFailed) {
			t.Errorf("query error = %v, want ErrorQueryFailed", err)
		}
	})

	t.Run("invalid name", func(t *testing.T) {
		store, userID := newTestStore(t)

		if _, err := store.GetOrCreateAccount("food2", models.ExpenseAccount, userID); !errors.Is(err, models.InvalidAccount) {
			t.Errorf("validation error = %v, want models.InvalidAccount", err)
		}
	})

	t.Run("corrupt account id", func(t *testing.T) {
		store, userID := newTestStore(t)

		if _, err := store.db.Exec(`
			INSERT INTO accounts (account_id, account_name, account_type, user_id, currency)
			VALUES ('not-a-uuid', 'food', 'EXPENSE', ?, 'HKD')`, userID.String()); err != nil {
			t.Fatal(err)
		}
		if _, err := store.GetOrCreateAccount("food", models.ExpenseAccount, userID); !errors.Is(err, ErrorQueryFailed) {
			t.Errorf("bad account_id error = %v, want ErrorQueryFailed", err)
		}
	})

	t.Run("corrupt created at", func(t *testing.T) {
		store, userID := newTestStore(t)

		if _, err := store.db.Exec(`
			INSERT INTO accounts (account_id, account_name, account_type, user_id, currency, created_at)
			VALUES (?, 'income', 'INCOME', ?, 'HKD', 'not-a-time')`, uuid.New().String(), userID.String()); err != nil {
			t.Fatal(err)
		}
		if _, err := store.GetOrCreateAccount("income", models.IncomeAccount, userID); !errors.Is(err, ErrorQueryFailed) {
			t.Errorf("bad created_at error = %v, want ErrorQueryFailed", err)
		}
	})

	t.Run("insert error", func(t *testing.T) {
		store, userID := newTestStore(t)

		if _, err := store.db.Exec(`CREATE TRIGGER block_insert BEFORE INSERT ON accounts
			BEGIN SELECT RAISE(ABORT, 'blocked'); END;`); err != nil {
			t.Fatal(err)
		}
		if _, err := store.GetOrCreateAccount("food", models.ExpenseAccount, userID); !errors.Is(err, ErrorInsert) {
			t.Errorf("insert error = %v, want ErrorInsert", err)
		}
	})

	t.Run("one asset per user", func(t *testing.T) {
		store, userID := newTestStore(t)

		if _, err := store.GetOrCreateAccount("wallet", models.AssetAccount, userID); err != nil {
			t.Fatalf("create wallet account: %v", err)
		}

		_, err := store.db.Exec(`
			INSERT INTO accounts (account_id, account_name, account_type, user_id, currency)
			VALUES (?, 'savings', 'ASSET', ?, 'HKD')`, uuid.New().String(), userID.String())
		if err == nil {
			t.Fatal("a second ASSET account was accepted")
		}
		if !strings.Contains(err.Error(), "UNIQUE constraint failed") {
			t.Errorf("error = %v, want a UNIQUE constraint failure", err)
		}
	})
}

func countRows(t *testing.T, store *ExpenseStore, table string) int {
	t.Helper()

	var count int
	if err := store.db.Get(&count, "SELECT COUNT(*) FROM "+table); err != nil {
		t.Fatal(err)
	}
	return count
}

func testAccounts(t *testing.T, store *ExpenseStore, userID uuid.UUID) (models.Account, models.Account) {
	t.Helper()

	asset, err := store.GetOrCreateAccount("wallet", models.AssetAccount, userID)
	if err != nil {
		t.Fatal(err)
	}
	expense, err := store.GetOrCreateAccount("food", models.ExpenseAccount, userID)
	if err != nil {
		t.Fatal(err)
	}
	return asset, expense
}

func testTransaction(userID uuid.UUID) models.Transaction {
	return models.Transaction{
		TransactionID: uuid.NewV7(),
		UserID:        userID,
		Description:   "coffee",
		OccuredAt:     time.Now().UTC(),
	}
}

func testEntryPair(t *testing.T, transactionID, assetID, expenseID uuid.UUID) []models.Entry {
	t.Helper()

	amount, err := decimal.Parse("10.00")
	if err != nil {
		t.Fatal(err)
	}
	return []models.Entry{
		{
			EntryID:       uuid.NewV7(),
			TransactionID: transactionID,
			AccountID:     assetID,
			Amount:        amount,
			Direction:     models.SourceDirection,
		},
		{
			EntryID:       uuid.NewV7(),
			TransactionID: transactionID,
			AccountID:     expenseID,
			Amount:        amount,
			Direction:     models.DestinationDirection,
		},
	}
}

func TestInsertAllWith(t *testing.T) {
	t.Run("inserts transactions and entries", func(t *testing.T) {
		store, userID := newTestStore(t)
		asset, expense := testAccounts(t, store, userID)
		txn := testTransaction(userID)
		entries := testEntryPair(t, txn.TransactionID, asset.AccountID, expense.AccountID)

		afterCalled := false
		err := store.InsertAllWith([]models.Transaction{txn}, entries, func(tx *sqlx.Tx) error {
			afterCalled = true
			var count int
			if err := tx.Get(&count, `SELECT COUNT(*) FROM entries`); err != nil {
				return err
			}
			if count != 2 {
				t.Errorf("after saw %d entries, want 2", count)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("InsertAllWith: %v", err)
		}
		if !afterCalled {
			t.Error("after hook was not called")
		}
		if got := countRows(t, store, "transactions"); got != 1 {
			t.Errorf("transactions = %d, want 1", got)
		}
		if got := countRows(t, store, "entries"); got != 2 {
			t.Errorf("entries = %d, want 2", got)
		}
	})

	t.Run("rolls back on transaction failure", func(t *testing.T) {
		store, userID := newTestStore(t)
		asset, expense := testAccounts(t, store, userID)
		first := testTransaction(userID)
		second := testTransaction(userID)
		second.TransactionID = first.TransactionID
		entries := append(
			testEntryPair(t, first.TransactionID, asset.AccountID, expense.AccountID),
			testEntryPair(t, second.TransactionID, asset.AccountID, expense.AccountID)...,
		)

		err := store.InsertAllWith([]models.Transaction{first, second}, entries, nil)
		if !errors.Is(err, ErrorInsert) {
			t.Fatalf("error = %v, want ErrorInsert", err)
		}
		if got := countRows(t, store, "transactions"); got != 0 {
			t.Errorf("transactions = %d, want 0", got)
		}
		if got := countRows(t, store, "entries"); got != 0 {
			t.Errorf("entries = %d, want 0", got)
		}
	})

	t.Run("rejects missing entries", func(t *testing.T) {
		cases := []struct {
			name    string
			entries []models.Entry
		}{
			{name: "nil", entries: nil},
			{name: "empty", entries: []models.Entry{}},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				store, userID := newTestStore(t)
				txn := testTransaction(userID)

				err := store.InsertAllWith([]models.Transaction{txn}, tc.entries, nil)
				if !errors.Is(err, ErrorEntryCountMismatch) {
					t.Fatalf("error = %v, want ErrorEntryCountMismatch", err)
				}
				if got := countRows(t, store, "transactions"); got != 0 {
					t.Errorf("transactions = %d, want 0", got)
				}
			})
		}
	})

	t.Run("rolls back both when entries fail", func(t *testing.T) {
		store, userID := newTestStore(t)
		asset, expense := testAccounts(t, store, userID)
		txn := testTransaction(userID)
		entries := testEntryPair(t, txn.TransactionID, asset.AccountID, expense.AccountID)
		entries[1].Direction = models.SourceDirection

		err := store.InsertAllWith([]models.Transaction{txn}, entries, nil)
		if !errors.Is(err, ErrorInsert) {
			t.Fatalf("error = %v, want ErrorInsert", err)
		}
		if got := countRows(t, store, "transactions"); got != 0 {
			t.Errorf("transactions = %d, want 0", got)
		}
		if got := countRows(t, store, "entries"); got != 0 {
			t.Errorf("entries = %d, want 0", got)
		}
	})

	t.Run("rolls back when after fails", func(t *testing.T) {
		store, userID := newTestStore(t)
		asset, expense := testAccounts(t, store, userID)
		txn := testTransaction(userID)
		entries := testEntryPair(t, txn.TransactionID, asset.AccountID, expense.AccountID)
		afterErr := errors.New("balance check failed")

		err := store.InsertAllWith([]models.Transaction{txn}, entries, func(tx *sqlx.Tx) error {
			return afterErr
		})
		if !errors.Is(err, afterErr) {
			t.Fatalf("error = %v, want %v", err, afterErr)
		}
		if got := countRows(t, store, "transactions"); got != 0 {
			t.Errorf("transactions = %d, want 0", got)
		}
		if got := countRows(t, store, "entries"); got != 0 {
			t.Errorf("entries = %d, want 0", got)
		}
	})
}

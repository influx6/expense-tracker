package store

import (
	"errors"
	"path/filepath"
	"testing"
	"uuid"

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
}

package wallet_test

import (
	"errors"
	"maps"
	"path/filepath"
	"testing"
	"time"
	"uuid"

	"github.com/govalues/decimal"
	"github.com/jmoiron/sqlx"

	"github.com/influx6/expense-tracker/internal/models"
	"github.com/influx6/expense-tracker/internal/store"
	"github.com/influx6/expense-tracker/internal/wallet"
)

type AccountSummary struct {
	Transactions uint `db:"transactions"`
	Entries      uint `db:"entries"`
}

func newTestWallet(t *testing.T) (*wallet.Wallet, string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "test.db")
	es, err := store.SqliteExpenseStore(path, 5000)
	if err != nil {
		t.Fatal(err)
	}
	w, err := wallet.NewWallet(es, "HKD", uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	return w, path
}

func openDB(t *testing.T, path string) *sqlx.DB {
	t.Helper()

	db, err := sqlx.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func accountSummaries(t *testing.T, db *sqlx.DB) map[string]AccountSummary {
	t.Helper()

	var rows []struct {
		AccountName  string `db:"account_name"`
		Transactions uint   `db:"transactions"`
		Entries      uint   `db:"entries"`
	}
	if err := db.Select(&rows, `
		SELECT a.account_name,
		       COUNT(DISTINCT e.transaction_id) AS transactions,
		       COUNT(e.entry_id) AS entries
		FROM accounts a
		LEFT JOIN entries e ON e.account_id = a.account_id
		GROUP BY a.account_id, a.account_name`); err != nil {
		t.Fatal(err)
	}

	summaries := make(map[string]AccountSummary, len(rows))
	for _, row := range rows {
		summaries[row.AccountName] = AccountSummary{
			Transactions: row.Transactions,
			Entries:      row.Entries,
		}
	}
	return summaries
}

func testEvent(t *testing.T, eventType models.EventType, amount, category string) models.TransactionEvent {
	t.Helper()

	d, err := decimal.Parse(amount)
	if err != nil {
		t.Fatal(err)
	}
	return models.TransactionEvent{
		OccuredAt: time.Now().UTC(),
		Amount:    d,
		Desc:      "test",
		Category:  models.AccountCategory(category),
		Type:      eventType,
	}
}

func TestApply(t *testing.T) {
	t.Run("applies a batch of valid events", func(t *testing.T) {
		w, path := newTestWallet(t)
		events := []models.TransactionEvent{
			testEvent(t, models.EventIncome, "100.00", "salary"),
			testEvent(t, models.EventExpense, "20.00", "food"),
			testEvent(t, models.EventTransferIn, "50.00", "friend_loan"),
			testEvent(t, models.EventTransferOut, "10.00", "friend_loan"),
		}

		if err := w.Apply(events); err != nil {
			t.Fatalf("Apply: %v", err)
		}

		db := openDB(t, path)
		wantSummaries := map[string]AccountSummary{
			"wallet":      {Transactions: 4, Entries: 4},
			"salary":      {Transactions: 1, Entries: 1},
			"food":        {Transactions: 1, Entries: 1},
			"friend_loan": {Transactions: 2, Entries: 2},
		}
		if got := accountSummaries(t, db); !maps.Equal(got, wantSummaries) {
			t.Errorf("account summaries = %v, want %v", got, wantSummaries)
		}

		var rows []struct {
			AccountType string `db:"account_type"`
			Direction   string `db:"direction"`
		}
		if err := db.Select(&rows, `
			SELECT a.account_type, e.direction
			FROM entries e
			JOIN accounts a ON a.account_id = e.account_id`); err != nil {
			t.Fatal(err)
		}
		got := make(map[string]int, len(rows))
		for _, row := range rows {
			got[row.AccountType+":"+row.Direction]++
		}
		want := map[string]int{
			"ASSET:DESTINATION":     2,
			"ASSET:SOURCE":          2,
			"INCOME:SOURCE":         1,
			"EXPENSE:DESTINATION":   1,
			"LIABILITY:SOURCE":      1,
			"LIABILITY:DESTINATION": 1,
		}
		if !maps.Equal(got, want) {
			t.Errorf("entry directions = %v, want %v", got, want)
		}
	})

	t.Run("rolls back the batch when an event is invalid", func(t *testing.T) {
		w, path := newTestWallet(t)
		amount, err := decimal.Parse("10.00")
		if err != nil {
			t.Fatal(err)
		}
		events := []models.TransactionEvent{
			testEvent(t, models.EventIncome, "100.00", "salary"),
			testEvent(t, models.EventExpense, "20.00", "food"),
			{
				OccuredAt: time.Now().UTC(),
				Amount:    amount,
				Desc:      "bad event",
				Category:  models.AccountCategory("unknown"),
				Type:      models.EventType("bogus"),
			},
		}

		err = w.Apply(events)
		if !errors.Is(err, wallet.ErrorInvalidEvent) {
			t.Fatalf("error = %v, want wallet.ErrorInvalidEvent", err)
		}

		db := openDB(t, path)
		wantSummaries := map[string]AccountSummary{
			"wallet": {},
			"salary": {},
			"food":   {},
		}
		if got := accountSummaries(t, db); !maps.Equal(got, wantSummaries) {
			t.Errorf("account summaries = %v, want %v", got, wantSummaries)
		}
	})
}

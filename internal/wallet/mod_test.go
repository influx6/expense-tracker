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

type AccountSummary struct {
	Transactions uint
	Entries      uint
	Received     decimal.Decimal
	Given        decimal.Decimal
	Balance      decimal.Decimal
}

type accountAccumulator struct {
	transactions map[string]struct{}
	entries      uint
	received     decimal.Decimal
	given        decimal.Decimal
}

func accountSummaries(t *testing.T, db *sqlx.DB) map[string]AccountSummary {
	t.Helper()

	var accountNames []string
	if err := db.Select(&accountNames, `SELECT account_name FROM accounts`); err != nil {
		t.Fatal(err)
	}

	accs := make(map[string]*accountAccumulator, len(accountNames))
	for _, name := range accountNames {
		accs[name] = &accountAccumulator{transactions: make(map[string]struct{})}
	}

	var rows []struct {
		AccountName   string `db:"account_name"`
		TransactionID string `db:"transaction_id"`
		Direction     string `db:"direction"`
		Amount        string `db:"amount"`
	}
	if err := db.Select(&rows, `
		SELECT a.account_name, e.transaction_id, e.direction, e.amount
		FROM entries e
		JOIN accounts a ON a.account_id = e.account_id`); err != nil {
		t.Fatal(err)
	}

	for _, row := range rows {
		acc := accs[row.AccountName]
		acc.entries++
		acc.transactions[row.TransactionID] = struct{}{}

		amount, err := decimal.Parse(row.Amount)
		if err != nil {
			t.Fatal(err)
		}
		if row.Direction == string(models.DestinationDirection) {
			acc.received, err = acc.received.Add(amount)
		} else {
			acc.given, err = acc.given.Add(amount)
		}
		if err != nil {
			t.Fatal(err)
		}
	}

	summaries := make(map[string]AccountSummary, len(accs))
	for name, acc := range accs {
		balance, err := acc.received.Sub(acc.given)
		if err != nil {
			t.Fatal(err)
		}
		summaries[name] = AccountSummary{
			Transactions: uint(len(acc.transactions)),
			Entries:      acc.entries,
			Received:     acc.received,
			Given:        acc.given,
			Balance:      balance,
		}
	}
	return summaries
}

func summariesEqual(got, want map[string]AccountSummary) bool {
	if len(got) != len(want) {
		return false
	}
	for name, wantSummary := range want {
		gotSummary, ok := got[name]
		if !ok ||
			gotSummary.Transactions != wantSummary.Transactions ||
			gotSummary.Entries != wantSummary.Entries ||
			gotSummary.Received.Cmp(wantSummary.Received) != 0 ||
			gotSummary.Given.Cmp(wantSummary.Given) != 0 ||
			gotSummary.Balance.Cmp(wantSummary.Balance) != 0 {
			return false
		}
	}
	return true
}

func mustDecimal(t *testing.T, s string) decimal.Decimal {
	t.Helper()

	d, err := decimal.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
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
			"wallet": {
				Transactions: 4,
				Entries:      4,
				Received:     mustDecimal(t, "150.00"),
				Given:        mustDecimal(t, "30.00"),
				Balance:      mustDecimal(t, "120.00"),
			},
			"salary": {
				Transactions: 1,
				Entries:      1,
				Given:        mustDecimal(t, "100.00"),
				Balance:      mustDecimal(t, "-100.00"),
			},
			"food": {
				Transactions: 1,
				Entries:      1,
				Received:     mustDecimal(t, "20.00"),
				Balance:      mustDecimal(t, "20.00"),
			},
			"friend_loan": {
				Transactions: 2,
				Entries:      2,
				Received:     mustDecimal(t, "10.00"),
				Given:        mustDecimal(t, "50.00"),
				Balance:      mustDecimal(t, "-40.00"),
			},
		}
		if got := accountSummaries(t, db); !summariesEqual(got, wantSummaries) {
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
		if got := accountSummaries(t, db); !summariesEqual(got, wantSummaries) {
			t.Errorf("account summaries = %v, want %v", got, wantSummaries)
		}
	})
}

package models_test

import (
	"errors"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/govalues/decimal"

	"github.com/influx6/expense-tracker/internal/models"
)

func mustDecimal(t *testing.T, s string) decimal.Decimal {
	t.Helper()
	d, err := decimal.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func validAccount() models.Account {
	return models.Account{
		AccountID:   uuid.New(),
		AccountName: "food",
		AccountType: models.ExpenseAccount,
		UserID:      uuid.New(),
		Currency:    "HKD",
	}
}

func TestAccountValidate(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*models.Account)
	}{
		{name: "valid", mutate: func(*models.Account) {}},
		{name: "missing account id", mutate: func(a *models.Account) { a.AccountID = uuid.Nil() }},
		{name: "missing user id", mutate: func(a *models.Account) { a.UserID = uuid.Nil() }},
		{name: "uppercase name", mutate: func(a *models.Account) { a.AccountName = "Food" }},
		{name: "name with digit", mutate: func(a *models.Account) { a.AccountName = "food2" }},
		{name: "unknown type", mutate: func(a *models.Account) { a.AccountType = models.AccountType("LOAN") }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			account := validAccount()
			tc.mutate(&account)

			err := account.Validate()
			wantErr := tc.name != "valid"
			if (err != nil) != wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, wantErr)
			}
			if wantErr && !errors.Is(err, models.InvalidAccount) {
				t.Errorf("error = %v, want models.InvalidAccount", err)
			}
		})
	}
}

func validTransaction() models.Transaction {
	return models.Transaction{
		TransactionID: uuid.New(),
		UserID:        uuid.New(),
		Description:   "coffee",
		OccuredAt:     time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	}
}

func TestTransactionValidate(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*models.Transaction)
	}{
		{name: "valid", mutate: func(*models.Transaction) {}},
		{name: "missing transaction id", mutate: func(tx *models.Transaction) { tx.TransactionID = uuid.Nil() }},
		{name: "missing user id", mutate: func(tx *models.Transaction) { tx.UserID = uuid.Nil() }},
		{name: "missing occurred at", mutate: func(tx *models.Transaction) { tx.OccuredAt = time.Time{} }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tx := validTransaction()
			tc.mutate(&tx)

			err := tx.Validate()
			wantErr := tc.name != "valid"
			if (err != nil) != wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, wantErr)
			}
			if wantErr && !errors.Is(err, models.InvalidTransaction) {
				t.Errorf("error = %v, want models.InvalidTransaction", err)
			}
		})
	}
}

func validEntries(t *testing.T) []models.Entry {
	t.Helper()
	txID := uuid.New()
	source := models.Entry{
		EntryID:       uuid.New(),
		TransactionID: txID,
		AccountID:     uuid.New(),
		Amount:        mustDecimal(t, "10.00"),
		Direction:     models.SourceDirection,
	}
	destination := models.Entry{
		EntryID:       uuid.New(),
		TransactionID: txID,
		AccountID:     uuid.New(),
		Amount:        mustDecimal(t, "10.00"),
		Direction:     models.DestinationDirection,
	}
	return []models.Entry{source, destination}
}

func TestEntryValidate(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*models.Entry)
	}{
		{name: "valid", mutate: func(*models.Entry) {}},
		{name: "missing entry id", mutate: func(e *models.Entry) { e.EntryID = uuid.Nil() }},
		{name: "missing transaction id", mutate: func(e *models.Entry) { e.TransactionID = uuid.Nil() }},
		{name: "missing account id", mutate: func(e *models.Entry) { e.AccountID = uuid.Nil() }},
		{name: "zero amount", mutate: func(e *models.Entry) { e.Amount = decimal.Decimal{} }},
		{name: "negative amount", mutate: func(e *models.Entry) { e.Amount = mustDecimal(t, "-1") }},
		{name: "unknown direction", mutate: func(e *models.Entry) { e.Direction = "SIDEWAYS" }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			entry := validEntries(t)[0]
			tc.mutate(&entry)

			err := entry.Validate()
			wantErr := tc.name != "valid"
			if (err != nil) != wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, wantErr)
			}
			if wantErr && !errors.Is(err, models.InvalidEntry) {
				t.Errorf("error = %v, want models.InvalidEntry", err)
			}
		})
	}
}

func TestValidateEntries(t *testing.T) {
	tests := []struct {
		name   string
		mutate func([]models.Entry) []models.Entry
		want   error
	}{
		{name: "valid", mutate: func(e []models.Entry) []models.Entry { return e }},
		{
			name: "two source entries",
			mutate: func(e []models.Entry) []models.Entry {
				e[1].Direction = models.SourceDirection
				return e
			},
			want: models.InvalidEntries,
		},
		{
			name: "two destination entries",
			mutate: func(e []models.Entry) []models.Entry {
				e[0].Direction = models.DestinationDirection
				return e
			},
			want: models.InvalidEntries,
		},
		{
			name: "different transactions",
			mutate: func(e []models.Entry) []models.Entry {
				e[1].TransactionID = uuid.New()
				return e
			},
			want: models.InvalidEntries,
		},
		{
			name: "same account on both sides",
			mutate: func(e []models.Entry) []models.Entry {
				e[1].AccountID = e[0].AccountID
				return e
			},
			want: models.InvalidEntries,
		},
		{
			name: "amounts differ",
			mutate: func(e []models.Entry) []models.Entry {
				e[1].Amount = mustDecimal(t, "9.99")
				return e
			},
			want: models.InvalidEntries,
		},
		{
			name: "invalid entry inside",
			mutate: func(e []models.Entry) []models.Entry {
				e[0].Amount = decimal.Decimal{}
				return e
			},
			want: models.InvalidEntry,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			entries := tc.mutate(validEntries(t))
			source, destination := entries[0], entries[1]
			err := models.ValidateTransactionEntries(source, destination)
			if tc.want == nil {
				if err != nil {
					t.Fatalf("ValidateEntries() error = %v", err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Errorf("error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestEventToAccountType(t *testing.T) {
	tests := []struct {
		event models.EventType
		want  models.AccountType
	}{
		{event: models.EventIncome, want: models.IncomeAccount},
		{event: models.EventExpense, want: models.ExpenseAccount},
		{event: models.EventTransferIn, want: models.LiabilityAccount},
		{event: models.EventTransferOut, want: models.LiabilityAccount},
	}

	for _, tc := range tests {
		got, err := models.EventToAccountType(tc.event)
		if err != nil {
			t.Errorf("EventToAccountType(%q): %v", tc.event, err)
			continue
		}
		if got != tc.want {
			t.Errorf("EventToAccountType(%q) = %q, want %q", tc.event, got, tc.want)
		}
	}

	if _, err := models.EventToAccountType("unknown"); !errors.Is(err, models.InvalidEventType) {
		t.Errorf("unknown event error = %v, want models.InvalidEventType", err)
	}
}

func TestModelError(t *testing.T) {
	sentinel := models.InvalidEntry
	if got := sentinel.Error(); !strings.Contains(got, "INVALID_ENTRY") {
		t.Errorf("Error() = %q, want it to contain the code", got)
	}
	if sentinel.Unwrap() != nil {
		t.Errorf("sentinel has a wrapped error")
	}
	if sentinel.Is(errors.New("other")) {
		t.Errorf("Is matched a non-ModelError")
	}

	err := (models.Account{}).Validate()
	if !errors.Is(err, models.InvalidAccount) {
		t.Errorf("error = %v, want models.InvalidAccount", err)
	}
	if errors.Unwrap(err) == nil {
		t.Errorf("validation error has no wrapped detail")
	}
	if !strings.Contains(err.Error(), "INVALID_ACCOUNT") {
		t.Errorf("Error() = %q, want it to contain the code", err.Error())
	}
}

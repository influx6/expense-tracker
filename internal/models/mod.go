package models

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"uuid"

	"github.com/govalues/decimal"
)

type ModelErrorCode string

const (
	CodeInvalidEventType   ModelErrorCode = "INVALID_EVENT_TYPE"
	CodeInvalidAccount     ModelErrorCode = "INVALID_ACCOUNT"
	CodeInvalidTransaction ModelErrorCode = "INVALID_TRANSACTION"
	CodeInvalidEntry       ModelErrorCode = "INVALID_ENTRY"
	CodeInvalidEntries     ModelErrorCode = "INVALID_ENTRIES"
)

var (
	InvalidEventType   = ModelError{Code: CodeInvalidEventType, Label: "Unknown EventType"}
	InvalidAccount     = ModelError{Code: CodeInvalidAccount, Label: "Invalid account"}
	InvalidTransaction = ModelError{Code: CodeInvalidTransaction, Label: "Invalid transaction"}
	InvalidEntry       = ModelError{Code: CodeInvalidEntry, Label: "Invalid entry"}
	InvalidEntries     = ModelError{Code: CodeInvalidEntries, Label: "Invalid transaction entries"}
)

type ModelError struct {
	Code    ModelErrorCode
	Label   string
	wrapped error
}

func (e ModelError) Is(target error) bool {
	if other, ok := target.(ModelError); ok {
		return other.Code == e.Code
	}
	return false
}

func (e ModelError) Unwrap() error {
	return e.wrapped
}

func (e ModelError) withErr(err error) ModelError {
	e.wrapped = err
	return e
}

func (e ModelError) Error() string {
	if e.wrapped != nil {
		return fmt.Sprintf("ModelError(%s): %s: %v", e.Code, e.Label, e.wrapped)
	}
	return fmt.Sprintf("ModelError(%s): %s", e.Code, e.Label)
}

type EventType string

const (
	EventIncome      EventType = "income"
	EventExpense     EventType = "expense"
	EventTransferIn  EventType = "transfer_in"
	EventTransferOut EventType = "transfer_out"
)

// AccountNamePattern matches valid account names: lowercase letters and underscores.
var AccountNamePattern = regexp.MustCompile(`^[a-z_]+$`)

type AccountCategory string

func (t AccountCategory) Validate() error {
	if !AccountNamePattern.MatchString(string(t)) {
		return InvalidAccount.withErr(fmt.Errorf("account_name %q must be lowercase letters and underscores", string(t)))
	}
	return nil
}

// ToAccountCategory is the central constructor for an AccountCategory. It
// lowercases the name, replaces spaces with underscores, and validates it.
func ToAccountCategory(raw string) (AccountCategory, error) {
	name := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(raw)), " ", "_")
	if name == "" {
		return "", InvalidAccount.withErr(fmt.Errorf("missing category"))
	}
	category := AccountCategory(name)
	if err := category.Validate(); err != nil {
		return "", err
	}
	return category, nil
}

// TransactionEvent is the clean domain value object passed into Wallet.Apply.
type TransactionEvent struct {
	OccuredAt time.Time
	Amount    decimal.Decimal
	Desc      string
	Category  AccountCategory // normalized: lowercase + _ only
	Type      EventType
}

type AccountType string

const (
	AssetAccount     AccountType = "ASSET"
	LiabilityAccount AccountType = "LIABILITY"
	IncomeAccount    AccountType = "INCOME"
	ExpenseAccount   AccountType = "EXPENSE"
)

func EventToAccountType(event EventType) (AccountType, error) {
	switch event {
	case EventIncome:
		return IncomeAccount, nil
	case EventTransferIn, EventTransferOut:
		return LiabilityAccount, nil
	case EventExpense:
		return ExpenseAccount, nil
	default:
		return "", InvalidEventType
	}
}

type Direction string

const (
	SourceDirection      Direction = "SOURCE"
	DestinationDirection Direction = "DESTINATION"
)

type Account struct {
	AccountID       uuid.UUID
	AccountCategory AccountCategory
	AccountType     AccountType
	UserID          uuid.UUID
	Currency        string
	CreatedAt       time.Time
}

func (a Account) Validate() error {
	if a.AccountID == uuid.Nil() {
		return InvalidAccount.withErr(fmt.Errorf("missing account_id"))
	}
	if a.UserID == uuid.Nil() {
		return InvalidAccount.withErr(fmt.Errorf("missing user_id"))
	}
	if err := a.AccountCategory.Validate(); err != nil {
		return err
	}
	switch a.AccountType {
	case AssetAccount, LiabilityAccount, IncomeAccount, ExpenseAccount:
	default:
		return InvalidAccount.withErr(fmt.Errorf("unknown account_type %q", a.AccountType))
	}
	return nil
}

type Transaction struct {
	TransactionID uuid.UUID
	UserID        uuid.UUID
	Description   string
	OccuredAt     time.Time
	CreatedAt     time.Time
}

func (t Transaction) Validate() error {
	if t.TransactionID == uuid.Nil() {
		return InvalidTransaction.withErr(fmt.Errorf("missing transaction_id"))
	}
	if t.UserID == uuid.Nil() {
		return InvalidTransaction.withErr(fmt.Errorf("missing user_id"))
	}
	if t.OccuredAt.IsZero() {
		return InvalidTransaction.withErr(fmt.Errorf("missing occured_at"))
	}
	return nil
}

type Entry struct {
	EntryID       uuid.UUID
	TransactionID uuid.UUID
	AccountID     uuid.UUID
	Amount        decimal.Decimal
	Direction     Direction
	CreatedAt     time.Time
}

func (e Entry) Validate() error {
	if e.EntryID == uuid.Nil() {
		return InvalidEntry.withErr(fmt.Errorf("missing entry_id"))
	}
	if e.TransactionID == uuid.Nil() {
		return InvalidEntry.withErr(fmt.Errorf("missing transaction_id"))
	}
	if e.AccountID == uuid.Nil() {
		return InvalidEntry.withErr(fmt.Errorf("missing account_id"))
	}
	if !e.Amount.IsPos() {
		return InvalidEntry.withErr(fmt.Errorf("amount must be strictly positive, got %s", e.Amount))
	}
	switch e.Direction {
	case SourceDirection, DestinationDirection:
	default:
		return InvalidEntry.withErr(fmt.Errorf("unknown direction %q", e.Direction))
	}
	return nil
}

// ValidateTransactionEntries checks the double-entry laws: a transaction has exactly two
// entries, one SOURCE and one DESTINATION, with equal amounts.
func ValidateTransactionEntries(source Entry, destination Entry) error {
	if sourceValidationErr := source.Validate(); sourceValidationErr != nil {
		return sourceValidationErr
	}
	if destValidationErr := destination.Validate(); destValidationErr != nil {
		return destValidationErr
	}
	if source.Direction != SourceDirection {
		return InvalidEntries.withErr(fmt.Errorf("source Entry as wrong direction: %q", source.Direction))
	}
	if destination.Direction != DestinationDirection {
		return InvalidEntries.withErr(fmt.Errorf("destination Entry as wrong direction: %q", destination.Direction))
	}
	if source.TransactionID != destination.TransactionID {
		return InvalidEntries.withErr(fmt.Errorf("entries belong to different transactions"))
	}
	if source.AccountID == destination.AccountID {
		return InvalidEntries.withErr(fmt.Errorf("source and destination must use different accounts"))
	}
	if source.Amount.Cmp(destination.Amount) != 0 {
		return InvalidEntries.withErr(fmt.Errorf("entry amounts differ: %s != %s", source.Amount, destination.Amount))
	}
	return nil
}

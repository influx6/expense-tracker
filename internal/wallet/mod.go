package wallet

import (
	"fmt"
	"time"
	"uuid"

	"github.com/govalues/decimal"
	"github.com/influx6/expense-tracker/internal/models"
	"github.com/influx6/expense-tracker/internal/store"
	"github.com/jmoiron/sqlx"
)

const RESERVED_WALLET_ACCOUNT = "WALLET"

type WalletErrorCode string

const (
	CodeInvalidEvent   WalletErrorCode = "INVALID_EVENT"
	CodeAccountError   WalletErrorCode = "ACCOUNT_ERROR"
	CodeInvalidEntries WalletErrorCode = "INVALID_ENTRIES"
)

type WalletError struct {
	Code       WalletErrorCode
	Label      string
	IsTerminal bool
	wrapped    error
}

func (e WalletError) Is(target error) bool {
	if other, ok := target.(WalletError); ok {
		return other.Code == e.Code
	}
	return false
}

func (e WalletError) Unwrap() error {
	return e.wrapped
}

func (e WalletError) withErr(err error) WalletError {
	e.wrapped = err
	return e
}

func (e WalletError) Error() string {
	if e.wrapped != nil {
		return fmt.Sprintf("WalletError(%s): %s: %v", e.Code, e.Label, e.wrapped)
	}
	return fmt.Sprintf("WalletError(%s): %s", e.Code, e.Label)
}

var _ error = WalletError{}

var (
	ErrorInvalidEvent   = WalletError{Code: CodeInvalidEvent, Label: "unsupported transaction event"}
	ErrorAccountError   = WalletError{Code: CodeAccountError, Label: "failed to prepare account"}
	ErrorInvalidEntries = WalletError{Code: CodeInvalidEntries, Label: "invalid transaction entries", IsTerminal: true}
)

type Wallet struct {
	userId       uuid.UUID
	currency     string
	AssetAccount *models.Account
	ExpenseStore *store.ExpenseStore
}

func NewWallet(es *store.ExpenseStore, currency string, userId uuid.UUID) (*Wallet, error) {
	assetAccount, getAccountErr := es.GetOrCreateAccount(
		RESERVED_WALLET_ACCOUNT,
		models.AssetAccount,
		userId,
	)
	if getAccountErr != nil {
		return nil, ErrorAccountError.withErr(getAccountErr)
	}

	return &Wallet{userId: userId, currency: currency, AssetAccount: &assetAccount, ExpenseStore: es}, nil
}

func (w *Wallet) Apply(events []models.TransactionEvent) error {
	transactions := make([]models.Transaction, 0, len(events))
	entries := make([]models.Entry, 0, len(events)*2)

	for _, event := range events {
		change, err := w.transactionEventToWalletRecord(event)
		if err != nil {
			return err
		}

		transactions = append(transactions, models.Transaction{
			UserID:        w.userId,
			TransactionID: change.TransactionId,
			Description:   change.Desc,
			OccuredAt:     change.OccuredAt,
			CreatedAt:     time.Now().UTC(),
		})

		transactionEntries, tranEntriesErr := change.intoEntries()
		if tranEntriesErr != nil {
			return tranEntriesErr
		}

		entries = append(entries, transactionEntries...)
	}

	return w.ExpenseStore.InsertAllWith(transactions, entries, func(tx *sqlx.Tx) error {
		// we perform double accounting validation here that
		// all SUM(ASSET.entries) + SUM(EXPENSE.entries) + SUM(LIABILTY.entries) + SUM(INCOME.entries) == 0
		return nil
	})
}

type walletChangeRequest struct {
	Desc          string
	UserId        uuid.UUID
	TransactionId uuid.UUID
	OccuredAt     time.Time
	Amount        decimal.Decimal
	SrcAccount    models.Account
	DestAccount   models.Account
}

func (t *walletChangeRequest) intoEntries() ([]models.Entry, error) {
	sourceEntry := models.Entry{
		EntryID:       uuid.NewV7(),
		TransactionID: t.TransactionId,
		AccountID:     t.SrcAccount.AccountID,
		Amount:        t.Amount,
		Direction:     models.SourceDirection,
		CreatedAt:     time.Now(),
	}
	destinationEntry := models.Entry{
		EntryID:       uuid.NewV7(),
		TransactionID: t.TransactionId,
		AccountID:     t.DestAccount.AccountID,
		Amount:        t.Amount,
		Direction:     models.DestinationDirection,
		CreatedAt:     time.Now(),
	}

	if err := models.ValidateTransactionEntries(sourceEntry, destinationEntry); err != nil {
		return nil, ErrorInvalidEntries.withErr(err)
	}

	return []models.Entry{sourceEntry, destinationEntry}, nil
}

// transactionEventToWalletRecord normalizes every incoming transaction event
// producing the right set of transaction request that is to be recorded.
func (w *Wallet) transactionEventToWalletRecord(event models.TransactionEvent) (walletChangeRequest, error) {
	changeRequest := walletChangeRequest{
		Desc:          event.Desc,
		UserId:        w.userId,
		TransactionId: uuid.NewV7(),
		OccuredAt:     event.OccuredAt.UTC(),
		Amount:        event.Amount,
	}

	switch event.Type {
	case models.EventExpense:
		destAccount, destAccountErr := w.ExpenseStore.GetOrCreateAccount(string(event.Category), models.ExpenseAccount, w.userId)
		if destAccountErr != nil {
			return walletChangeRequest{}, ErrorAccountError.withErr(destAccountErr)
		}

		changeRequest.SrcAccount = *w.AssetAccount
		changeRequest.DestAccount = destAccount
		return changeRequest, nil
	case models.EventIncome:
		incomeAccount, incomeAccountErr := w.ExpenseStore.GetOrCreateAccount(string(event.Category), models.IncomeAccount, w.userId)
		if incomeAccountErr != nil {
			return walletChangeRequest{}, ErrorAccountError.withErr(incomeAccountErr)
		}

		changeRequest.SrcAccount = incomeAccount
		changeRequest.DestAccount = *w.AssetAccount
		return changeRequest, nil
	case models.EventTransferIn:
		// transfers in increases assets but they are liabilities as well
		// that will need to be settled, but they do increase asset value.
		liabilityAccount, liabilityAccountErr := w.ExpenseStore.GetOrCreateAccount(string(event.Category), models.LiabilityAccount, w.userId)
		if liabilityAccountErr != nil {
			return walletChangeRequest{}, ErrorAccountError.withErr(liabilityAccountErr)
		}

		changeRequest.SrcAccount = liabilityAccount
		changeRequest.DestAccount = *w.AssetAccount
		return changeRequest, nil
	case models.EventTransferOut:
		// transfers out decreases asset value but also decreases your libability.
		liabilityAccount, liabilityAccountErr := w.ExpenseStore.GetOrCreateAccount(string(event.Category), models.LiabilityAccount, w.userId)
		if liabilityAccountErr != nil {
			return walletChangeRequest{}, ErrorAccountError.withErr(liabilityAccountErr)
		}

		changeRequest.SrcAccount = *w.AssetAccount
		changeRequest.DestAccount = liabilityAccount
		return changeRequest, nil
	default:
		return walletChangeRequest{}, ErrorInvalidEvent.withErr(fmt.Errorf("%q", event.Type))
	}
}

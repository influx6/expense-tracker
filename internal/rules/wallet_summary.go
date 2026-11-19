package rules

import (
	"context"

	"github.com/govalues/decimal"

	"github.com/influx6/expense-tracker/internal/models"
	"github.com/influx6/expense-tracker/internal/wallet"
)

type AccountSummary struct {
	Account models.Account
	Debits  decimal.Decimal
	Credits decimal.Decimal
	Balance decimal.Decimal
}

type TypeSummary struct {
	AccountType models.AccountType
	Debits      decimal.Decimal
	Credits     decimal.Decimal
	Balance     decimal.Decimal
}

type WalletSummary struct {
	Totals     []TypeSummary
	Categories []AccountSummary
}

var _ wallet.ServiceRule[*WalletSummary] = WalletSummaryRule{}

type WalletSummaryRule struct {
	Year  int
	Month int
}

func (r WalletSummaryRule) Name() string {
	return "wallet-summary"
}

func (r WalletSummaryRule) Analyse(ctx context.Context, wl *wallet.Wallet) (*WalletSummary, error) {
	totals := map[models.AccountType]*TypeSummary{
		models.AssetAccount:     {AccountType: models.AssetAccount},
		models.LiabilityAccount: {AccountType: models.LiabilityAccount},
		models.IncomeAccount:    {AccountType: models.IncomeAccount},
		models.ExpenseAccount:   {AccountType: models.ExpenseAccount},
	}
	var categories []AccountSummary

	for account, err := range wl.ExpenseStore.AllAccountsIn(ctx, wl.UserId, wl.Currency, r.Year, r.Month) {
		if err != nil {
			return nil, err
		}

		accountSummary := AccountSummary{Account: account}
		for entry, err := range wl.ExpenseStore.AllEntriesByAccountIn(ctx, account, r.Year, r.Month) {
			if err != nil {
				return nil, err
			}

			if entry.Direction == models.DestinationDirection {
				accountSummary.Debits, err = accountSummary.Debits.Add(entry.Amount)
			} else {
				accountSummary.Credits, err = accountSummary.Credits.Add(entry.Amount)
			}
			if err != nil {
				return nil, err
			}
		}

		accountSummary.Balance, err = accountSummary.Debits.Sub(accountSummary.Credits)
		if err != nil {
			return nil, err
		}

		if total, ok := totals[account.AccountType]; ok {
			total.Debits, err = total.Debits.Add(accountSummary.Debits)
			if err != nil {
				return nil, err
			}
			total.Credits, err = total.Credits.Add(accountSummary.Credits)
			if err != nil {
				return nil, err
			}
			total.Balance, err = total.Balance.Add(accountSummary.Balance)
			if err != nil {
				return nil, err
			}
		}

		if account.AccountType != models.AssetAccount {
			categories = append(categories, accountSummary)
		}
	}

	return &WalletSummary{
		Totals: []TypeSummary{
			*totals[models.AssetAccount],
			*totals[models.LiabilityAccount],
			*totals[models.IncomeAccount],
			*totals[models.ExpenseAccount],
		},
		Categories: categories,
	}, nil
}

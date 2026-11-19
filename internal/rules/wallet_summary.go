package rules

import (
	"context"
	"fmt"
	"io"
	"sort"

	"github.com/govalues/decimal"

	"github.com/influx6/expense-tracker/internal/humans"
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
	Currency   string
	Totals     []TypeSummary
	Categories []AccountSummary
}

var summaryTypeOrder = map[models.AccountType]int{
	models.AssetAccount:     0,
	models.LiabilityAccount: 1,
	models.IncomeAccount:    2,
	models.ExpenseAccount:   3,
}

func (s *WalletSummary) Print(out io.Writer) {
	sort.Slice(s.Categories, func(i, j int) bool {
		left, right := s.Categories[i].Account, s.Categories[j].Account
		if leftRank, rightRank := summaryTypeOrder[left.AccountType], summaryTypeOrder[right.AccountType]; leftRank != rightRank {
			return leftRank < rightRank
		}
		return left.AccountCategory < right.AccountCategory
	})

	fmt.Fprintf(out, "Currency: %s\n\n", s.Currency)

	fmt.Fprintln(out, "Totals:")
	fmt.Fprintf(out, "  %-12s %20s %20s %20s\n", "Type", "Debits (RECEIVED)", "Credits (SENT)", "Balance")
	for _, total := range s.Totals {
		fmt.Fprintf(out, "  %-12s %20s %20s %20s\n",
			humans.AccountType(total.AccountType),
			humans.Amount(total.Debits),
			humans.Amount(total.Credits),
			humans.Amount(total.Balance),
		)
	}

	fmt.Fprintln(out, "\nCategories:")
	fmt.Fprintf(out, "  %-12s %-18s %20s %20s %20s\n", "Type", "Category", "Debits (RECEIVED)", "Credits (SENT)", "Balance")
	for _, category := range s.Categories {
		fmt.Fprintf(out, "  %-12s %-18s %20s %20s %20s\n",
			humans.AccountType(category.Account.AccountType),
			humans.Category(category.Account.AccountCategory),
			humans.Amount(category.Debits),
			humans.Amount(category.Credits),
			humans.Amount(category.Balance),
		)
	}
}

var _ wallet.ServiceRule[wallet.PrintOutput] = WalletSummaryRule{}

type WalletSummaryRule struct {
	Year  int
	Month int
}

func (r WalletSummaryRule) Name() string {
	return "wallet-summary"
}

func (r WalletSummaryRule) Analyse(ctx context.Context, wl *wallet.Wallet) (wallet.PrintOutput, error) {
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
		Currency: wl.Currency,
		Totals: []TypeSummary{
			*totals[models.AssetAccount],
			*totals[models.LiabilityAccount],
			*totals[models.IncomeAccount],
			*totals[models.ExpenseAccount],
		},
		Categories: categories,
	}, nil
}

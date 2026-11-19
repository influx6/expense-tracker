package rules

import (
	"context"
	"sort"

	"github.com/govalues/decimal"

	"github.com/influx6/expense-tracker/internal/models"
	"github.com/influx6/expense-tracker/internal/wallet"
)

type LiabilityPeak struct {
	Year   int
	Month  int
	Amount decimal.Decimal
}

type LiabilityPeaks struct {
	Peaks []LiabilityPeak
}

var _ wallet.ServiceRule[*LiabilityPeaks] = LiabilityPeakRule{}

type LiabilityPeakRule struct{}

func (LiabilityPeakRule) Name() string {
	return "liability-peaks"
}

func (LiabilityPeakRule) Analyse(ctx context.Context, wl *wallet.Wallet) (*LiabilityPeaks, error) {
	netByYearMonth := make(map[int]map[int]decimal.Decimal)

	for account, err := range wl.ExpenseStore.AllAccounts(ctx, wl.UserId, wl.Currency) {
		if err != nil {
			return nil, err
		}
		if account.AccountType != models.LiabilityAccount {
			continue
		}

		for entry, err := range wl.ExpenseStore.AllEntriesByAccount(ctx, account) {
			if err != nil {
				return nil, err
			}

			year, month := entry.OccuredAt.Year(), int(entry.OccuredAt.Month())
			if netByYearMonth[year] == nil {
				netByYearMonth[year] = make(map[int]decimal.Decimal)
			}

			delta := entry.Amount
			if entry.Direction == models.DestinationDirection {
				delta = delta.Neg()
			}
			netByYearMonth[year][month], err = netByYearMonth[year][month].Add(delta)
			if err != nil {
				return nil, err
			}
		}
	}

	years := make([]int, 0, len(netByYearMonth))
	for year := range netByYearMonth {
		years = append(years, year)
	}
	sort.Ints(years)

	peaks := make([]LiabilityPeak, 0, len(years))
	for _, year := range years {
		peakMonth := 0
		var peakAmount decimal.Decimal
		for month, amount := range netByYearMonth[year] {
			if peakMonth == 0 || amount.Cmp(peakAmount) > 0 || (amount.Cmp(peakAmount) == 0 && month < peakMonth) {
				peakMonth, peakAmount = month, amount
			}
		}
		if peakMonth != 0 {
			peaks = append(peaks, LiabilityPeak{Year: year, Month: peakMonth, Amount: peakAmount})
		}
	}

	return &LiabilityPeaks{Peaks: peaks}, nil
}

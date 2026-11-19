package rules

import (
	"context"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/govalues/decimal"

	"github.com/influx6/expense-tracker/internal/humans"
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

func (p *LiabilityPeaks) Print(out io.Writer) {
	if len(p.Peaks) == 0 {
		fmt.Fprintln(out, "Liability peaks: none")
		return
	}

	fmt.Fprintln(out, "Liability peaks:")
	fmt.Fprintf(out, "  %-6s %-10s %20s\n", "Year", "Month", "Net Liability Taken On")
	for _, peak := range p.Peaks {
		fmt.Fprintf(out, "  %-6d %-10s %20s\n",
			peak.Year,
			time.Month(peak.Month).String(),
			humans.Amount(peak.Amount),
		)
	}
}

var _ wallet.ServiceRule[wallet.PrintOutput] = LiabilityPeakRule{}

type LiabilityPeakRule struct{}

func (LiabilityPeakRule) Name() string {
	return "liability-peaks"
}

func (LiabilityPeakRule) Analyse(ctx context.Context, wl *wallet.Wallet) (wallet.PrintOutput, error) {
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

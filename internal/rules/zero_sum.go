package rules

import (
	"context"
	"errors"
	"fmt"

	"github.com/influx6/expense-tracker/internal/wallet"
)

var ErrUnbalanced = errors.New("zerosum: accounts do not sum to zero")

var _ wallet.ServiceRule[wallet.PrintOutput] = ZeroSumBalance{}

// ZeroSumBalance enforces the desired effects that
// an user when summed across all it's accounts
// will result in a total value of zero.
// All credits and debits are properly recorded via double
// accounting and correctly accounts for the movements of value between them.
type ZeroSumBalance struct{}

func (t ZeroSumBalance) Name() string {
	return "zerosum"
}

func (t ZeroSumBalance) Analyse(ctx context.Context, wl *wallet.Wallet) (wallet.PrintOutput, error) {
	total, err := wl.ExpenseStore.ZeroSum(ctx, wl.UserId, wl.Currency)
	if err != nil {
		return nil, err
	}

	if !total.IsZero() {
		return nil, fmt.Errorf("%w: got %s", ErrUnbalanced, total)
	}
	return nil, nil
}

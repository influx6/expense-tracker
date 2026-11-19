package cmd

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/influx6/expense-tracker/internal/humans"
	"github.com/influx6/expense-tracker/internal/rules"
	"github.com/influx6/expense-tracker/internal/store"
	"github.com/influx6/expense-tracker/internal/wallet"
)

type rule interface {
	Name() string
	Analyse(ctx context.Context, wl *wallet.Wallet) (any, error)
}

type walletSummaryRule struct{ rules.WalletSummaryRule }

func (r walletSummaryRule) Analyse(ctx context.Context, wl *wallet.Wallet) (any, error) {
	return r.WalletSummaryRule.Analyse(ctx, wl)
}

type liabilityPeakRule struct{ rules.LiabilityPeakRule }

func (r liabilityPeakRule) Analyse(ctx context.Context, wl *wallet.Wallet) (any, error) {
	return r.LiabilityPeakRule.Analyse(ctx, wl)
}

var ruleRunners = []rule{
	rules.ZeroSumBalance{},
	walletSummaryRule{},
	liabilityPeakRule{},
}

var rulesCmd = &cobra.Command{
	Use:   "rules [rule-name|list]",
	Short: "Run a rule or list the available rules",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 || args[0] == "list" {
			for _, name := range availableRules() {
				fmt.Fprintln(cmd.OutOrStdout(), name)
			}
			return nil
		}

		name := args[0]
		selected, ok := findRule(name)
		if !ok {
			return fmt.Errorf("unknown rule %q, available: %s", name, strings.Join(availableRules(), ", "))
		}

		dbPath, err := cmd.Flags().GetString("db")
		if err != nil {
			return err
		}

		ctx := cmd.Context()
		expenseStore, err := store.SqliteExpenseStore(ctx, dbPath, 5000)
		if err != nil {
			return err
		}

		expenseWallet, err := wallet.NewWallet(ctx, expenseStore, defaultCurrency, defaultUserID)
		if err != nil {
			return err
		}

		result, err := selected.Analyse(ctx, expenseWallet)
		if err != nil {
			return err
		}

		out := cmd.OutOrStdout()
		switch value := result.(type) {
		case nil:
			fmt.Fprintf(out, "%s: ok\n", name)
		case *rules.WalletSummary:
			printWalletSummary(cmd, expenseWallet.Currency, value)
		case *rules.LiabilityPeaks:
			printLiabilityPeaks(cmd, value)
		default:
			fmt.Fprintln(out, value)
		}
		return nil
	},
}

func printLiabilityPeaks(cmd *cobra.Command, peaks *rules.LiabilityPeaks) {
	out := cmd.OutOrStdout()

	if len(peaks.Peaks) == 0 {
		fmt.Fprintln(out, "Liability peaks: none")
		return
	}

	fmt.Fprintln(out, "Liability peaks:")
	fmt.Fprintf(out, "  %-6s %-10s %20s\n", "Year", "Month", "Net Liability Taken On")
	for _, peak := range peaks.Peaks {
		fmt.Fprintf(out, "  %-6d %-10s %20s\n",
			peak.Year,
			time.Month(peak.Month).String(),
			humans.Amount(peak.Amount),
		)
	}
}

func findRule(name string) (rule, bool) {
	for _, available := range ruleRunners {
		if available.Name() == name {
			return available, true
		}
	}
	return nil, false
}

func availableRules() []string {
	names := make([]string, 0, len(ruleRunners))
	for _, available := range ruleRunners {
		names = append(names, available.Name())
	}
	sort.Strings(names)
	return names
}

func init() {
	rootCmd.AddCommand(rulesCmd)

	rulesCmd.Flags().String("db", "expense-tracker.db", "SQLite database path")
}

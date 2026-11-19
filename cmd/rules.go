package cmd

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/influx6/expense-tracker/internal/rules"
	"github.com/influx6/expense-tracker/internal/store"
	"github.com/influx6/expense-tracker/internal/wallet"
)

type ruleRunner func(ctx context.Context, wl *wallet.Wallet) (any, error)

var ruleRunners = map[string]ruleRunner{
	"zerosum": func(ctx context.Context, wl *wallet.Wallet) (any, error) {
		return (rules.ZeroSumBalance{}).Analyse(ctx, wl)
	},
	"wallet-summary": func(ctx context.Context, wl *wallet.Wallet) (any, error) {
		return (rules.WalletSummaryRule{}).Analyse(ctx, wl)
	},
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
		run, ok := ruleRunners[name]
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

		result, err := run(ctx, expenseWallet)
		if err != nil {
			return err
		}

		out := cmd.OutOrStdout()
		switch value := result.(type) {
		case nil:
			fmt.Fprintf(out, "%s: ok\n", name)
		case *rules.WalletSummary:
			printWalletSummary(cmd, expenseWallet.Currency, value)
		default:
			fmt.Fprintln(out, value)
		}
		return nil
	},
}

func availableRules() []string {
	names := make([]string, 0, len(ruleRunners))
	for name := range ruleRunners {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func init() {
	rootCmd.AddCommand(rulesCmd)

	rulesCmd.Flags().String("db", "expense-tracker.db", "SQLite database path")
}

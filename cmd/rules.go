package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/influx6/expense-tracker/internal/rules"
	"github.com/influx6/expense-tracker/internal/store"
	"github.com/influx6/expense-tracker/internal/wallet"
)

var ruleRunners = []wallet.ServiceRule[wallet.PrintOutput]{
	rules.ZeroSumBalance{},
	rules.WalletSummaryRule{},
	rules.LiabilityPeakRule{},
}

var rulesCmd = &cobra.Command{
	Use:   "rules",
	Short: "List or run wallet rules",
}

var rulesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List the available rules",
	RunE: func(cmd *cobra.Command, args []string) error {
		for _, name := range availableRules() {
			fmt.Fprintln(cmd.OutOrStdout(), name)
		}
		return nil
	},
}

var rulesRunCmd = &cobra.Command{
	Use:   "run [rule-name]",
	Short: "Run a rule",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
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
		if result == nil {
			fmt.Fprintf(out, "%s: ok\n", name)
			return nil
		}
		result.Print(out)
		return nil
	},
}

func findRule(name string) (wallet.ServiceRule[wallet.PrintOutput], bool) {
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
	rulesCmd.AddCommand(rulesListCmd, rulesRunCmd)

	rulesRunCmd.Flags().String("db", "expense-tracker.db", "SQLite database path")
}

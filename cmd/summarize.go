package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/influx6/expense-tracker/internal/rules"
	"github.com/influx6/expense-tracker/internal/store"
	"github.com/influx6/expense-tracker/internal/wallet"
)

var summarizeCmd = &cobra.Command{
	Use:   "summarize",
	Short: "Summarize balances by account type and category",
	RunE: func(cmd *cobra.Command, args []string) error {
		dbPath, err := cmd.Flags().GetString("db")
		if err != nil {
			return err
		}

		year, err := cmd.Flags().GetInt("year")
		if err != nil {
			return err
		}
		month, err := cmd.Flags().GetInt("month")
		if err != nil {
			return err
		}
		if year < 0 {
			return fmt.Errorf("--year must be positive")
		}
		if month < 0 || month > 12 {
			return fmt.Errorf("--month must be between 1 and 12")
		}
		if month > 0 && year == 0 {
			return fmt.Errorf("--month requires --year")
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

		summary, err := (rules.WalletSummaryRule{Year: year, Month: month}).Analyse(ctx, expenseWallet)
		if err != nil {
			return err
		}

		summary.Print(cmd.OutOrStdout())
		return nil
	},
}

func init() {
	rootCmd.AddCommand(summarizeCmd)

	summarizeCmd.Flags().String("db", "expense-tracker.db", "SQLite database path")
	summarizeCmd.Flags().Int("year", 0, "narrow the summary to a year")
	summarizeCmd.Flags().Int("month", 0, "narrow the summary to a month (requires --year)")
}

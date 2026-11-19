package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/influx6/expense-tracker/internal/parser"
	"github.com/influx6/expense-tracker/internal/store"
	"github.com/influx6/expense-tracker/internal/wallet"
)

var loadCmd = &cobra.Command{
	Use:   "load",
	Short: "Load transactions from a CSV file",
	RunE: func(cmd *cobra.Command, args []string) error {
		file, err := cmd.Flags().GetString("file")
		if err != nil {
			return err
		}
		invalid, err := cmd.Flags().GetString("invalid")
		if err != nil {
			return err
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

		result, err := parser.ParseCSVFile(file, invalid, 2, []string{wallet.RESERVED_WALLET_ACCOUNT})
		if err != nil {
			return err
		}

		expenseWallet, err := wallet.NewWallet(ctx, expenseStore, defaultCurrency, defaultUserID)
		if err != nil {
			return err
		}

		applyResult, err := expenseWallet.Apply(ctx, result.ValidEvents)
		if err != nil {
			return err
		}

		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "parsed: %d\n", len(result.ValidEvents))
		fmt.Fprintf(out, "loaded: %d\n", applyResult.Transactions)
		fmt.Fprintf(out, "rejected: %d\n", len(result.BadRows))

		if len(result.BadRows) > 0 {
			fmt.Fprintf(out, "invalid rows written to: %s\n", invalid)

			limit := min(5, len(result.BadRows))
			for i, badRow := range result.BadRows[:limit] {
				fmt.Fprintf(out, "  %d. %s: %s\n", i+1, strings.Join(badRow.RawFields, ", "), badRow.Reason)
			}
			if remaining := len(result.BadRows) - limit; remaining > 0 {
				fmt.Fprintf(out, "  ... and %d more\n", remaining)
			}
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(loadCmd)

	loadCmd.Flags().StringP("file", "f", "", "CSV file to load")
	_ = loadCmd.MarkFlagRequired("file")
	loadCmd.Flags().String("invalid", "invalid.csv", "file to write rejected rows into")
	loadCmd.Flags().String("db", "expense-tracker.db", "SQLite database path")
}

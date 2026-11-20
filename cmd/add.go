package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/influx6/expense-tracker/internal/humans"
	"github.com/influx6/expense-tracker/internal/models"
	"github.com/influx6/expense-tracker/internal/parser"
	"github.com/influx6/expense-tracker/internal/store"
	"github.com/influx6/expense-tracker/internal/wallet"
)

var addCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a single transaction entry",
	RunE: func(cmd *cobra.Command, args []string) error {
		fields := make(map[string]string, 5)
		for _, name := range []string{"when", "amount", "desc", "category", "type"} {
			value, err := cmd.Flags().GetString(name)
			if err != nil {
				return err
			}
			fields[name] = value
		}

		event, err := parser.ParseMap(fields, 2, []string{wallet.RESERVED_WALLET_ACCOUNT})
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

		expenseWallet, err := wallet.NewWallet(ctx, expenseStore, defaultCurrency, defaultUserID)
		if err != nil {
			return err
		}

		result, err := expenseWallet.Apply(ctx, []models.TransactionEvent{event})
		if err != nil {
			return err
		}

		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "id: %s\n", event.ID)
		if result.Transactions == 0 {
			fmt.Fprintln(out, "already recorded")
			return nil
		}

		fmt.Fprintf(out, "created: %s\n", event.OccuredAt.UTC().Format(time.RFC3339))
		fmt.Fprintln(out, "entries:")

		entries, err := expenseStore.GetTransaction(ctx, event.ID, defaultUserID, defaultCurrency)
		if err != nil {
			return err
		}
		for _, entryWithAccount := range entries {
			fmt.Fprintf(out, "  Entry [%s] when=%s amount=%s\n",
				humans.Category(entryWithAccount.Account.AccountCategory),
				entryWithAccount.Entry.OccuredAt.UTC().Format(time.RFC3339),
				humans.Amount(entryWithAccount.Entry.Amount),
			)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(addCmd)

	addCmd.Flags().String("when", "", "timestamp of the entry")
	addCmd.Flags().String("amount", "", "positive amount, at most 2 decimal places")
	addCmd.Flags().String("desc", "", "description of the entry")
	addCmd.Flags().String("category", "", "category of the entry")
	addCmd.Flags().String("type", "", "income, expense, transfer_in, or transfer_out")
	addCmd.Flags().String("db", "expense-tracker.db", "SQLite database path")

	_ = addCmd.MarkFlagRequired("when")
	_ = addCmd.MarkFlagRequired("amount")
	_ = addCmd.MarkFlagRequired("category")
	_ = addCmd.MarkFlagRequired("type")
}

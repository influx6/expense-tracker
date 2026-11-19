package cmd

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/influx6/expense-tracker/internal/humans"
	"github.com/influx6/expense-tracker/internal/models"
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

		printWalletSummary(cmd, expenseWallet.Currency, summary)
		return nil
	},
}

var summaryTypeOrder = map[models.AccountType]int{
	models.AssetAccount:     0,
	models.LiabilityAccount: 1,
	models.IncomeAccount:    2,
	models.ExpenseAccount:   3,
}

func printWalletSummary(cmd *cobra.Command, currency string, summary *rules.WalletSummary) {
	out := cmd.OutOrStdout()

	sort.Slice(summary.Categories, func(i, j int) bool {
		left, right := summary.Categories[i].Account, summary.Categories[j].Account
		if leftRank, rightRank := summaryTypeOrder[left.AccountType], summaryTypeOrder[right.AccountType]; leftRank != rightRank {
			return leftRank < rightRank
		}
		return left.AccountCategory < right.AccountCategory
	})

	fmt.Fprintf(out, "Currency: %s\n\n", currency)

	fmt.Fprintln(out, "Totals:")
	fmt.Fprintf(out, "  %-12s %20s %20s %20s\n", "Type", "Debits (RECEIVED)", "Credits (SENT)", "Balance")
	for _, total := range summary.Totals {
		fmt.Fprintf(out, "  %-12s %20s %20s %20s\n",
			humans.AccountType(total.AccountType),
			humans.Amount(total.Debits),
			humans.Amount(total.Credits),
			humans.Amount(total.Balance),
		)
	}

	fmt.Fprintln(out, "\nCategories:")
	fmt.Fprintf(out, "  %-12s %-18s %20s %20s %20s\n", "Type", "Category", "Debits (RECEIVED)", "Credits (SENT)", "Balance")
	for _, category := range summary.Categories {
		fmt.Fprintf(out, "  %-12s %-18s %20s %20s %20s\n",
			humans.AccountType(category.Account.AccountType),
			humans.Category(category.Account.AccountCategory),
			humans.Amount(category.Debits),
			humans.Amount(category.Credits),
			humans.Amount(category.Balance),
		)
	}
}

func init() {
	rootCmd.AddCommand(summarizeCmd)

	summarizeCmd.Flags().String("db", "expense-tracker.db", "SQLite database path")
	summarizeCmd.Flags().Int("year", 0, "narrow the summary to a year")
	summarizeCmd.Flags().Int("month", 0, "narrow the summary to a month (requires --year)")
}

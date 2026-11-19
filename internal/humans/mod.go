package humans

import (
	"strings"

	"github.com/govalues/decimal"

	"github.com/influx6/expense-tracker/internal/models"
)

func AccountType(accountType models.AccountType) string {
	name := strings.ToLower(string(accountType))
	if name == "" {
		return name
	}
	return strings.ToUpper(name[:1]) + name[1:]
}

func Category(category models.AccountCategory) string {
	return strings.ReplaceAll(string(category), "_", " ")
}

func Amount(amount decimal.Decimal) string {
	return groupThousands(amount.Rescale(2).String())
}

func groupThousands(value string) string {
	negative := strings.HasPrefix(value, "-")
	value = strings.TrimPrefix(value, "-")

	integer, fraction, hasFraction := strings.Cut(value, ".")

	var grouped strings.Builder
	for i, digit := range integer {
		if i > 0 && (len(integer)-i)%3 == 0 {
			grouped.WriteByte(',')
		}
		grouped.WriteRune(digit)
	}

	result := grouped.String()
	if hasFraction {
		result += "." + fraction
	}
	if negative {
		result = "-" + result
	}
	return result
}

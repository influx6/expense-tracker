package parser

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/govalues/decimal"
	"github.com/influx6/expense-tracker/internal/models"
)

// ErrorCode identifies the kind of parser failure.
type ErrorCode string

const (
	CodeMissingTimestamp  ErrorCode = "MISSING_TIMESTAMP"
	CodeInvalidTimestamp  ErrorCode = "INVALID_TIMESTAMP"
	CodeMissingAmount     ErrorCode = "MISSING_AMOUNT"
	CodeInvalidAmount     ErrorCode = "INVALID_AMOUNT"
	CodeTooManyDecimals   ErrorCode = "TOO_MANY_DECIMAL_PLACES"
	CodeAmountTooLarge    ErrorCode = "AMOUNT_TOO_LARGE"
	CodeNonPositiveAmount ErrorCode = "NON_POSITIVE_AMOUNT"
	CodeMissingCategory   ErrorCode = "MISSING_CATEGORY"
	CodeInvalidCategory   ErrorCode = "INVALID_CATEGORY"
	CodeReservedCategory  ErrorCode = "RESERVED_CATEGORY"
	CodeInvalidType       ErrorCode = "INVALID_TYPE"
	CodeInvalidHeader     ErrorCode = "INVALID_CSV_HEADER"
	CodeOpenCSV           ErrorCode = "OPEN_CSV"
	CodeReadCSV           ErrorCode = "READ_CSV"
	CodeWriteInvalidCSV   ErrorCode = "WRITE_INVALID_CSV"
)

// ParserError represents a parser failure. Use errors.Is against one of the
// exported Error... values to identify the kind of failure.
type ParserError struct {
	Code       ErrorCode
	Label      string
	IsTerminal bool // true if the error aborts the whole batch
	wrapped    error
}

func (e ParserError) Is(target error) bool {
	if other, ok := target.(ParserError); ok {
		return other.Code == e.Code
	}
	return false
}

func (e ParserError) Unwrap() error {
	return e.wrapped
}

func (e ParserError) withErr(err error) ParserError {
	e.wrapped = err
	return e
}

func (e ParserError) Error() string {
	if e.wrapped != nil {
		return fmt.Sprintf("ParserError(%s): %s: %v", e.Code, e.Label, e.wrapped)
	}
	return fmt.Sprintf("ParserError(%s): %s", e.Code, e.Label)
}

var _ error = ParserError{}

var (
	ErrorMissingTimestamp  = ParserError{Code: CodeMissingTimestamp, Label: "missing timestamp"}
	ErrorInvalidTimestamp  = ParserError{Code: CodeInvalidTimestamp, Label: "invalid UTC timestamp"}
	ErrorMissingAmount     = ParserError{Code: CodeMissingAmount, Label: "missing amount"}
	ErrorInvalidAmount     = ParserError{Code: CodeInvalidAmount, Label: "invalid amount: expected a plain decimal number"}
	ErrorTooManyDecimals   = ParserError{Code: CodeTooManyDecimals, Label: "too many decimal places"}
	ErrorAmountTooLarge    = ParserError{Code: CodeAmountTooLarge, Label: "amount is too large"}
	ErrorNonPositiveAmount = ParserError{Code: CodeNonPositiveAmount, Label: "amount must be strictly positive (> 0)"}
	ErrorMissingCategory   = ParserError{Code: CodeMissingCategory, Label: "missing category"}
	ErrorInvalidCategory   = ParserError{Code: CodeInvalidCategory, Label: "invalid category: only letters and underscores allowed"}
	ErrorReservedCategory  = ParserError{Code: CodeReservedCategory, Label: "category 'wallet' is reserved for the system asset account"}
	ErrorInvalidType       = ParserError{Code: CodeInvalidType, Label: "invalid type: expected income, expense, transfer_in, or transfer_out"}
	ErrorInvalidHeader     = ParserError{Code: CodeInvalidHeader, Label: "invalid CSV header", IsTerminal: true}
	ErrorOpenCSV           = ParserError{Code: CodeOpenCSV, Label: "open csv file", IsTerminal: true}
	ErrorReadCSV           = ParserError{Code: CodeReadCSV, Label: "read csv header", IsTerminal: true}
	ErrorWriteInvalidCSV   = ParserError{Code: CodeWriteInvalidCSV, Label: "write invalid csv", IsTerminal: true}
)

// BadRow captures a rejected CSV record alongside the reason it failed validation.
type BadRow struct {
	RawFields []string
	Reason    string
}

// BatchResult holds valid parsed events and any rejected rows from a CSV file.
type BatchResult struct {
	ValidEvents []models.TransactionEvent
	BadRows     []BadRow
}

// ParseMap validates a single record passed as a map[string]string (e.g. from `cli record` flags).
// Expected keys: "when", "amount", "desc", "category", "type".
func ParseMap(fields map[string]string, currencyDecimalPlaces uint, reservedCategories []string) (models.TransactionEvent, error) {
	return validateAndBuildEvent(
		fields["when"],
		fields["amount"],
		fields["desc"],
		fields["category"],
		fields["type"],
		currencyDecimalPlaces,
		reservedCategories,
	)
}

var (
	requiredColumns = []string{"when", "amount", "desc", "category", "type"}
)

// ParseCSVFile reads a CSV file with columns `when,amount,desc,category,type`,
// separates valid TransactionEvents from invalid rows, and writes bad rows to invalidOutPath.
func ParseCSVFile(csvPath string, invalidOutPath string, currencyDecimalPlaces uint, reservedCategories []string) (BatchResult, error) {
	var result BatchResult

	f, err := os.Open(csvPath)
	if err != nil {
		return result, ErrorOpenCSV.withErr(err)
	}
	defer f.Close()

	reader := csv.NewReader(f)

	header, err := reader.Read()
	if err != nil {
		return result, ErrorReadCSV.withErr(err)
	}

	// after identifying the header, pin it down.
	reader.FieldsPerRecord = len(requiredColumns)

	colIndex, err := mapHeaderColumns(header)
	if err != nil {
		return result, err
	}

	for {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			result.BadRows = append(result.BadRows, BadRow{
				RawFields: record,
				Reason:    fmt.Sprintf("csv read error: %v", err),
			})
			continue
		}

		ev, err := validateAndBuildEvent(
			record[colIndex["when"]],
			record[colIndex["amount"]],
			record[colIndex["desc"]],
			record[colIndex["category"]],
			record[colIndex["type"]],
			currencyDecimalPlaces,
			reservedCategories,
		)
		if err != nil {
			result.BadRows = append(result.BadRows, BadRow{
				RawFields: record,
				Reason:    err.Error(),
			})
			continue
		}

		result.ValidEvents = append(result.ValidEvents, ev)
	}

	if len(result.BadRows) > 0 && invalidOutPath != "" {
		if err := WriteInvalidCSV(invalidOutPath, header, result.BadRows); err != nil {
			return result, err
		}
	}

	return result, nil
}

func validateAndBuildEvent(rawWhen, rawAmount, rawDesc, rawCategory, rawType string, currencyDecimalPlaces uint, reservedCategories []string) (models.TransactionEvent, error) {
	var ev models.TransactionEvent

	// 1. Validate UTC Timestamp
	when, err := parseUTCTimestamp(rawWhen)
	if err != nil {
		return ev, err
	}

	// 2. Validate Decimal Amount (> 0, within the currency's decimal places)
	amount, err := parseAmount(rawAmount, currencyDecimalPlaces)
	if err != nil {
		return ev, err
	}

	// 3. Normalize & Validate Category (lowercase, spaces -> _, letters + _ only)
	category, err := normalizeCategory(rawCategory, reservedCategories)
	if err != nil {
		return ev, err
	}

	// 4. Validate Event Type Enum
	evType, err := parseEventType(rawType)
	if err != nil {
		return ev, err
	}

	ev.OccuredAt = when
	ev.Amount = amount
	ev.Desc = strings.TrimSpace(rawDesc)
	ev.Category = category
	ev.Type = evType
	return ev, nil
}

var (
	timestamp_layout = []string{
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		"2006-01-02",
	}
)

func parseUTCTimestamp(raw string) (time.Time, error) {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return time.Time{}, ErrorMissingTimestamp
	}

	for _, layout := range timestamp_layout {
		if t, err := time.Parse(layout, clean); err == nil {
			return t.UTC(), nil
		}
	}

	return time.Time{}, ErrorInvalidTimestamp.withErr(fmt.Errorf("%q", clean))
}

var amountPattern = regexp.MustCompile(`^([+-]?)(\d+)(?:\.(\d+))?$`)

func parseAmount(raw string, decimalPlaces uint) (decimal.Decimal, error) {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return decimal.Decimal{}, ErrorMissingAmount
	}

	match := amountPattern.FindStringSubmatch(clean)
	if match == nil {
		return decimal.Decimal{}, ErrorInvalidAmount.withErr(fmt.Errorf("%q", clean))
	}

	// 15.050 is accepted as 15.05.
	fraction := strings.TrimRight(match[3], "0")
	if len(fraction) > int(decimalPlaces) {
		return decimal.Decimal{}, ErrorTooManyDecimals.withErr(
			fmt.Errorf("%q must have at most %d decimal places", clean, decimalPlaces),
		)
	}

	// decimal.Parse silently rounds once the integer part goes beyond MaxPrec, so reject any integer part that can not fit
	intDigits := strings.TrimLeft(match[2], "0")
	if maxIntDigits := decimal.MaxPrec - int(decimalPlaces); len(intDigits) > maxIntDigits {
		return decimal.Decimal{}, ErrorAmountTooLarge.withErr(
			fmt.Errorf("%q: integer part has more than %d digits", clean, maxIntDigits),
		)
	}

	// decimal.Parse rejects strings longer than 330 bytes, even when the extra
	// bytes are zeros that do not change the value. Normalize first.
	d, err := decimal.Parse(normalizeAmount(intDigits, fraction))
	if err != nil {
		return decimal.Decimal{}, ErrorInvalidAmount.withErr(err)
	}

	// The sign is not stored. Apply it as x * -1.
	if match[1] == "-" {
		d = d.Neg()
	}

	if !d.IsPos() {
		return decimal.Decimal{}, ErrorNonPositiveAmount.withErr(fmt.Errorf("%q", clean))
	}

	return d, nil
}

// normalizeAmount removes zeros that are not significant. The value does not
// change and the sign is not stored. For example, ("0015", "050") becomes
// "15.05".
func normalizeAmount(intDigits, fraction string) string {
	if intDigits == "" {
		intDigits = "0"
	}
	if fraction == "" {
		return intDigits
	}
	return intDigits + "." + fraction
}

func normalizeCategory(raw string, reservedCategories []string) (models.AccountCategory, error) {
	if strings.TrimSpace(raw) == "" {
		return "", ErrorMissingCategory
	}

	category, err := models.ToAccountCategory(raw)
	if err != nil {
		return "", ErrorInvalidCategory.withErr(err)
	}

	if slices.ContainsFunc(reservedCategories, func(reserved string) bool {
		return strings.ToLower(strings.TrimSpace(reserved)) == string(category)
	}) {
		return "", ErrorReservedCategory.withErr(fmt.Errorf("%q", category))
	}
	return category, nil
}

func parseEventType(raw string) (models.EventType, error) {
	clean := models.EventType(strings.ToLower(strings.TrimSpace(raw)))
	switch clean {
	case models.EventIncome, models.EventExpense, models.EventTransferIn, models.EventTransferOut:
		return clean, nil
	default:
		return "", ErrorInvalidType.withErr(fmt.Errorf("%q", raw))
	}
}

func mapHeaderColumns(header []string) (map[string]int, error) {
	if len(header) != len(requiredColumns) {
		return nil, ErrorInvalidHeader.withErr(fmt.Errorf("expected %d columns (%s), got %d",
			len(requiredColumns), strings.Join(requiredColumns, ","), len(header)))
	}

	// The header has exactly len(requiredColumns) unique names, and every required name must be present.
	idx := make(map[string]int, len(header))
	for i, col := range header {
		name := strings.ToLower(strings.TrimSpace(col))
		if _, duplicate := idx[name]; duplicate {
			return nil, ErrorInvalidHeader.withErr(fmt.Errorf("duplicate CSV header column: %s", name))
		}
		if !slices.Contains(requiredColumns, name) {
			return nil, ErrorInvalidHeader.withErr(fmt.Errorf("unexpected CSV header column: %s", name))
		}
		idx[name] = i
	}

	return idx, nil
}

// WriteInvalidCSV writes rejected rows with an appended `reason` column.
func WriteInvalidCSV(path string, originalHeader []string, badRows []BadRow) error {
	out, err := os.Create(path)
	if err != nil {
		return ErrorWriteInvalidCSV.withErr(err)
	}
	defer out.Close()

	// Report write errors instead of silently losing the invalid rows.
	writer := csv.NewWriter(out)
	if err := writer.Write(append(originalHeader, "reason")); err != nil {
		return ErrorWriteInvalidCSV.withErr(err)
	}
	for _, br := range badRows {
		rowWithReason := append(append([]string{}, br.RawFields...), br.Reason)
		if err := writer.Write(rowWithReason); err != nil {
			return ErrorWriteInvalidCSV.withErr(err)
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return ErrorWriteInvalidCSV.withErr(err)
	}
	return nil
}

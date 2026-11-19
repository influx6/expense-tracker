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
)

type EventType string

const (
	EventIncome      EventType = "income"
	EventExpense     EventType = "expense"
	EventTransferIn  EventType = "transfer_in"
	EventTransferOut EventType = "transfer_out"
)

// TransactionEvent is the clean domain value object passed into Wallet.Apply.
type TransactionEvent struct {
	OccuredAt time.Time
	Amount    decimal.Decimal
	Desc      string
	Category  string // normalized: lowercase + _ only
	Type      EventType
}

// BadRow captures a rejected CSV record alongside the reason it failed validation.
type BadRow struct {
	RawFields []string
	Reason    string
}

// BatchResult holds valid parsed events and any rejected rows from a CSV file.
type BatchResult struct {
	ValidEvents []TransactionEvent
	BadRows     []BadRow
}

// ParseMap validates a single record passed as a map[string]string (e.g. from `cli record` flags).
// Expected keys: "when", "amount", "desc", "category", "type".
func ParseMap(fields map[string]string) (TransactionEvent, error) {
	return validateAndBuildEvent(
		fields["when"],
		fields["amount"],
		fields["desc"],
		fields["category"],
		fields["type"],
	)
}

var (
	requiredColumns = []string{"when", "amount", "desc", "category", "type"}
)

// ParseCSVFile reads a CSV file with columns `when,amount,desc,category,type`,
// separates valid TransactionEvents from invalid rows, and writes bad rows to invalidOutPath.
func ParseCSVFile(csvPath string, invalidOutPath string) (BatchResult, error) {
	var result BatchResult

	f, err := os.Open(csvPath)
	if err != nil {
		return result, fmt.Errorf("open csv file: %w", err)
	}
	defer f.Close()

	reader := csv.NewReader(f)

	header, err := reader.Read()
	if err != nil {
		return result, fmt.Errorf("read csv header: %w", err)
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
			return result, fmt.Errorf("write invalid csv: %w", err)
		}
	}

	return result, nil
}

func validateAndBuildEvent(rawWhen, rawAmount, rawDesc, rawCategory, rawType string) (TransactionEvent, error) {
	var ev TransactionEvent

	// 1. Validate UTC Timestamp
	when, err := parseUTCTimestamp(rawWhen)
	if err != nil {
		return ev, err
	}

	// 2. Validate Decimal Amount (> 0, max 2 decimal places for HKD)
	amount, err := parseAmount(rawAmount, 2)
	if err != nil {
		return ev, err
	}

	// 3. Normalize & Validate Category (lowercase, spaces -> _, letters + _ only)
	category, err := normalizeCategory(rawCategory)
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
		return time.Time{}, errors.New("missing timestamp")
	}

	for _, layout := range timestamp_layout {
		if t, err := time.Parse(layout, clean); err == nil {
			return t.UTC(), nil
		}
	}

	return time.Time{}, fmt.Errorf("invalid UTC timestamp %q", clean)
}

var amountPattern = regexp.MustCompile(`^([+-]?)(\d+)(?:\.(\d+))?$`)

func parseAmount(raw string, decimalPlaces uint) (decimal.Decimal, error) {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return decimal.Decimal{}, errors.New("missing amount")
	}

	match := amountPattern.FindStringSubmatch(clean)
	if match == nil {
		return decimal.Decimal{}, fmt.Errorf("invalid amount %q: expected a plain decimal number", clean)
	}

	// 15.050 is accepted as 15.05.
	fraction := strings.TrimRight(match[3], "0")
	if len(fraction) > int(decimalPlaces) {
		return decimal.Decimal{}, fmt.Errorf("invalid amount %q: at most %d decimal places allowed", clean, decimalPlaces)
	}

	// decimal.Parse silently rounds once the integer part goes beyond MaxPrec, so reject any integer part that can not fit
	intDigits := strings.TrimLeft(match[2], "0")
	if maxIntDigits := decimal.MaxPrec - int(decimalPlaces); len(intDigits) > maxIntDigits {
		return decimal.Decimal{}, fmt.Errorf("invalid amount %q: integer part has more than %d digits", clean, maxIntDigits)
	}

	// decimal.Parse rejects strings longer than 330 bytes, even when the extra
	// bytes are zeros that do not change the value. Normalize first.
	d, err := decimal.Parse(normalizeAmount(intDigits, fraction))
	if err != nil {
		return decimal.Decimal{}, fmt.Errorf("invalid decimal amount %q: %w", clean, err)
	}

	// The sign is not stored. Apply it as x * -1.
	if match[1] == "-" {
		d = d.Neg()
	}

	if !d.IsPos() {
		return decimal.Decimal{}, fmt.Errorf("amount must be strictly positive (> 0), got %s", d.String())
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

var categoryRegex = regexp.MustCompile(`^[a-z_]+$`)

func normalizeCategory(raw string) (string, error) {
	clean := strings.ToLower(strings.TrimSpace(raw))
	clean = strings.ReplaceAll(clean, " ", "_")
	if clean == "" {
		return "", errors.New("missing category")
	}
	if !categoryRegex.MatchString(clean) {
		return "", fmt.Errorf("invalid category %q: only letters and underscores allowed", raw)
	}
	if clean == "wallet" {
		return "", errors.New("category 'wallet' is reserved for the system asset account")
	}
	return clean, nil
}

func parseEventType(raw string) (EventType, error) {
	clean := EventType(strings.ToLower(strings.TrimSpace(raw)))
	switch clean {
	case EventIncome, EventExpense, EventTransferIn, EventTransferOut:
		return clean, nil
	default:
		return "", fmt.Errorf("invalid type %q: expected income, expense, transfer_in, or transfer_out", raw)
	}
}

func mapHeaderColumns(header []string) (map[string]int, error) {
	if len(header) != len(requiredColumns) {
		return nil, fmt.Errorf("expected %d columns (%s), got %d",
			len(requiredColumns), strings.Join(requiredColumns, ","), len(header))
	}

	// The header has exactly len(requiredColumns) unique names, and every required name must be present.
	idx := make(map[string]int, len(header))
	for i, col := range header {
		name := strings.ToLower(strings.TrimSpace(col))
		if _, duplicate := idx[name]; duplicate {
			return nil, fmt.Errorf("duplicate CSV header column: %s", name)
		}
		if !slices.Contains(requiredColumns, name) {
			return nil, fmt.Errorf("unexpected CSV header column: %s", name)
		}
		idx[name] = i
	}

	return idx, nil
}

// WriteInvalidCSV writes rejected rows with an appended `reason` column.
func WriteInvalidCSV(path string, originalHeader []string, badRows []BadRow) error {
	out, err := os.Create(path)
	if err != nil {
		return err
	}
	defer out.Close()

	// Report write errors instead of silently losing the invalid rows.
	writer := csv.NewWriter(out)
	if err := writer.Write(append(originalHeader, "reason")); err != nil {
		return err
	}
	for _, br := range badRows {
		rowWithReason := append(append([]string{}, br.RawFields...), br.Reason)
		if err := writer.Write(rowWithReason); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}

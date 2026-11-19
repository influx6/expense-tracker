package parser_test

import (
	"encoding/csv"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/influx6/expense-tracker/internal/models"
	"github.com/influx6/expense-tracker/internal/parser"
)

var reservedCategories = []string{"wallet"}

func canonicalHeader() []string {
	return []string{"when", "amount", "desc", "category", "type"}
}

func validFields() map[string]string {
	return map[string]string{
		"when":     "2026-01-15T10:30:00Z",
		"amount":   "150.25",
		"desc":     "sample",
		"category": "food",
		"type":     "expense",
	}
}

func writeCSVFile(dir, name string, header []string, rows [][]string) (string, error) {
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	writer := csv.NewWriter(f)
	if err := writer.Write(header); err != nil {
		return "", err
	}
	for _, row := range rows {
		if err := writer.Write(row); err != nil {
			return "", err
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return "", err
	}
	return path, nil
}

func readCSVFile(path string) ([][]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	reader := csv.NewReader(f)
	reader.FieldsPerRecord = -1
	return reader.ReadAll()
}

func TestParseMapValid(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(map[string]string)
		wantWhen string
		wantAmt  string
		wantCat  string
		wantType models.EventType
		wantDesc string
	}{
		{
			name:     "expense",
			wantWhen: "2026-01-15T10:30:00Z",
			wantAmt:  "150.25",
			wantCat:  "food",
			wantType: models.EventExpense,
			wantDesc: "sample",
		},
		{
			name:     "income with messy type",
			mutate:   func(f map[string]string) { f["type"] = " INCOME " },
			wantWhen: "2026-01-15T10:30:00Z",
			wantAmt:  "150.25",
			wantCat:  "food",
			wantType: models.EventIncome,
			wantDesc: "sample",
		},
		{
			name:     "transfer in",
			mutate:   func(f map[string]string) { f["type"] = "transfer_in" },
			wantWhen: "2026-01-15T10:30:00Z",
			wantAmt:  "150.25",
			wantCat:  "food",
			wantType: models.EventTransferIn,
			wantDesc: "sample",
		},
		{
			name:     "transfer out",
			mutate:   func(f map[string]string) { f["type"] = "transfer_out" },
			wantWhen: "2026-01-15T10:30:00Z",
			wantAmt:  "150.25",
			wantCat:  "food",
			wantType: models.EventTransferOut,
			wantDesc: "sample",
		},
		{
			name:     "timestamp with offset",
			mutate:   func(f map[string]string) { f["when"] = "2026-01-15T18:30:00+08:00" },
			wantWhen: "2026-01-15T10:30:00Z",
			wantAmt:  "150.25",
			wantCat:  "food",
			wantType: models.EventExpense,
			wantDesc: "sample",
		},
		{
			name:     "date only",
			mutate:   func(f map[string]string) { f["when"] = "2026-01-15" },
			wantWhen: "2026-01-15T00:00:00Z",
			wantAmt:  "150.25",
			wantCat:  "food",
			wantType: models.EventExpense,
			wantDesc: "sample",
		},
		{
			name:     "category is normalized",
			mutate:   func(f map[string]string) { f["category"] = "Food Delivery" },
			wantWhen: "2026-01-15T10:30:00Z",
			wantAmt:  "150.25",
			wantCat:  "food_delivery",
			wantType: models.EventExpense,
			wantDesc: "sample",
		},
		{
			name:     "desc is trimmed",
			mutate:   func(f map[string]string) { f["desc"] = "  hello there  " },
			wantWhen: "2026-01-15T10:30:00Z",
			wantAmt:  "150.25",
			wantCat:  "food",
			wantType: models.EventExpense,
			wantDesc: "hello there",
		},
		{
			name:     "plus sign is not stored",
			mutate:   func(f map[string]string) { f["amount"] = "+15.05" },
			wantWhen: "2026-01-15T10:30:00Z",
			wantAmt:  "15.05",
			wantCat:  "food",
			wantType: models.EventExpense,
			wantDesc: "sample",
		},
		{
			name:     "redundant amount zeros",
			mutate:   func(f map[string]string) { f["amount"] = "15.050" },
			wantWhen: "2026-01-15T10:30:00Z",
			wantAmt:  "15.05",
			wantCat:  "food",
			wantType: models.EventExpense,
			wantDesc: "sample",
		},
		{
			// decimal.Parse rejects strings longer than 330 bytes, so the
			// parser must remove the redundant zeros before it calls Parse.
			name:     "long zero padded amount",
			mutate:   func(f map[string]string) { f["amount"] = "1." + strings.Repeat("0", 400) },
			wantWhen: "2026-01-15T10:30:00Z",
			wantAmt:  "1",
			wantCat:  "food",
			wantType: models.EventExpense,
			wantDesc: "sample",
		},
		{
			name:     "long leading zeros",
			mutate:   func(f map[string]string) { f["amount"] = strings.Repeat("0", 400) + "15.05" },
			wantWhen: "2026-01-15T10:30:00Z",
			wantAmt:  "15.05",
			wantCat:  "food",
			wantType: models.EventExpense,
			wantDesc: "sample",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fields := validFields()
			if tc.mutate != nil {
				tc.mutate(fields)
			}

			ev, err := parser.ParseMap(fields, 2, reservedCategories)
			if err != nil {
				t.Fatalf("ParseMap: %v", err)
			}
			if ev.OccuredAt.Location() != time.UTC {
				t.Errorf("time location = %v, want UTC", ev.OccuredAt.Location())
			}
			if got := ev.OccuredAt.Format(time.RFC3339); got != tc.wantWhen {
				t.Errorf("when = %s, want %s", got, tc.wantWhen)
			}
			if got := ev.Amount.String(); got != tc.wantAmt {
				t.Errorf("amount = %s, want %s", got, tc.wantAmt)
			}
			if ev.Category != tc.wantCat {
				t.Errorf("category = %q, want %q", ev.Category, tc.wantCat)
			}
			if ev.Type != tc.wantType {
				t.Errorf("type = %q, want %q", ev.Type, tc.wantType)
			}
			if ev.Desc != tc.wantDesc {
				t.Errorf("desc = %q, want %q", ev.Desc, tc.wantDesc)
			}
		})
	}
}

func TestParseMapInvalid(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(map[string]string)
		wantErr string
	}{
		{
			name:    "sub-cent amount",
			mutate:  func(f map[string]string) { f["amount"] = "15.055" },
			wantErr: "at most 2 decimal places",
		},
		{
			name:    "exponent amount",
			mutate:  func(f map[string]string) { f["amount"] = "1e3" },
			wantErr: "expected a plain decimal number",
		},
		{
			name:    "comma amount",
			mutate:  func(f map[string]string) { f["amount"] = "1,000.00" },
			wantErr: "expected a plain decimal number",
		},
		{
			name:    "zero amount",
			mutate:  func(f map[string]string) { f["amount"] = "0" },
			wantErr: "strictly positive",
		},
		{
			name:    "negative amount",
			mutate:  func(f map[string]string) { f["amount"] = "-5" },
			wantErr: "strictly positive",
		},
		{
			name:    "too many integer digits",
			mutate:  func(f map[string]string) { f["amount"] = "123456789012345678.99" },
			wantErr: "more than 17 digits",
		},
		{
			name:    "bad category character",
			mutate:  func(f map[string]string) { f["category"] = "food2" },
			wantErr: "only letters and underscores",
		},
		{
			name:    "reserved category",
			mutate:  func(f map[string]string) { f["category"] = "wallet" },
			wantErr: "reserved",
		},
		{
			name:    "bad type",
			mutate:  func(f map[string]string) { f["type"] = "credit" },
			wantErr: "invalid type",
		},
		{
			name:    "bad timestamp",
			mutate:  func(f map[string]string) { f["when"] = "not-a-time" },
			wantErr: "invalid UTC timestamp",
		},
		{
			name:    "missing timestamp",
			mutate:  func(f map[string]string) { delete(f, "when") },
			wantErr: "missing timestamp",
		},
		{
			name:    "missing amount",
			mutate:  func(f map[string]string) { delete(f, "amount") },
			wantErr: "missing amount",
		},
		{
			name:    "missing category",
			mutate:  func(f map[string]string) { delete(f, "category") },
			wantErr: "missing category",
		},
		{
			name:    "missing type",
			mutate:  func(f map[string]string) { delete(f, "type") },
			wantErr: "invalid type",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fields := validFields()
			tc.mutate(fields)

			if ev, err := parser.ParseMap(fields, 2, reservedCategories); err == nil {
				t.Fatalf("ParseMap accepted invalid fields as %+v", ev)
			} else if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestParseCSVFileValid(t *testing.T) {
	dir := t.TempDir()
	rows := [][]string{
		{"2026-01-02T03:04:05Z", "10.5", "coffee, large", "food", "expense"},
		{"2026-01-03", "1." + strings.Repeat("0", 400), "long amount", "gift", "income"},
		{"2026-01-05T00:00:00+08:00", "7.25", "loan", "bank", "transfer_in"},
	}
	path, err := writeCSVFile(dir, "valid.csv", canonicalHeader(), rows)
	if err != nil {
		t.Fatal(err)
	}

	result, err := parser.ParseCSVFile(path, "", 2, reservedCategories)
	if err != nil {
		t.Fatalf("ParseCSVFile: %v", err)
	}
	if len(result.BadRows) != 0 {
		t.Fatalf("got %d bad rows, want 0: %+v", len(result.BadRows), result.BadRows)
	}
	if len(result.ValidEvents) != len(rows) {
		t.Fatalf("got %d valid events, want %d", len(result.ValidEvents), len(rows))
	}

	first := result.ValidEvents[0]
	if first.Desc != "coffee, large" || first.Category != "food" || first.Type != models.EventExpense {
		t.Errorf("first event = %+v", first)
	}
	if first.Amount.String() != "10.5" {
		t.Errorf("first amount = %s, want 10.5", first.Amount)
	}
	if result.ValidEvents[1].Amount.String() != "1" {
		t.Errorf("long amount = %s, want 1", result.ValidEvents[1].Amount)
	}
	if !result.ValidEvents[2].OccuredAt.Equal(time.Date(2026, 1, 4, 16, 0, 0, 0, time.UTC)) {
		t.Errorf("third time = %v, want 2026-01-04T16:00:00Z", result.ValidEvents[2].OccuredAt)
	}
}

func TestParseCSVFileBadRows(t *testing.T) {
	dir := t.TempDir()
	rows := [][]string{
		{"2026-01-02T03:04:05Z", "10.50", "coffee", "food", "expense"},
		{"2026-01-02T03:04:05Z", "15.055", "too precise", "food", "expense"},
		{"2026-01-02T03:04:05Z", "5", "bad type", "food", "credit"},
		{"nope", "5", "bad time", "food", "expense"},
		{"2026-01-02", "5", "short row", "food"},
		{"2026-01-03", "7", "transfer", "bank", "transfer_in"},
	}
	path, err := writeCSVFile(dir, "mixed.csv", canonicalHeader(), rows)
	if err != nil {
		t.Fatal(err)
	}
	invalidPath := filepath.Join(dir, "invalid.csv")

	result, err := parser.ParseCSVFile(path, invalidPath, 2, reservedCategories)
	if err != nil {
		t.Fatalf("ParseCSVFile: %v", err)
	}
	if len(result.ValidEvents) != 2 {
		t.Errorf("got %d valid events, want 2", len(result.ValidEvents))
	}
	if len(result.BadRows) != 4 {
		t.Fatalf("got %d bad rows, want 4", len(result.BadRows))
	}

	wantReasons := []string{
		"at most 2 decimal places",
		"invalid type",
		"invalid UTC timestamp",
		"wrong number of fields",
	}
	for i, want := range wantReasons {
		if !strings.Contains(result.BadRows[i].Reason, want) {
			t.Errorf("bad row %d reason = %q, want it to contain %q", i, result.BadRows[i].Reason, want)
		}
	}

	invalidRows, err := readCSVFile(invalidPath)
	if err != nil {
		t.Fatalf("reading invalid.csv: %v", err)
	}
	if len(invalidRows) != len(result.BadRows)+1 {
		t.Fatalf("invalid.csv has %d rows, want %d", len(invalidRows), len(result.BadRows)+1)
	}
	if want := append(canonicalHeader(), "reason"); !slices.Equal(invalidRows[0], want) {
		t.Errorf("invalid.csv header = %v, want %v", invalidRows[0], want)
	}
	for i, badRow := range result.BadRows {
		want := append(append([]string{}, badRow.RawFields...), badRow.Reason)
		if !slices.Equal(invalidRows[i+1], want) {
			t.Errorf("invalid.csv row %d = %v, want %v", i+1, invalidRows[i+1], want)
		}
	}
}

func TestParseCSVFileHeader(t *testing.T) {
	tests := []struct {
		name    string
		header  []string
		rows    [][]string
		wantErr string
	}{
		{
			name:   "reordered columns",
			header: []string{"type", "when", "category", "amount", "desc"},
			rows:   [][]string{{"expense", "2026-01-02", "food", "3.50", "coffee"}},
		},
		{
			name:   "case and padding insensitive",
			header: []string{" When ", "AMOUNT", "Desc", "category ", " TYPE"},
			rows:   [][]string{{"2026-01-02", "3.50", "coffee", "food", "expense"}},
		},
		{
			name:    "too few columns",
			header:  []string{"when", "amount", "desc", "category"},
			wantErr: "expected 5 columns",
		},
		{
			name:    "extra column",
			header:  []string{"when", "amount", "desc", "category", "type", "note"},
			wantErr: "expected 5 columns",
		},
		{
			name:    "duplicate column",
			header:  []string{"when", "when", "amount", "desc", "category"},
			wantErr: "duplicate CSV header column: when",
		},
		{
			name:    "unknown column",
			header:  []string{"when", "amount", "desc", "category", "date"},
			wantErr: "unexpected CSV header column: date",
		},
		{
			name:    "empty file",
			wantErr: "read csv header",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path, err := writeCSVFile(dir, "input.csv", tc.header, tc.rows)
			if err != nil {
				t.Fatal(err)
			}

			result, err := parser.ParseCSVFile(path, "", 2, reservedCategories)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("ParseCSVFile accepted header %v", tc.header)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("error = %q, want it to contain %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseCSVFile: %v", err)
			}
			if len(result.ValidEvents) != len(tc.rows) {
				t.Errorf("got %d valid events, want %d", len(result.ValidEvents), len(tc.rows))
			}
		})
	}

	t.Run("header only", func(t *testing.T) {
		dir := t.TempDir()
		path, err := writeCSVFile(dir, "header-only.csv", canonicalHeader(), nil)
		if err != nil {
			t.Fatal(err)
		}
		result, err := parser.ParseCSVFile(path, "", 2, reservedCategories)
		if err != nil {
			t.Fatalf("ParseCSVFile: %v", err)
		}
		if len(result.ValidEvents) != 0 || len(result.BadRows) != 0 {
			t.Errorf("got %d valid and %d bad rows, want none", len(result.ValidEvents), len(result.BadRows))
		}
	})
}

func TestParseCSVFileMissingFile(t *testing.T) {
	_, err := parser.ParseCSVFile(filepath.Join(t.TempDir(), "missing.csv"), "", 2, reservedCategories)
	if err == nil {
		t.Fatal("ParseCSVFile accepted a missing file")
	}
	if !strings.Contains(err.Error(), "open csv file") {
		t.Errorf("error = %q, want it to contain %q", err, "open csv file")
	}
}

func TestParseCSVFileInvalidOutputPath(t *testing.T) {
	dir := t.TempDir()
	rows := [][]string{{"nope", "5", "bad time", "food", "expense"}}
	path, err := writeCSVFile(dir, "mixed.csv", canonicalHeader(), rows)
	if err != nil {
		t.Fatal(err)
	}

	// A directory cannot be used as the invalid output file.
	var _, parseErr = parser.ParseCSVFile(path, dir, 2, reservedCategories)
	if parseErr == nil {
		t.Fatal("ParseCSVFile accepted a directory as invalid output path")
	}

	if !strings.Contains(parseErr.Error(), "write invalid csv") {
		t.Errorf("error = %q, want it to contain %q", err, "write invalid csv")
	}
}

func TestWriteInvalidCSV(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "invalid.csv")
	badRows := []parser.BadRow{
		{
			RawFields: []string{"a", "b", "c", "d", "e"},
			Reason:    "bad, \"quoted\", multi\nline",
		},
		{
			RawFields: []string{"1", "2", "3", "4", "5"},
			Reason:    "simple",
		},
	}

	if err := parser.WriteInvalidCSV(path, canonicalHeader(), badRows); err != nil {
		t.Fatalf("WriteInvalidCSV: %v", err)
	}

	rows, err := readCSVFile(path)
	if err != nil {
		t.Fatalf("reading invalid.csv: %v", err)
	}
	if len(rows) != len(badRows)+1 {
		t.Fatalf("got %d rows, want %d", len(rows), len(badRows)+1)
	}
	if want := append(canonicalHeader(), "reason"); !slices.Equal(rows[0], want) {
		t.Errorf("header = %v, want %v", rows[0], want)
	}
	for i, badRow := range badRows {
		want := append(append([]string{}, badRow.RawFields...), badRow.Reason)
		if !slices.Equal(rows[i+1], want) {
			t.Errorf("row %d = %v, want %v", i+1, rows[i+1], want)
		}
	}
}

func TestWriteInvalidCSVError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing-dir", "invalid.csv")
	if err := parser.WriteInvalidCSV(path, canonicalHeader(), nil); err == nil {
		t.Fatal("WriteInvalidCSV accepted an unwritable path")
	}
}

func TestParserErrors(t *testing.T) {
	parseMapError := func(mutate func(map[string]string)) error {
		fields := validFields()
		mutate(fields)
		_, err := parser.ParseMap(fields, 2, reservedCategories)
		return err
	}

	tests := []struct {
		name string
		run  func() error
		want error
	}{
		{
			name: "missing timestamp",
			run:  func() error { return parseMapError(func(f map[string]string) { delete(f, "when") }) },
			want: parser.ErrorMissingTimestamp,
		},
		{
			name: "invalid timestamp",
			run:  func() error { return parseMapError(func(f map[string]string) { f["when"] = "nope" }) },
			want: parser.ErrorInvalidTimestamp,
		},
		{
			name: "missing amount",
			run:  func() error { return parseMapError(func(f map[string]string) { delete(f, "amount") }) },
			want: parser.ErrorMissingAmount,
		},
		{
			name: "invalid amount",
			run:  func() error { return parseMapError(func(f map[string]string) { f["amount"] = "abc" }) },
			want: parser.ErrorInvalidAmount,
		},
		{
			name: "too many decimals",
			run:  func() error { return parseMapError(func(f map[string]string) { f["amount"] = "15.055" }) },
			want: parser.ErrorTooManyDecimals,
		},
		{
			name: "amount too large",
			run: func() error {
				return parseMapError(func(f map[string]string) { f["amount"] = "123456789012345678.99" })
			},
			want: parser.ErrorAmountTooLarge,
		},
		{
			name: "non-positive amount",
			run:  func() error { return parseMapError(func(f map[string]string) { f["amount"] = "-5" }) },
			want: parser.ErrorNonPositiveAmount,
		},
		{
			name: "missing category",
			run:  func() error { return parseMapError(func(f map[string]string) { delete(f, "category") }) },
			want: parser.ErrorMissingCategory,
		},
		{
			name: "invalid category",
			run:  func() error { return parseMapError(func(f map[string]string) { f["category"] = "food2" }) },
			want: parser.ErrorInvalidCategory,
		},
		{
			name: "reserved category",
			run:  func() error { return parseMapError(func(f map[string]string) { f["category"] = "wallet" }) },
			want: parser.ErrorReservedCategory,
		},
		{
			name: "invalid type",
			run:  func() error { return parseMapError(func(f map[string]string) { f["type"] = "credit" }) },
			want: parser.ErrorInvalidType,
		},
		{
			name: "invalid header",
			run: func() error {
				path, err := writeCSVFile(t.TempDir(), "bad-header.csv",
					[]string{"when", "amount", "desc", "category"}, nil)
				if err != nil {
					t.Fatal(err)
				}
				_, err = parser.ParseCSVFile(path, "", 2, reservedCategories)
				return err
			},
			want: parser.ErrorInvalidHeader,
		},
		{
			name: "missing csv file",
			run: func() error {
				_, err := parser.ParseCSVFile(filepath.Join(t.TempDir(), "missing.csv"), "", 2, reservedCategories)
				return err
			},
			want: parser.ErrorOpenCSV,
		},
		{
			name: "unwritable invalid csv",
			run: func() error {
				return parser.WriteInvalidCSV(filepath.Join(t.TempDir(), "missing-dir", "invalid.csv"),
					canonicalHeader(), nil)
			},
			want: parser.ErrorWriteInvalidCSV,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run()
			if err == nil {
				t.Fatalf("expected %v, got nil", tc.want)
			}
			if !errors.Is(err, tc.want) {
				t.Errorf("errors.Is(error, %v) = false; error = %v", tc.want, err)
			}
			var parserErr parser.ParserError
			if !errors.As(err, &parserErr) {
				t.Errorf("error type = %T, want parser.ParserError", err)
			}
		})
	}
}

func TestParseMapCurrencyDecimalPlaces(t *testing.T) {
	tests := []struct {
		name    string
		places  uint
		amount  string
		wantErr bool
	}{
		{name: "zero places accepts an integer", places: 0, amount: "100"},
		{name: "zero places rejects decimals", places: 0, amount: "15.05", wantErr: true},
		{name: "one place accepts one decimal", places: 1, amount: "15.5"},
		{name: "one place rejects two decimals", places: 1, amount: "15.05", wantErr: true},
		{name: "two places accepts HKD", places: 2, amount: "15.05"},
		{name: "two places rejects three decimals", places: 2, amount: "15.055", wantErr: true},
		{name: "three places accepts three decimals", places: 3, amount: "15.055"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fields := validFields()
			fields["amount"] = tc.amount

			_, err := parser.ParseMap(fields, tc.places, reservedCategories)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantErr && !errors.Is(err, parser.ErrorTooManyDecimals) {
				t.Errorf("error = %v, want parser.ErrorTooManyDecimals", err)
			}
		})
	}
}

func TestParseMapReservedCategories(t *testing.T) {
	tests := []struct {
		name     string
		reserved []string
		category string
		wantErr  bool
	}{
		{name: "nothing reserved", category: "wallet"},
		{name: "wallet is reserved", reserved: []string{"wallet"}, category: "wallet", wantErr: true},
		{name: "reserved entry is case insensitive", reserved: []string{"Wallet"}, category: "wallet", wantErr: true},
		{name: "custom reserved name", reserved: []string{"salary"}, category: "salary", wantErr: true},
		{name: "only the given names are reserved", reserved: []string{"salary"}, category: "wallet"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fields := validFields()
			fields["category"] = tc.category

			_, err := parser.ParseMap(fields, 2, tc.reserved)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantErr && !errors.Is(err, parser.ErrorReservedCategory) {
				t.Errorf("error = %v, want parser.ErrorReservedCategory", err)
			}
		})
	}
}

package output

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/xuri/excelize/v2"

	"gscraper/internal/gmaps"
)

type column struct {
	name    string
	numeric bool
	value   func(gmaps.Place) string
}

var columns = []column{
	{"name", false, func(p gmaps.Place) string { return p.Name }},
	{"address", false, func(p gmaps.Place) string { return p.Address }},
	{"website", false, func(p gmaps.Place) string { return p.Website }},
	{"phone_number", false, func(p gmaps.Place) string { return p.Phone }},
	{"reviews_count", true, func(p gmaps.Place) string { return formatInt(p.ReviewsCount) }},
	{"reviews_average", true, func(p gmaps.Place) string { return formatFloat(p.ReviewsAverage, 1) }},
	{"store_shopping", false, func(p gmaps.Place) string { return yesNo(p.StoreShopping) }},
	{"in_store_pickup", false, func(p gmaps.Place) string { return yesNo(p.InStorePickup) }},
	{"store_delivery", false, func(p gmaps.Place) string { return yesNo(p.StoreDelivery) }},
	{"place_type", false, func(p gmaps.Place) string { return p.PlaceType }},
	{"opens_at", false, func(p gmaps.Place) string { return p.OpensAt }},
	{"introduction", false, func(p gmaps.Place) string { return strings.Join(strings.Fields(p.Introduction), " ") }},
	{"categories", false, func(p gmaps.Place) string { return strings.Join(p.Categories, ", ") }},
	{"phone_international", false, func(p gmaps.Place) string { return p.PhoneIntl }},
	{"website_url", false, func(p gmaps.Place) string { return p.WebsiteURL }},
	{"latitude", true, func(p gmaps.Place) string { return formatFloat(p.Latitude, 7) }},
	{"longitude", true, func(p gmaps.Place) string { return formatFloat(p.Longitude, 7) }},
	{"place_id", false, func(p gmaps.Place) string { return p.PlaceID }},
	{"maps_url", false, func(p gmaps.Place) string { return p.MapsURL() }},
	{"query", false, func(p gmaps.Place) string { return p.Query }},
}

var standard = []string{"name", "address", "website", "phone_number", "reviews_count", "reviews_average", "opens_at", "introduction", "categories", "phone_international", "website_url"}

func Columns(all bool) []string {
	return names(selected(all))
}

func selected(all bool) []column {
	if all {
		return columns
	}
	var cols []column
	for _, name := range standard {
		i := slices.IndexFunc(columns, func(c column) bool { return c.name == name })
		cols = append(cols, columns[i])
	}
	return cols
}

func (c column) cell(p gmaps.Place) string {
	return strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, c.value(p))
}

func names(cols []column) []string {
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = c.name
	}
	return out
}

func row(p gmaps.Place, cols []column) []string {
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = c.cell(p)
	}
	return out
}

func formatInt(n int) string {
	if n == 0 {
		return ""
	}
	return strconv.Itoa(n)
}

func formatFloat(f float64, decimals int) string {
	if f == 0 {
		return ""
	}
	return strconv.FormatFloat(f, 'f', decimals, 64)
}

func yesNo(b bool) string {
	if b {
		return "Yes"
	}
	return "No"
}

func Write(path string, places []gmaps.Place, allColumns, appendMode bool) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	cols := selected(allColumns)

	switch ext := strings.ToLower(filepath.Ext(path)); ext {
	case ".csv", ".txt":
		return writeCSV(path, places, cols, appendMode)
	case ".json", ".xlsx":
		if appendMode {
			return fmt.Errorf("append only works with .csv files")
		}
		if ext == ".json" {
			return writeJSON(path, places, cols)
		}
		return writeXLSX(path, places, cols)
	default:
		return fmt.Errorf("unsupported file type %q, use .csv, .json or .xlsx", ext)
	}
}

func writeCSV(path string, places []gmaps.Place, cols []column, appendMode bool) error {
	flags := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	header := true
	if appendMode {
		flags = os.O_CREATE | os.O_WRONLY | os.O_APPEND
		if st, err := os.Stat(path); err == nil && st.Size() > 0 {
			header = false
		}
	}

	f, err := os.OpenFile(path, flags, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(bufio.NewWriter(f))
	w.Comma = '|'
	if header {
		w.Write(names(cols))
	}
	for _, p := range places {
		w.Write(row(p, cols))
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return err
	}
	return f.Close()
}

func writeJSON(path string, places []gmaps.Place, cols []column) error {
	var out bytes.Buffer
	out.WriteString("[\n")
	for i, p := range places {
		fields := make([]string, len(cols))
		for j, c := range cols {
			fields[j] = fmt.Sprintf("%q: %s", c.name, jsonValue(c, p))
		}
		comma := ","
		if i == len(places)-1 {
			comma = ""
		}
		fmt.Fprintf(&out, "  {%s}%s\n", strings.Join(fields, ", "), comma)
	}
	out.WriteString("]\n")
	return os.WriteFile(path, out.Bytes(), 0o644)
}

func jsonValue(c column, p gmaps.Place) string {
	v := c.cell(p)
	if !c.numeric {
		quoted, _ := json.Marshal(v)
		return string(quoted)
	}
	if v == "" {
		return "null"
	}
	return v
}

func writeXLSX(path string, places []gmaps.Place, cols []column) error {
	f := excelize.NewFile()
	defer f.Close()
	sheet := f.GetSheetName(0)

	header := make([]any, len(cols))
	for i, c := range cols {
		header[i] = c.name
	}
	f.SetSheetRow(sheet, "A1", &header)

	for r, p := range places {
		cells := make([]any, len(cols))
		for i, c := range cols {
			v := c.cell(p)
			cells[i] = v
			if n, err := strconv.ParseFloat(v, 64); c.numeric && err == nil {
				cells[i] = n
			}
		}
		cell, _ := excelize.CoordinatesToCellName(1, r+2)
		f.SetSheetRow(sheet, cell, &cells)
	}

	lastHeader, _ := excelize.CoordinatesToCellName(len(cols), 1)
	lastCell, _ := excelize.CoordinatesToCellName(len(cols), len(places)+1)
	if bold, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}}); err == nil {
		f.SetCellStyle(sheet, "A1", lastHeader, bold)
	}
	f.AutoFilter(sheet, "A1:"+lastCell, nil)
	f.SetPanes(sheet, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"})
	return f.SaveAs(path)
}

func ReadCSV(path string) (header []string, rows [][]string, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}

	r := csv.NewReader(bytes.NewReader(data))
	r.LazyQuotes = true
	r.FieldsPerRecord = -1
	if firstLine, _, _ := bytes.Cut(data, []byte("\n")); bytes.Contains(firstLine, []byte("|")) {
		r.Comma = '|'
	}

	records, err := r.ReadAll()
	if err != nil || len(records) == 0 {
		return nil, nil, err
	}
	return records[0], records[1:], nil
}

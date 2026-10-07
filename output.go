package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/ginkio-nl/abmctl/internal/apiclient"
)

// column describes one table column: a header and the resource attribute
// key(s) to pull the value from (first match wins), or a value func for
// columns that aren't a plain attribute. date trims an ISO 8601 timestamp
// to its date.
type column struct {
	header string
	keys   []string
	value  func(r apiclient.Resource) string
	date   bool
}

// idColumn shows a resource's ID. Column sets include it explicitly, so
// resources whose ID duplicates another column (devices, whose ID is their
// serial number) can leave it out.
var idColumn = column{header: "ID", value: func(r apiclient.Resource) string { return r.ID }}

func (c column) get(r apiclient.Resource) string {
	var v string
	if c.value != nil {
		v = c.value(r)
	} else {
		v = r.FirstStr(c.keys...)
	}
	if c.date {
		v = dateOnly(v)
	}
	return v
}

// dateOnly formats an ISO 8601 timestamp as its UTC date (2006-01-02).
// Apple's timestamps are UTC, and some (like coverage end dates) are
// date-only values sent as midnight UTC, so converting to local time could
// shift them a day. Anything unparseable is returned as-is.
func dateOnly(s string) string {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return s
	}
	return t.UTC().Format(time.DateOnly)
}

// printResources renders resources as pretty-printed JSON (the full,
// authoritative payload), CSV, an Excel workbook, or a best-effort table
// using the given columns, depending on output.
func printResources(output string, resources []apiclient.Resource, cols []column) error {
	switch output {
	case "json":
		return printJSON(resources)
	case "csv":
		return printCSV(resources, cols)
	case "xlsx":
		return printXLSX(resources, cols)
	default:
		return printTable(resources, cols)
	}
}

func printResource(output string, resource apiclient.Resource, cols []column) error {
	switch output {
	case "json":
		return printJSON(resource)
	case "csv":
		return printCSV([]apiclient.Resource{resource}, cols)
	case "xlsx":
		return printXLSX([]apiclient.Resource{resource}, cols)
	default:
		return printTable([]apiclient.Resource{resource}, cols)
	}
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func printTable(resources []apiclient.Resource, cols []column) error {
	if len(resources) == 0 {
		fmt.Println("No results.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	defer w.Flush()

	fmt.Fprintln(w, headerRow(cols))
	for _, r := range resources {
		fmt.Fprintln(w, valueRow(r, cols))
	}
	return nil
}

// printCSV writes resources as CSV: one column per col, one resource per
// row. Missing values are written as empty fields rather than
// table view's "-", since CSV output is meant for scripting/spreadsheets.
func printCSV(resources []apiclient.Resource, cols []column) error {
	w := csv.NewWriter(os.Stdout)
	defer w.Flush()

	header := make([]string, len(cols))
	for i, c := range cols {
		header[i] = c.header
	}
	if err := w.Write(header); err != nil {
		return err
	}

	for _, r := range resources {
		row := make([]string, len(cols))
		for i, c := range cols {
			row[i] = c.get(r)
		}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	return w.Error()
}

func headerRow(cols []column) string {
	var s strings.Builder
	for i, c := range cols {
		if i > 0 {
			s.WriteString("\t")
		}
		s.WriteString(c.header)
	}
	return s.String()
}

func valueRow(r apiclient.Resource, cols []column) string {
	var s strings.Builder
	for i, c := range cols {
		if i > 0 {
			s.WriteString("\t")
		}
		v := c.get(r)
		if v == "" {
			v = "-"
		}
		s.WriteString(v)
	}
	return s.String()
}

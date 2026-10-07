package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"abmctl/internal/apiclient"
)

// column describes one table column: a header and the resource attribute
// key(s) to pull the value from (first match wins), or a value func for
// columns that aren't a plain attribute.
type column struct {
	header string
	keys   []string
	value  func(r apiclient.Resource) string
}

func (c column) get(r apiclient.Resource) string {
	if c.value != nil {
		return c.value(r)
	}
	return r.FirstStr(c.keys...)
}

// printResources renders resources as pretty-printed JSON (the full,
// authoritative payload), CSV, or a best-effort table using the given
// columns, depending on output.
func printResources(output string, resources []apiclient.Resource, cols []column) error {
	switch output {
	case "json":
		return printJSON(resources)
	case "csv":
		return printCSV(resources, cols)
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

	fmt.Fprintln(w, "ID\t"+headerRow(cols))
	for _, r := range resources {
		fmt.Fprintln(w, r.ID+"\t"+valueRow(r, cols))
	}
	return nil
}

// printCSV writes resources as CSV: an ID column followed by cols, one
// resource per row. Missing values are written as empty fields rather than
// table view's "-", since CSV output is meant for scripting/spreadsheets.
func printCSV(resources []apiclient.Resource, cols []column) error {
	w := csv.NewWriter(os.Stdout)
	defer w.Flush()

	header := make([]string, len(cols)+1)
	header[0] = "ID"
	for i, c := range cols {
		header[i+1] = c.header
	}
	if err := w.Write(header); err != nil {
		return err
	}

	for _, r := range resources {
		row := make([]string, len(cols)+1)
		row[0] = r.ID
		for i, c := range cols {
			row[i+1] = c.get(r)
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

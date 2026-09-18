package main

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"abmctl/internal/apiclient"
)

// column describes one table column: a header and the resource attribute
// key(s) to pull the value from (first match wins).
type column struct {
	header string
	keys   []string
}

// printResources renders resources either as pretty-printed JSON (the full,
// authoritative payload) or as a best-effort table using the given columns.
func printResources(output string, resources []apiclient.Resource, cols []column) error {
	if output == "json" {
		return printJSON(resources)
	}
	return printTable(resources, cols)
}

func printResource(output string, resource apiclient.Resource, cols []column) error {
	if output == "json" {
		return printJSON(resource)
	}
	return printTable([]apiclient.Resource{resource}, cols)
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

func headerRow(cols []column) string {
	s := ""
	for i, c := range cols {
		if i > 0 {
			s += "\t"
		}
		s += c.header
	}
	return s
}

func valueRow(r apiclient.Resource, cols []column) string {
	s := ""
	for i, c := range cols {
		if i > 0 {
			s += "\t"
		}
		v := r.FirstStr(c.keys...)
		if v == "" {
			v = "-"
		}
		s += v
	}
	return s
}

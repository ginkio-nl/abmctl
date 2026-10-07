package main

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"testing"
	"time"

	"github.com/ginkio-nl/abmctl/internal/apiclient"
)

func TestXLSXCellRef(t *testing.T) {
	for _, tc := range []struct {
		col, row int
		want     string
	}{
		{0, 1, "A1"},
		{25, 2, "Z2"},
		{26, 3, "AA3"},
		{27, 3, "AB3"},
		{701, 10, "ZZ10"},
		{702, 10, "AAA10"},
	} {
		if got := xlsxCellRef(tc.col, tc.row); got != tc.want {
			t.Errorf("xlsxCellRef(%d, %d) = %q, want %q", tc.col, tc.row, got, tc.want)
		}
	}
}

func TestXLSXDateSerial(t *testing.T) {
	for in, want := range map[string]int{
		"1900-03-01": 61, // first day after Excel's fictional 1900-02-29
		"2018-10-26": 43399,
		"2026-10-21": 46316,
	} {
		d, _ := time.Parse(time.DateOnly, in)
		if got := xlsxDateSerial(d); got != want {
			t.Errorf("xlsxDateSerial(%s) = %d, want %d", in, got, want)
		}
	}
}

// xlsxTestCell is the subset of a worksheet <c> element the tests check.
type xlsxTestCell struct {
	Ref    string `xml:"r,attr"`
	Type   string `xml:"t,attr"`
	Style  string `xml:"s,attr"`
	Value  string `xml:"v"`
	Inline string `xml:"is>t"`
}

func readXLSXSheet(t *testing.T, data []byte) (map[string]xlsxTestCell, string) {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("not a zip: %v", err)
	}

	var sheet []byte
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		// Every part must be well-formed XML.
		dec := xml.NewDecoder(bytes.NewReader(body))
		for {
			if _, err := dec.Token(); err == io.EOF {
				break
			} else if err != nil {
				t.Fatalf("%s is not well-formed XML: %v", f.Name, err)
			}
		}
		if f.Name == "xl/worksheets/sheet1.xml" {
			sheet = body
		}
	}
	if sheet == nil {
		t.Fatal("workbook has no xl/worksheets/sheet1.xml")
	}

	var ws struct {
		Rows []struct {
			Cells []xlsxTestCell `xml:"c"`
		} `xml:"sheetData>row"`
		AutoFilter struct {
			Ref string `xml:"ref,attr"`
		} `xml:"autoFilter"`
	}
	if err := xml.Unmarshal(sheet, &ws); err != nil {
		t.Fatalf("parsing sheet: %v", err)
	}
	cells := map[string]xlsxTestCell{}
	for _, row := range ws.Rows {
		for _, c := range row.Cells {
			cells[c.Ref] = c
		}
	}
	return cells, ws.AutoFilter.Ref
}

func TestWriteXLSX(t *testing.T) {
	cols := []column{
		idColumn,
		{header: "NAME", keys: []string{"name"}},
		{header: "ADDED", keys: []string{"added"}, date: true},
	}
	resources := []apiclient.Resource{
		{ID: "007", Attributes: map[string]any{"name": `=HYPERLINK("x") & <b>`, "added": "2018-10-26T15:02:19Z"}},
		{ID: "2", Attributes: map[string]any{"added": "not a date"}},
	}

	var buf bytes.Buffer
	if err := writeXLSX(&buf, resources, cols); err != nil {
		t.Fatalf("writeXLSX: %v", err)
	}
	cells, filter := readXLSXSheet(t, buf.Bytes())

	if c := cells["A1"]; c.Inline != "ID" || c.Style != "1" {
		t.Errorf("A1 = %+v, want a bold ID header", c)
	}
	// IDs stay text, so leading zeros survive.
	if c := cells["A2"]; c.Type != "inlineStr" || c.Inline != "007" {
		t.Errorf("A2 = %+v, want inline text 007", c)
	}
	// Formula-looking and XML-special values come through as literal text.
	if c := cells["B2"]; c.Type != "inlineStr" || c.Inline != `=HYPERLINK("x") & <b>` {
		t.Errorf("B2 = %+v, want the literal name", c)
	}
	if c := cells["C2"]; c.Type != "" || c.Value != "43399" || c.Style != "2" {
		t.Errorf("C2 = %+v, want date serial 43399 with the date style", c)
	}
	// Missing values are empty cells.
	if c, ok := cells["B3"]; ok {
		t.Errorf("B3 = %+v, want no cell for a missing value", c)
	}
	// Unparseable dates fall back to text, as in the table view.
	if c := cells["C3"]; c.Inline != "not a date" {
		t.Errorf("C3 = %+v, want text fallback", c)
	}
	if filter != "A1:C3" {
		t.Errorf("autoFilter = %q, want A1:C3", filter)
	}
}

func TestWriteXLSXNoResources(t *testing.T) {
	var buf bytes.Buffer
	if err := writeXLSX(&buf, nil, []column{idColumn}); err != nil {
		t.Fatalf("writeXLSX: %v", err)
	}
	cells, _ := readXLSXSheet(t, buf.Bytes())
	if len(cells) != 1 || cells["A1"].Inline != "ID" {
		t.Errorf("cells = %+v, want just the header", cells)
	}
}

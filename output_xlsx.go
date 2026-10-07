package main

import (
	"archive/zip"
	"bufio"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ginkio-nl/abmctl/internal/apiclient"
)

// printXLSX writes resources as an Excel workbook to stdout. Since xlsx is
// a binary (zip) format, it refuses to write to a terminal: redirect it to
// a file instead.
func printXLSX(resources []apiclient.Resource, cols []column) error {
	if fi, err := os.Stdout.Stat(); err == nil && fi.Mode()&os.ModeCharDevice != 0 {
		return errors.New("--output xlsx writes a binary file; redirect it, e.g. abmctl devices list -o xlsx > devices.xlsx")
	}
	w := bufio.NewWriter(os.Stdout)
	if err := writeXLSX(w, resources, cols); err != nil {
		return err
	}
	return w.Flush()
}

// Cell style indexes into the cellXfs list in xlsxStyles.
const (
	xlsxStyleHeader = 1
	xlsxStyleDate   = 2
)

// xlsxMaxColWidth caps the auto-sized column width (in characters), so one
// long value doesn't make a column unreadably wide.
const xlsxMaxColWidth = 60

// writeXLSX writes a minimal single-sheet workbook with the same columns as
// the table and CSV views: a bold, frozen header row with an autofilter,
// then one resource per row. Date columns are written as real Excel dates
// (so they sort and filter as dates); everything else is text, so values
// like serial numbers are never reinterpreted as numbers or formulas.
// Missing values are left as empty cells, as in CSV.
//
// It's hand-written against the Office Open XML spec rather than built on
// a library, since one plain sheet only takes a handful of small XML parts.
func writeXLSX(w io.Writer, resources []apiclient.Resource, cols []column) error {
	zw := zip.NewWriter(w)
	parts := []struct{ name, body string }{
		{"[Content_Types].xml", xlsxContentTypes},
		{"_rels/.rels", xlsxRootRels},
		{"xl/workbook.xml", xlsxWorkbook},
		{"xl/_rels/workbook.xml.rels", xlsxWorkbookRels},
		{"xl/styles.xml", xlsxStyles},
		{"xl/worksheets/sheet1.xml", xlsxSheet(resources, cols)},
	}
	for _, p := range parts {
		f, err := zw.Create(p.name)
		if err != nil {
			return err
		}
		if _, err := io.WriteString(f, p.body); err != nil {
			return err
		}
	}
	return zw.Close()
}

func xlsxSheet(resources []apiclient.Resource, cols []column) string {
	widths := make([]int, len(cols))
	for i, c := range cols {
		widths[i] = utf8.RuneCountInString(c.header) + 3 // room for the filter button
	}

	var rows strings.Builder
	rows.WriteString(`<row r="1">`)
	for i, c := range cols {
		xlsxTextCell(&rows, xlsxCellRef(i, 1), c.header, xlsxStyleHeader)
	}
	rows.WriteString(`</row>`)

	for n, r := range resources {
		rowNum := n + 2
		fmt.Fprintf(&rows, `<row r="%d">`, rowNum)
		for i, c := range cols {
			v := c.get(r)
			if v == "" {
				continue
			}
			ref := xlsxCellRef(i, rowNum)
			if t, err := time.Parse(time.DateOnly, v); c.date && err == nil {
				fmt.Fprintf(&rows, `<c r="%s" s="%d"><v>%d</v></c>`, ref, xlsxStyleDate, xlsxDateSerial(t))
				widths[i] = max(widths[i], len(time.DateOnly)+2)
				continue
			}
			xlsxTextCell(&rows, ref, v, 0)
			widths[i] = max(widths[i], utf8.RuneCountInString(v)+2)
		}
		rows.WriteString(`</row>`)
	}

	var s strings.Builder
	s.WriteString(xml.Header)
	s.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`)
	s.WriteString(`<sheetViews><sheetView workbookViewId="0"><pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews>`)
	if len(cols) > 0 {
		s.WriteString(`<cols>`)
		for i, wd := range widths {
			fmt.Fprintf(&s, `<col min="%d" max="%d" width="%d" customWidth="1"/>`, i+1, i+1, min(wd, xlsxMaxColWidth))
		}
		s.WriteString(`</cols>`)
	}
	s.WriteString(`<sheetData>`)
	s.WriteString(rows.String())
	s.WriteString(`</sheetData>`)
	if len(cols) > 0 {
		fmt.Fprintf(&s, `<autoFilter ref="A1:%s"/>`, xlsxCellRef(len(cols)-1, len(resources)+1))
	}
	s.WriteString(`</worksheet>`)
	return s.String()
}

// xlsxTextCell writes an inline-string cell. Inline strings are always
// literal text, so a value starting with "=" can't become a formula.
func xlsxTextCell(b *strings.Builder, ref, v string, style int) {
	fmt.Fprintf(b, `<c r="%s" t="inlineStr"`, ref)
	if style != 0 {
		fmt.Fprintf(b, ` s="%d"`, style)
	}
	b.WriteString(`><is><t xml:space="preserve">`)
	xml.EscapeText(b, []byte(v)) // also replaces characters XML can't hold
	b.WriteString(`</t></is></c>`)
}

// xlsxCellRef returns an A1-style reference for a zero-based column and a
// one-based row, e.g. (0, 1) -> "A1", (27, 3) -> "AB3".
func xlsxCellRef(col, row int) string {
	var letters []byte
	for col++; col > 0; col = (col - 1) / 26 {
		letters = append([]byte{byte('A' + (col-1)%26)}, letters...)
	}
	return string(letters) + strconv.Itoa(row)
}

// xlsxDateSerial converts a date to Excel's serial day number (days since
// 1899-12-30, which absorbs Excel's fictional 1900-02-29).
func xlsxDateSerial(t time.Time) int {
	epoch := time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)
	return int(t.Sub(epoch).Hours() / 24)
}

const xlsxContentTypes = xml.Header + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
	`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
	`<Default Extension="xml" ContentType="application/xml"/>` +
	`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>` +
	`<Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>` +
	`<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>` +
	`</Types>`

const xlsxRootRels = xml.Header + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
	`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>` +
	`</Relationships>`

const xlsxWorkbook = xml.Header + `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">` +
	`<sheets><sheet name="abmctl" sheetId="1" r:id="rId1"/></sheets>` +
	`</workbook>`

const xlsxWorkbookRels = xml.Header + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
	`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>` +
	`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>` +
	`</Relationships>`

// Styles: 0 = default, 1 = bold header, 2 = date shown as yyyy-mm-dd (the
// same format as the table and CSV views).
const xlsxStyles = xml.Header + `<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">` +
	`<numFmts count="1"><numFmt numFmtId="164" formatCode="yyyy-mm-dd"/></numFmts>` +
	`<fonts count="2"><font><sz val="11"/><name val="Calibri"/></font><font><b/><sz val="11"/><name val="Calibri"/></font></fonts>` +
	`<fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills>` +
	`<borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders>` +
	`<cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>` +
	`<cellXfs count="3">` +
	`<xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/>` +
	`<xf numFmtId="0" fontId="1" fillId="0" borderId="0" xfId="0" applyFont="1"/>` +
	`<xf numFmtId="164" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>` +
	`</cellXfs>` +
	`<cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles>` +
	`</styleSheet>`

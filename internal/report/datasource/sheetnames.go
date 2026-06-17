package datasource

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
)

// sheetNames returns the worksheet names of an .xlsx workbook in document order.
// An .xlsx file is a zip archive whose xl/workbook.xml lists the sheets; reading
// it directly is more reliable than DuckDB (the excel extension exposes no
// sheet-listing function).
func sheetNames(path string) ([]string, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("open xlsx: %w", err)
	}
	defer zr.Close()

	for _, f := range zr.File {
		if f.Name != "xl/workbook.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("open workbook.xml: %w", err)
		}
		names, parseErr := parseSheetNames(rc)
		rc.Close()
		return names, parseErr
	}
	return nil, fmt.Errorf("xl/workbook.xml not found")
}

func parseSheetNames(r io.Reader) ([]string, error) {
	var doc struct {
		Sheets struct {
			Sheet []struct {
				Name string `xml:"name,attr"`
			} `xml:"sheet"`
		} `xml:"sheets"`
	}
	if err := xml.NewDecoder(r).Decode(&doc); err != nil {
		return nil, fmt.Errorf("parse workbook.xml: %w", err)
	}
	names := make([]string, 0, len(doc.Sheets.Sheet))
	for _, s := range doc.Sheets.Sheet {
		if s.Name != "" {
			names = append(names, s.Name)
		}
	}
	return names, nil
}

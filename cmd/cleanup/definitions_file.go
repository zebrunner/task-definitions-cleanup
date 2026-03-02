package main

import (
	"encoding/csv"
	"errors"
	"io"
	"os"
	"strings"
)

func loadRecords(path string) ([]*definitionRecord, error) {
	if strings.TrimSpace(path) == "" {
		return make([]*definitionRecord, 0), nil
	}

	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// Missing file is treated as an empty input set.
			return make([]*definitionRecord, 0), nil
		}
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1

	records := make([]*definitionRecord, 0)
	for {
		row, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if len(row) == 0 {
			continue
		}

		id := strings.TrimSpace(row[0])
		if id == "" || strings.EqualFold(id, "Identifier") {
			continue
		}

		rec := &definitionRecord{
			Identifier:       id,
			DeregisterStatus: safeCell(row, 1),
			DeleteStatus:     safeCell(row, 2),
			LastError:        safeCell(row, 3),
		}
		records = append(records, rec)
	}
	return records, nil
}

func writeRecords(path string, records []*definitionRecord) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	if err := w.Write([]string{"Identifier", "deregistered", "deleted"}); err != nil {
		return err
	}
	for _, rec := range records {
		if err := w.Write([]string{rec.Identifier, rec.DeregisterStatus, rec.DeleteStatus}); err != nil {
			return err
		}
	}
	return w.Error()
}

func writeResultRecords(path string, records []*definitionRecord) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	if err := w.Write([]string{"Identifier", "deregistered", "deleted", "error"}); err != nil {
		return err
	}
	for _, rec := range records {
		if err := w.Write([]string{rec.Identifier, rec.DeregisterStatus, rec.DeleteStatus, rec.LastError}); err != nil {
			return err
		}
	}
	return w.Error()
}

func safeCell(row []string, idx int) string {
	if idx >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[idx])
}

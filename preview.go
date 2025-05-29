package main

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/google/uuid"
)

const previewMax = 10

var uploadStore = sync.Map{}

var previewHandler = func(w http.ResponseWriter, r *http.Request) {
	file, header, err := r.FormFile("file")
	if err != nil {
		previewError(w, http.StatusBadRequest, "error reading file: %v", err)
		return
	}
	defer file.Close()
	site := r.FormValue("site")

	if !strings.HasSuffix(header.Filename, ".xlsx") && !strings.HasSuffix(header.Filename, ".xls") {
		previewError(w, http.StatusBadRequest, "only .xsl & .xlsx files are supported")
		return
	}
	buf, err := convertToCSV(file, Site(site), header.Filename)
	if err != nil {
		previewError(w, http.StatusInternalServerError, "failed to parse excel: %v", err)
		return
	}
	reader := csv.NewReader(bytes.NewReader(buf.Bytes()))
	reader.FieldsPerRecord = -1
	var rows [][]string
	for range previewMax {
		row, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			previewError(w, http.StatusInternalServerError, "error reading preview: %v", err)
			return
		}
		rows = append(rows, row)
	}
	token := uuid.NewString()
	uploadStore.Store(token, buf)

	w.Header().Set("Content-Type", "text/html")
	if err = tmpl.ExecuteTemplate(w, "preview", PreviewResult{
		Rows:  rows,
		Token: token,
	}); err != nil {
		previewError(w, http.StatusInternalServerError, "template error: %v", err)
		return
	}
}

type PreviewResult struct {
	Rows  [][]string
	Token string
	Error string
}

func previewError(w http.ResponseWriter, code int, format string, a ...any) {
	res := PreviewResult{
		Error: fmt.Sprintf(format, a...),
	}
	log.Println("preview w/ error:", res.Error)
	if err := tmpl.ExecuteTemplate(w, "preview", res); err != nil {
		log.Printf("TEMPLATE EXECUTION ERROR: %v", err)
	}
	if code > 399 {
		log.Println(res.Error)
	}
}

// returns a preview and an upload buffer
func convertToCSV(file io.Reader, site Site, name string) (*bytes.Buffer, error) {
	tmpDir, err := os.MkdirTemp("", "upload-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)

	inputPath := filepath.Join(tmpDir, name)
	outFile, err := os.Create(inputPath)
	if err != nil {
		return nil, err
	}
	if _, err = io.Copy(outFile, file); err != nil {
		return nil, err
	}
	outFile.Close()

	cmd := exec.Command("libreoffice", "--headless", "--convert-to", "csv", "--outdir", tmpDir, inputPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err = cmd.Run(); err != nil {
		return nil, fmt.Errorf("conversion failed: %v", err)
	}

	csvPath := filepath.Join(tmpDir, replaceExtension(name, ".csv"))
	f, err := os.Open(csvPath)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := f.Close(); err != nil {
			log.Printf("error closing file: %v\n", err)
		}
	}()

	reader := csv.NewReader(f)
	reader.FieldsPerRecord = -1

	// xlsx, err := excelize.OpenReader(file)
	// if err != nil {
	// 	return nil, fmt.Errorf("Error parsing .xlsx file: %v", err)
	// }
	// sheetName := xlsx.GetSheetName(0)
	// rows, err := xlsx.GetRows(sheetName)
	// if err != nil {
	// 	return nil, fmt.Errorf("Error reading sheet: %v", err)
	//
	// }
	buf := new(bytes.Buffer)
	writer := csv.NewWriter(buf)

	rowIndex := 0
	for {
		row, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("error reading csv row: %v", err)
		}
		if isEmpty(row) {
			continue
		}
		if rowIndex > 0 {
			// if i != 0 {
			if err = validateForBQ(row, schemaMap[site]); err != nil {
				return nil, err
			}
		}
		if err = writer.Write(row); err != nil {
			return nil, fmt.Errorf("Error writing CSV: %v", err)
		}
		rowIndex++
	}
	writer.Flush()
	return buf, writer.Error() // just in case :)
}

func replaceExtension(name, newExt string) string {
	return name[:len(name)-len(filepath.Ext(name))] + newExt
}

func isEmpty(row []string) bool {
	for _, cell := range row {
		if strings.TrimSpace(cell) != "" {
			return false
		}
	}
	return true
}

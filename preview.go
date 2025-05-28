package main

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/google/uuid"
)

const previewMax = 25

var uploadStore = sync.Map{}

var previewHandler = func(w http.ResponseWriter, r *http.Request) {
	file, header, err := r.FormFile("file")
	if err != nil {
		previewError(w, http.StatusBadRequest, "error reading file: %v", err)
		return
	}
	defer file.Close()

	if !strings.HasSuffix(header.Filename, ".xlsx") {
		previewError(w, http.StatusBadRequest, "only .xlsx files are supported")
		return
	}
	buf, err := convertToCSV(file)
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

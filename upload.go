package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

type Site string

const (
	Frio      Site = "Frio"
	Medina    Site = "Medina"
	Methodist Site = "Methodist"
	STRIC     Site = "STRIC"
	ValVerde  Site = "Val Verde"
)

var allowedTables = map[Site]struct{}{
	Frio:      {},
	Medina:    {},
	Methodist: {},
	STRIC:     {},
	ValVerde:  {},
}

func getTableList() []string {
	tables := make([]string, 0, len(allowedTables))
	for table := range allowedTables {
		tables = append(tables, string(table))
	}
	slices.Sort(tables)
	return tables
}

var uploadHandler = func(w http.ResponseWriter, r *http.Request) {
	token := r.FormValue("token")
	if token == "" {
		uploadError(w, http.StatusBadRequest, "missing token")
		return
	}
	site := r.FormValue("site")
	if _, ok := allowedTables[Site(site)]; !ok {
		uploadError(w, http.StatusBadRequest, "invalid BigQuery table selected")
		return
	}
	site = strings.ToLower(strings.ReplaceAll(site, " ", "_"))

	value, ok := uploadStore.Load(token)
	if !ok {
		uploadError(w, http.StatusBadRequest, "upload session expired or token invalid")
		return
	}

	csvBuf, ok := value.(*bytes.Buffer)
	if !ok {
		uploadError(w, http.StatusInternalServerError, "Internal error: invalid data type")
		return
	}
	objectName := fmt.Sprintf("upload_%s.csv", token)
	if err := uploadToGCS(ctx, csvBuf, objectName, map[string]string{"site": site}); err != nil {
		uploadError(w, http.StatusInternalServerError, "couldn't upload file: %v", err)
		return
	}

	uploadStore.Delete(token)
	if err := tmpl.ExecuteTemplate(w, "upload", UploadResult{
		Type:    "Success",
		Message: fmt.Sprintf("saved file as %s", objectName),
	}); err != nil {
		uploadError(w, http.StatusInternalServerError, "template error: %v", err)
		return
	}
}

func uploadToGCS(ctx context.Context, buf *bytes.Buffer, name string, metadata map[string]string) error {
	wc := client.Bucket(bucketName).Object(name).NewWriter(ctx)
	wc.ContentType = "text/csv"
	wc.Metadata = metadata

	if _, err := io.Copy(wc, buf); err != nil {
		return fmt.Errorf("failed to write to GCS: %v", err)
	}
	return wc.Close()
}

type bqField struct {
	Name string
	Type string
}

type schema []bqField

var schemaMap = map[Site]schema{
	STRIC: {
		{"AppointmentID", "INTEGER"},
		{"Accession", "STRING"},
		{"AppointmentDate", "DATE"},
		{"Location", "STRING"},
		{"PatientMRN", "STRING"},
		{"PatientLastName", "STRING"},
		{"PatientFirstName", "STRING"},
		{"PatientMiddleName", "STRING"},
		{"Insurance_Plan", "STRING"},
		{"Insurance_PlanSecondary", "STRING"},
		{"Insurance_PlanTertiary", "STRING"},
		{"ReferringPhysicianFirstName", "STRING"},
		{"ReferringPhysicianLastName", "STRING"},
		{"Insurance_SubscriberNumber", "STRING"},
		{"Insurance_SubscriberNumberSecondary", "STRING"},
		{"Insurance_SubscriberNumberTertiary", "STRING"},
		{"ExamResultsStatus", "STRING"},
		{"ExamFinalizedDate", "TIMESTAMP"},
		{"ExamFinalizedDate_hourofday", "INTEGER"},
		{"CPTCode", "STRING"},
		{"CPTDescription", "STRING"},
		{"Exam_ExamCode", "STRING"},
		{"ExamDescriptionDisplay", "STRING"},
	},
}

type fieldError struct {
	field    bqField
	val      string
	savedErr error
}

func (e *fieldError) Error() string {
	return fmt.Sprintf("cannot convert value '%s' in column '%s' to %s: %v", e.val, e.field.Name, e.field.Type, e.savedErr)
}

const excelTimeFormat = "1/2/2006 3:04:05 PM"

func validateForBQ(row []string, schema schema) error {
	if schema == nil {
		return nil
	}
	for i, field := range schema {
		if i >= len(row) {
			return fmt.Errorf("missing value for column %s", field.Name)
		}
		val := row[i]
		if val == "" {
			continue
		}
		switch field.Type {
		case "STRING":
			if i == 1 {
				row[i] = strings.TrimSuffix(val, ".0")
			}
			continue
		case "INTEGER":
			if _, err := strconv.Atoi(val); err != nil {
				return &fieldError{field, val, err}
			}
		case "FLOAT":
			if _, err := strconv.ParseFloat(val, 64); err != nil {
				return &fieldError{field, val, err}
			}
		case "BOOLEAN":
			if _, err := strconv.ParseBool(val); err != nil {
				return &fieldError{field, val, err}
			}
		case "DATE":
			loc, _ := time.LoadLocation("America/Chicago")
			t, err := time.ParseInLocation(excelTimeFormat, val, loc)
			if err != nil {
				return &fieldError{field, val, err}
			}
			row[i] = t.Format("2006-01-02")
		case "TIMESTAMP":
			loc, _ := time.LoadLocation("America/Chicago")
			t, err := time.ParseInLocation(excelTimeFormat, val, loc)
			if err != nil {
				return &fieldError{field, val, err}
			}
			row[i] = t.Format("2006-01-02 15:04:05")
		default:
			return fmt.Errorf("unknown type %s for colmn %s", field.Type, field.Name)
		}
	}
	return nil
}

type UploadResult struct {
	Type    string
	Message string
}

func uploadError(w http.ResponseWriter, code int, format string, a ...any) {
	res := UploadResult{
		Type:    "Error",
		Message: fmt.Sprintf(format, a...),
	}
	log.Println("upload error:", res.Message)
	if err := tmpl.ExecuteTemplate(w, "upload", res); err != nil {
		log.Printf("TEMPLATE EXECUTION ERROR: %v", err)
	}
	if code > 399 {
		log.Println(res.Message)
	}
}

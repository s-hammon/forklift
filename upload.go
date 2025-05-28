package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
)

var uploadHandler = func(w http.ResponseWriter, r *http.Request) {
	token := r.FormValue("token")
	if token == "" {
		http.Error(w, "missing token", http.StatusBadRequest)
		return
	}

	value, ok := uploadStore.Load(token)
	if !ok {
		http.Error(w, "upload session expired or token invalid", http.StatusBadRequest)
		return
	}

	csvBuf, ok := value.(*bytes.Buffer)
	if !ok {
		http.Error(w, "Internal error: invalid data type", http.StatusInternalServerError)
		return
	}
	objectName := fmt.Sprintf("upload_%s.csv", token)
	if err := uploadToGCS(ctx, csvBuf, objectName); err != nil {
		http.Error(w, "Failed to upload to GCS: "+err.Error(), http.StatusInternalServerError)
		return
	}

	uploadStore.Delete(token)
}

// returns a preview and an upload buffer
func convertToCSV(file io.Reader) (*bytes.Buffer, error) {
	xlsx, err := excelize.OpenReader(file)
	if err != nil {
		return nil, fmt.Errorf("Error parsing .xlsx file: %v", err)
	}
	sheetName := xlsx.GetSheetName(0)
	rows, err := xlsx.GetRows(sheetName)
	if err != nil {
		return nil, fmt.Errorf("Error reading sheet: %v", err)

	}
	buf := new(bytes.Buffer)
	writer := csv.NewWriter(buf)

	for i, row := range rows {
		if i != 0 {
			if err = validateForBQ(row); err != nil {
				return nil, err
			}
		}
		if err = writer.Write(row); err != nil {
			return nil, fmt.Errorf("Error writing CSV: %v", err)
		}
	}
	writer.Flush()
	return buf, writer.Error() // just in case :)
}

func uploadToGCS(ctx context.Context, buf *bytes.Buffer, objectName string) error {
	wc := client.Bucket(bucketName).Object(objectName).NewWriter(ctx)
	wc.ContentType = "text/csv"

	if _, err := io.Copy(wc, buf); err != nil {
		return fmt.Errorf("failed to write to GCS: %v", err)
	}
	if err := wc.Close(); err != nil {
		return fmt.Errorf("error closing conn for upload: %v", err)
	}
	return nil
}

type bqField struct {
	Name string
	Type string
}

var schema = []bqField{
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

func validateForBQ(row []string) error {
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

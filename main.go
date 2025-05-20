package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/storage"
	"github.com/xuri/excelize/v2"
)

var (
	bucketName string
	objectName string

	ctx    context.Context
	client *storage.Client
)

func init() {
	ctx = context.Background()
	var err error

	bucketName = os.Getenv("GCS_BUCKET_NAME")
	if bucketName == "" {
		log.Fatal("name for GCS bucket not provided")
	}

	client, err = storage.NewClient(ctx)
	if err != nil {
		log.Fatalf("couldn't start GCS client: %v", err)
	}
}

func main() {
	http.HandleFunc("/upload", uploadHandler)

	fmt.Println("starting file server")
	fs := http.FileServer(http.Dir("static"))
	http.Handle("/", fs)
	fmt.Println("file server loaded")

	fmt.Println("listening on :8080")
	http.ListenAndServe(":8080", nil)
}

var uploadHandler = func(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Error reading file: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer file.Close()

	if !strings.HasSuffix(header.Filename, ".xlsx") {
		http.Error(w, "Only .xlsx files are supported", http.StatusBadRequest)
		return
	}
	csvBuf, err := convertToCSV(file)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err = uploadToGCS(ctx, csvBuf, header.Filename); err != nil {
		http.Error(w, "Could not upload to GCS: "+err.Error(), http.StatusInternalServerError)
	}
	w.Write([]byte("success!"))
}

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
	csvBuf := bytes.NewBuffer(nil)
	writer := csv.NewWriter(csvBuf)
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
	return csvBuf, writer.Error() // just in case :)
}

func uploadToGCS(ctx context.Context, buf *bytes.Buffer, name string) error {
	client, err := storage.NewClient(ctx)
	if err != nil {
		return err
	}
	defer client.Close()

	objectName = strings.TrimSuffix(name, ".xlsx") + ".csv"
	wc := client.Bucket(bucketName).Object(objectName).NewWriter(ctx)
	wc.ContentType = "text/csv"

	if _, err = io.Copy(wc, buf); err != nil {
		return err
	}
	return wc.Close()
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

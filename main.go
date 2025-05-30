package main

import (
	"context"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"os/exec"

	"cloud.google.com/go/storage"
)

var (
	bucketName string

	ctx    context.Context
	client *storage.Client

	tmpl *template.Template
)

func init() {
	var err error
	if _, err = exec.LookPath("libreoffice"); err != nil {
		log.Fatal("'libreoffice' is not in PATH")
	}
	bucketName = os.Getenv("GCS_BUCKET_NAME")
	if bucketName == "" {
		log.Fatal("name for GCS bucket not provided")
	}

	tmpl, err = template.New("").ParseGlob("templates/*.html")
	if err != nil {
		log.Fatalf("TEMPLATE PARSE ERROR: %v", err)
	}

	if tmpl.Lookup("preview") == nil {
		log.Fatal("missing preview template")
	}
	if tmpl.Lookup("error") == nil {
		log.Fatal("missing error template")
	}
}

func initGCS() {
	ctx = context.Background()

	var err error
	client, err = storage.NewClient(ctx)
	if err != nil {
		log.Fatalf("couldn't start GCS client: %v", err)
	}
}

func main() {
	initGCS()
	defer client.Close()

	mux := http.NewServeMux()

	mux.HandleFunc("/", Homepage)
	mux.HandleFunc("POST /preview", previewHandler)
	mux.HandleFunc("GET /cancel", cancelHandler)
	mux.HandleFunc("POST /upload", uploadHandler)

	fmt.Println("listening on :8080")
	mux.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(http.Dir("assets"))))
	http.ListenAndServe(":8080", mux)
}

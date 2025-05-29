package main

import "net/http"

func Homepage(w http.ResponseWriter, r *http.Request) {
	data := struct {
		BQTables []string
	}{getTableList()}
	tmpl.ExecuteTemplate(w, "home", data)
}

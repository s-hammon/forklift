package main

import "net/http"

func Homepage(w http.ResponseWriter, r *http.Request) {
	tmpl.ExecuteTemplate(w, "home", nil)
}

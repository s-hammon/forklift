package main

import "net/http"

var cancelHandler = func(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte(""))
}

package main

import (
	"fmt"
	"net/http"
)

// yo dog, I heard you like functional programming
// so I wrapped your function in a function
// so that you can procedurally map an int to the front-end
// using middleware :^)
var cache = func(seconds int) func(http.Handler) http.Handler {
	return func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", fmt.Sprintf("max-age=%d", seconds))
			h.ServeHTTP(w, r)
		})
	}
}

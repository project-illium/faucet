// Copyright (c) 2022 The illium developers
// Use of this source code is governed by an MIT
// license that can be found in the LICENSE file.
package main

import (
	"net/http"
)

func main() {
	// Handle the root route with the index.html file
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "index.html")
	})

	// Serve the app.js file
	http.Handle("/index.js", http.FileServer(http.Dir(".")))
	http.Handle("/styles.css", http.FileServer(http.Dir(".")))

	http.ListenAndServe(":8080", nil)
}

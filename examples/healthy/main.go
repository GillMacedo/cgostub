// Command healthy is a made-up HTTP service for the cgostub examples. It
// uses only pure Go packages, so nothing outside the standard library
// changes without cgo.
package main

import (
	"encoding/json"
	"log"
	"net/http"
)

func main() {
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	log.Fatal(http.ListenAndServe("127.0.0.1:8080", nil))
}

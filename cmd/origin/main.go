package main

import (
	"fmt"
	"log"
	"net/http"
	"time"
)

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(20 * time.Millisecond)
		fmt.Fprintf(w, "Hello from origin server %s! You requested: %s\n", r.Host, r.URL.Path)
	})
	log.Println("Origin server listening on :8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatal(err)
	}
}

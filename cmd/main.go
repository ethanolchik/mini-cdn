package main

import (
	"log"
	"net/http"

	"github.com/ethanolchik/mini-cdn/internal/proxy"
)

func main() {
	p := proxy.New([]string{"http://localhost:8080"})

	log.Println("Proxy server listening on :8081")
	if err := http.ListenAndServe(":8081", p); err != nil {
		log.Fatal(err)
	}
}

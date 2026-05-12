package main

import (
	"log"
	"net/http"
	"os"

	"github.com/ethanolchik/mini-cdn/internal/balancer"
	"github.com/ethanolchik/mini-cdn/internal/proxy"
)

func main() {
	port := os.Args[1]
	origins := os.Args[2:]
	if len(origins) == 0 {
		origins = []string{"http://localhost:8081", "http://localhost:8082", "http://localhost:8083", "http://localhost:8084", "http://localhost:8085"}
	}
	lb := balancer.New(origins)

	p := proxy.New(lb)

	log.Printf("Proxy server listening on %s", port)
	if err := http.ListenAndServe(":"+port, p); err != nil {
		log.Fatal(err)
	}
}

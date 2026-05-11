package main

import (
	"log"
	"net/http"

	"github.com/ethanolchik/mini-cdn/internal/balancer"
	"github.com/ethanolchik/mini-cdn/internal/proxy"
)

func main() {
	origins := []string{"http://localhost:8080"}
	lb := balancer.New(origins)

	p := proxy.New(lb)

	log.Println("Proxy server listening on :8081")
	if err := http.ListenAndServe(":8081", p); err != nil {
		log.Fatal(err)
	}
}

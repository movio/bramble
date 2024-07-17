//go:generate go run github.com/99designs/gqlgen
package main

import (
	_ "embed"
	"fmt"
	"log"
	"net/http"
	"os"
)

func main() {
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}

	http.Handle("/query", newResolver())
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "OK")
	})
	log.Printf("example %s running on %s", name, addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}

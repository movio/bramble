package main

import (
	_ "embed"
	"fmt"
	"net/http"
	"os"

	"log"
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
	http.ListenAndServe(addr, nil)
}

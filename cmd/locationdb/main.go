package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/ruckstackapp/locationdb"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	dataDir := flag.String("data-dir", "./data", "data directory")
	flag.Parse()

	app, err := locationdb.NewApp(*dataDir)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("locationdb listening on %s", *addr)
	if err := http.ListenAndServe(*addr, app.Handler()); err != nil {
		log.Fatal(err)
	}
}

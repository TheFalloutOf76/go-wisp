package main

import (
	"fmt"
	"net/http"

	"go-wisp/wisp"
)

func main() {
	wispHandler := wisp.CreateWispHandler(wisp.WispConfig{
		DefaultBufferRemaining: 255,
	})
	http.HandleFunc("/", wispHandler)
	fmt.Println("starting wisp server on port 8080...")
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		fmt.Printf("failed to start server: %v", err)
	}
}

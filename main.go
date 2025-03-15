package main

import (
	"bufio"
	"fmt"
	"net/http"
	"strings"

	"go-wisp/wisp"
)

const blocklistURL = "https://raw.githubusercontent.com/hagezi/dns-blocklists/main/domains/pro.txt"

func getBlocklist(url string) (map[string]struct{}, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("failed to fetch blocklist, status code: %d", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	blocklist := make(map[string]struct{})
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		blocklist[line] = struct{}{}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return blocklist, nil
}

func main() {
	blocklist, err := getBlocklist(blocklistURL)
	if err != nil {
		blocklist = make(map[string]struct{})
		fmt.Printf("failed to fetch blocklist: %v\n", err)
	}

	wispHandler := wisp.CreateWispHandler(wisp.Config{
		BufferRemainingLength: 255,
		Blacklist: struct {
			Hostnames map[string]struct{}
		}{
			Hostnames: blocklist,
		},
		DisableUDP: true,
	})
	http.HandleFunc("/", wispHandler)
	fmt.Println("starting wisp server on port 8080. . .")
	err = http.ListenAndServe(":8080", nil)
	if err != nil {
		fmt.Printf("failed to start server: %v", err)
	}
}

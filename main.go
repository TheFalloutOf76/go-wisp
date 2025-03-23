package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"go-wisp/wisp"
)

type Config struct {
	Port                  string `json:"port"`
	DisableUDP            bool   `json:"disableUDP"`
	TcpBufferSize         int    `json:"tcpBufferSize"`
	BufferRemainingLength uint32 `json:"bufferRemainingLength"`
	TcpNoDelay            bool   `json:"tcpNoDelay"`
	WebsocketTcpNoDelay   bool   `json:"websocketTcpNoDelay"`
	Blacklist             struct {
		Hostnames struct {
			FetchFromUrl string   `json:"fetchFromUrl"`
			Include      []string `json:"include"`
			Exclude      []string `json:"exclude"`
		} `json:"hostnames"`
	} `json:"blacklist"`
	Proxy string `json:"proxy"`
}

func getBlocklistFromUrl(url string) (map[string]struct{}, error) {
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

func loadConfig(filename string) (Config, error) {
	file, err := os.Open(filename)
	if err != nil {
		return Config{}, err
	}
	defer file.Close()

	var cfg Config
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func createWispConfig(cfg Config) *wisp.Config {
	blocklist := make(map[string]struct{})
	fetchURL := cfg.Blacklist.Hostnames.FetchFromUrl
	if fetchURL != "" {
		bl, err := getBlocklistFromUrl(fetchURL)
		if err != nil {
			fmt.Printf("failed to fetch blocklist from URL: %v\n", err)
		} else {
			blocklist = bl
		}
	}

	for _, host := range cfg.Blacklist.Hostnames.Include {
		blocklist[host] = struct{}{}
	}

	for _, host := range cfg.Blacklist.Hostnames.Exclude {
		delete(blocklist, host)
	}

	return &wisp.Config{
		DisableUDP:            cfg.DisableUDP,
		TcpBufferSize:         cfg.TcpBufferSize,
		BufferRemainingLength: cfg.BufferRemainingLength,
		TcpNoDelay:            cfg.TcpNoDelay,
		WebsocketTcpNoDelay:   cfg.WebsocketTcpNoDelay,
		Blacklist: struct {
			Hostnames map[string]struct{}
		}{
			Hostnames: blocklist,
		},
		Proxy: cfg.Proxy,
	}
}

func main() {
	cfg, err := loadConfig("config.json")
	if err != nil {
		fmt.Printf("failed to load config: %v", err)
		return
	}
	wispConfig := createWispConfig(cfg)

	wispHandler := wisp.CreateWispHandler(wispConfig)

	http.HandleFunc("/", wispHandler)
	fmt.Printf("starting wisp server on port %s. . .", cfg.Port)
	err = http.ListenAndServe(":"+cfg.Port, nil)
	if err != nil {
		fmt.Printf("failed to start server: %v", err)
	}
}

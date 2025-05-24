package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"

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
		Hostnames []string `json:"hostnames"`
	} `json:"blacklist"`
	Whitelist struct {
		Hostnames []string `json:"hostnames"`
	} `json:"whitelist"`
	Proxy                      string `json:"proxy"`
	WebsocketPermessageDeflate bool   `json:"websocketPermessageDeflate"`
	DnsServer                  string `json:"dnsServer"`
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
	blacklistedHostnames := make(map[string]struct{})
	for _, host := range cfg.Blacklist.Hostnames {
		blacklistedHostnames[host] = struct{}{}
	}

	whitelistedHostnames := make(map[string]struct{})
	for _, host := range cfg.Whitelist.Hostnames {
		whitelistedHostnames[host] = struct{}{}
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
			Hostnames: blacklistedHostnames,
		},
		Whitelist: struct {
			Hostnames map[string]struct{}
		}{
			Hostnames: whitelistedHostnames,
		},
		Proxy:                      cfg.Proxy,
		WebsocketPermessageDeflate: cfg.WebsocketPermessageDeflate,
		DnsServer:                  cfg.DnsServer,
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

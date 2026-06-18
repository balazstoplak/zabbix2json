package main

import (
	"flag"
	"log"
	"net/http"
)

func main() {
	configPath := flag.String("config", "/etc/zabbix2json.yaml", "path to YAML config")
	flag.Parse()

	cfg, err := Load(*configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if cfg.URL == "" || cfg.Token == "" {
		log.Fatal("config: url and token are required (set in YAML or ZABBIX_URL/ZABBIX_TOKEN)")
	}

	client := NewHTTPClient(cfg.URL, cfg.Token, cfg.Timeout)
	srv := NewServer(client, cfg)

	mux := http.NewServeMux()
	mux.HandleFunc("/cmd.cgi", srv.handleCmd)
	mux.HandleFunc("/", srv.handleStatus)

	log.Printf("zabbix2json listening on %s -> %s", cfg.Listen, cfg.URL)
	if err := http.ListenAndServe(cfg.Listen, mux); err != nil {
		log.Fatalf("server: %v", err)
	}
}

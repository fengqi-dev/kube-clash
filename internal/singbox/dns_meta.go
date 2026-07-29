package singbox

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type dnsMeta struct {
	Listen  string   `json:"listen"`
	Port    int      `json:"port"`
	Domains []string `json:"domains"`
	Search  []string `json:"search"`
	Ndots   int      `json:"ndots"`
}

func loadDNSMeta(workDir string) (dnsMeta, error) {
	content, err := os.ReadFile(filepath.Join(workDir, "dns-meta.json"))
	if err != nil {
		return dnsMeta{}, err
	}
	var meta dnsMeta
	if err := json.Unmarshal(content, &meta); err != nil {
		return dnsMeta{}, err
	}
	if meta.Listen == "" {
		meta.Listen = DefaultDNSListen
	}
	if meta.Port == 0 {
		meta.Port = DefaultDNSPort
	}
	if len(meta.Domains) == 0 {
		meta.Domains = ResolverDomains("")
	}
	if len(meta.Search) == 0 {
		meta.Search = SearchDomains("default")
	}
	if meta.Ndots <= 0 {
		meta.Ndots = 5
	}
	return meta, nil
}

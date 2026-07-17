package geoip

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"
)

var httpClient = &http.Client{Timeout: 2 * time.Second}

type GeoInfo struct {
	Country string `json:"country"`
	Region  string `json:"regionName"`
}

func Lookup(ip string) (*GeoInfo, error) {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil || parsedIP.IsPrivate() || parsedIP.IsLoopback() {
		return &GeoInfo{Country: "Local", Region: "Local"}, nil
	}

	resp, err := httpClient.Get(fmt.Sprintf("http://ip-api.com/json/%s?fields=country,regionName", ip))
	if err != nil {
		return &GeoInfo{Country: "Unknown", Region: "Unknown"}, nil
	}
	defer resp.Body.Close()

	var info GeoInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return &GeoInfo{Country: "Unknown", Region: "Unknown"}, nil
	}
	return &info, nil
}

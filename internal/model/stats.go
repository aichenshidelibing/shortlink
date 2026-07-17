package model

import "time"

type LinkStats struct {
	ShortCode       string        `json:"short_code"`
	TotalClicks     int64         `json:"total_clicks"`
	UniqueCountries int           `json:"unique_countries"`
	UniqueVisitors  int64         `json:"unique_visitors"`
	Timeline        []DailyStat   `json:"timeline"`
	Geo             []GeoStat     `json:"geo"`
	Referrers       []GroupedStat `json:"referrers"`
	Devices         []GroupedStat `json:"devices"`
	Browsers        []GroupedStat `json:"browsers"`
	OS              []GroupedStat `json:"os"`
	Hours           []HourlyStat  `json:"hours"`
}

type DailyStat struct {
	Date  string `json:"date"`
	Count int64  `json:"count"`
}

type GeoStat struct {
	Country string `json:"country"`
	Count   int64  `json:"count"`
}

type GroupedStat struct {
	Name  string `json:"name"`
	Count int64  `json:"count"`
}

type HourlyStat struct {
	Hour  int   `json:"hour"`
	Count int64 `json:"count"`
}

type RealtimeClick struct {
	Country string    `json:"country"`
	Region  string    `json:"region"`
	Time    time.Time `json:"time"`
}

package gpsd

import "time"

// source: https://gpsd.gitlab.io/gpsd/gpsd_json.html
type GPSPosition struct {
	Class  string    `json:"class"`
	Mode   int       `json:"mode"`
	Device string    `json:"device,omitempty"`
	Time   time.Time `json:"time,omitempty"`
	Ept    float64   `json:"ept,omitempty"`
	Lat    float64   `json:"lat,omitempty"`
	Lon    float64   `json:"lon,omitempty"`
	Alt    float64   `json:"alt,omitempty"`
	Eph    float64   `json:"eph,omitempty"`
	Epv    float64   `json:"epv,omitempty"`
	Track  float64   `json:"track,omitempty"`
	Speed  float64   `json:"speed,omitempty"`
	Climb  float64   `json:"climb,omitempty"`
}

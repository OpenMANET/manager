package sysupgrade

import "time"

// Asset is a single binary file attached to a GitHub release.
type Asset struct {
	Name        string
	DownloadURL string
	ContentType string
	SizeBytes   int64
}

// Release is a single OpenMANET firmware release.
type Release struct {
	PublishedAt time.Time
	Tag         string
	Name        string
	Body        string
	Version     string // canonical X.Y.Z[-pre]; empty when tag does not parse
	Assets      []Asset
	Prerelease  bool
}

// Update is a single available upgrade target — a release combined with
// the asset that matches the local hardware.
type Update struct {
	MatchedAsset     Asset
	Release          Release
	NewerThanCurrent bool
}

// Manifest is the optional manifest.json shipped inside a release. When
// present, it is the authoritative mapping from a board's
// /etc/board.json id to the asset filename for that board.
type Manifest struct {
	Boards map[string]string `json:"boards"`
}

// StagedImage is metadata for a firmware image that has been uploaded
// out-of-band (POST /api/sysupgrade/upload) and is sitting on disk
// waiting to be flashed. The Manager tracks at most one staged image
// at a time.
//
// Compatibility is decided by parsing the OpenWrt FWx0 metadata trailer
// (CompatVersion / SupportedDevices) and comparing against DeviceCompat
// — see imagemeta.go. Filename pattern matching is no longer used.
type StagedImage struct {
	UploadedAt       time.Time
	CompatMessage    string
	Filename         string
	Sha256           string
	CompatVersion    string
	Path             string
	DeviceCompat     string
	PreflightError   string
	SupportedDevices []string
	SizeBytes        int64
	MetadataPresent  bool
	ImageCompatible  bool
	PreflightOK      bool
}

// SystemInfo is the rich system metadata returned by Manager.GetSystemInfo.
type SystemInfo struct {
	Hostname                string
	Distribution            string
	Release                 string
	Revision                string
	Target                  string
	BoardName               string
	Model                   string
	Description             string
	OpenmanetVersion        string
	Kernel                  string
	Architecture            string
	BuildDate               string
	SysupgradeCapableReason string
	RootfsType              string
	SysupgradeCapable       bool
}

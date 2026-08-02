package tak

import (
	"encoding/xml"
	"fmt"
	"strings"
	"time"

	"github.com/coreywagehoft/go-tak/pkg/cot"
	"github.com/coreywagehoft/go-tak/pkg/cotproto"
)

const (
	radioUnitType    = "a-f-G-U-U-S-R"
	cameraSensorType = "b-m-p-s-p-loc"
	videoSourceType  = "b-i-v"
	defaultStaleTime = 5 * time.Minute
)

// Position is the GNSS data required to locate a TAK event.
type Position struct {
	Altitude        float64
	Ce              float64
	GeoidSeparation float64
	Lat             float64
	Le              float64
	Lon             float64
	Speed           float64
	Track           float64
}

// Node identifies the OpenMANET node whose events are being advertised.
type Node struct {
	Callsign string
	Platform string
	UID      string
}

// BuildNodeMessages returns the node's ordinary radio event when no camera
// stream is available. When a stream is present, it returns a video-source
// event and a separately clickable camera sensor event instead, so ATAK shows
// one node marker rather than both a radio marker and a camera marker. The
// sensor refers to the video event by stable UID.
func BuildNodeMessages(now time.Time, position Position, node Node, stream *CameraStream) ([]*cotproto.TakMessage, error) {
	node = normalizeNode(node)
	if stream == nil {
		return []*cotproto.TakMessage{buildRadioMessage(now, position, node)}, nil
	}

	video, err := buildVideoMessage(now, position, node, *stream)
	if err != nil {
		return nil, err
	}

	sensor, err := buildSensorMessage(now, position, node)
	if err != nil {
		return nil, err
	}

	return []*cotproto.TakMessage{video, sensor}, nil
}

func normalizeNode(node Node) Node {
	originalUID := node.UID
	if !strings.Contains(strings.ToLower(node.UID), "manet") {
		node.UID += "-MANET"
	}

	switch node.Callsign {
	case "", originalUID:
		node.Callsign = node.UID
	}

	if node.Platform == "" {
		node.Platform = "OpenMANET"
	}

	return node
}

func buildRadioMessage(now time.Time, position Position, node Node) *cotproto.TakMessage {
	return message(now, position, radioUnitType, node.UID, node.Callsign, &cotproto.Detail{
		Contact: &cotproto.Contact{Callsign: node.Callsign},
		Group:   &cotproto.Group{Name: "Magenta", Role: "MANET Radio"},
		Takv: &cotproto.Takv{
			Device:   node.UID,
			Platform: fmt.Sprintf("%s (OpenMANET)", node.Platform),
		},
	})
}

func buildVideoMessage(now time.Time, position Position, node Node, stream CameraStream) (*cotproto.TakMessage, error) {
	videoUID := node.UID + "-video"

	detail, err := videoDetail(stream, videoUID, node.Callsign+" Video")
	if err != nil {
		return nil, err
	}

	return message(now, position, videoSourceType, videoUID, node.Callsign+" Video", &cotproto.Detail{
		XmlDetail: detail,
		Contact:   &cotproto.Contact{Callsign: node.Callsign + " Video"},
	}), nil
}

func buildSensorMessage(now time.Time, position Position, node Node) (*cotproto.TakMessage, error) {
	detail, err := sensorDetail(node.UID + "-video")
	if err != nil {
		return nil, err
	}

	return message(now, position, cameraSensorType, node.UID+"-camera", node.Callsign+" Camera", &cotproto.Detail{
		XmlDetail: detail,
		Contact:   &cotproto.Contact{Callsign: node.Callsign + " Camera"},
	}), nil
}

func message(now time.Time, position Position, eventType, uid, callsign string, detail *cotproto.Detail) *cotproto.TakMessage {
	hae := position.Altitude
	if position.GeoidSeparation != 0 {
		hae += position.GeoidSeparation
	}

	detail.Track = &cotproto.Track{Speed: position.Speed, Course: position.Track}

	detail.PrecisionLocation = &cotproto.PrecisionLocation{Geopointsrc: "GPS", Altsrc: "GPS"}
	if detail.Contact == nil {
		detail.Contact = &cotproto.Contact{Callsign: callsign}
	}

	return &cotproto.TakMessage{CotEvent: &cotproto.CotEvent{
		Type:      eventType,
		Uid:       uid,
		SendTime:  cot.TimeToMillis(now),
		StartTime: cot.TimeToMillis(now),
		StaleTime: cot.TimeToMillis(now.Add(defaultStaleTime)),
		How:       cot.HowDefault,
		Lat:       position.Lat,
		Lon:       position.Lon,
		Hae:       hae,
		Ce:        position.Ce,
		Le:        position.Le,
		Detail:    detail,
	}}
}

type videoXML struct {
	XMLName    xml.Name           `xml:"__video"`
	UID        string             `xml:"uid,attr"`
	URL        string             `xml:"url,attr"`
	Connection connectionEntryXML `xml:"ConnectionEntry"`
}

type connectionEntryXML struct {
	Address  string `xml:"address,attr"`
	Alias    string `xml:"alias,attr"`
	Path     string `xml:"path,attr"`
	Protocol string `xml:"protocol,attr"`
	UID      string `xml:"uid,attr"`

	BufferTime        int  `xml:"bufferTime,attr"`
	NetworkTimeout    int  `xml:"networkTimeout,attr"`
	Port              int  `xml:"port,attr"`
	RoverPort         int  `xml:"roverPort,attr"`
	RTSPReliable      int  `xml:"rtspReliable,attr"`
	IgnoreEmbeddedKLV bool `xml:"ignoreEmbeddedKLV,attr"`
}

type sensorXML struct {
	XMLName xml.Name `xml:"sensor"`
	HideFOV bool     `xml:"hideFov,attr"`
}

type sensorVideoXML struct {
	XMLName xml.Name `xml:"__video"`
	UID     string   `xml:"uid,attr"`
}

func videoDetail(stream CameraStream, uid, alias string) (string, error) {
	data, err := xml.Marshal(videoXML{
		UID: uid,
		URL: stream.URL(),
		Connection: connectionEntryXML{
			Address:           stream.Address,
			Alias:             alias,
			BufferTime:        -1,
			IgnoreEmbeddedKLV: false,
			NetworkTimeout:    12000,
			Path:              stream.Path,
			Port:              stream.Port,
			Protocol:          rtspScheme,
			RoverPort:         -1,
			RTSPReliable:      0,
			UID:               uid,
		},
	})
	if err != nil {
		return "", fmt.Errorf("marshal video detail: %w", err)
	}

	return string(data), nil
}

func sensorDetail(videoUID string) (string, error) {
	sensor, err := xml.Marshal(sensorXML{HideFOV: true})
	if err != nil {
		return "", fmt.Errorf("marshal sensor detail: %w", err)
	}

	video, err := xml.Marshal(sensorVideoXML{UID: videoUID})
	if err != nil {
		return "", fmt.Errorf("marshal sensor video detail: %w", err)
	}

	return string(sensor) + string(video), nil
}

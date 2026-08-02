package tak

import (
	"bufio"
	"bytes"
	"encoding/xml"
	"testing"
	"time"

	"github.com/coreywagehoft/go-tak/pkg/cot"
)

func TestBuildNodeMessagesWithoutCameraPreservesRadioMarker(t *testing.T) {
	t.Parallel()

	messages, err := BuildNodeMessages(testTime(), testPosition(), Node{UID: "node-1", Callsign: "node-1", Platform: "Raspberry Pi"}, nil)
	if err != nil {
		t.Fatalf("BuildNodeMessages() error = %v", err)
	}

	if len(messages) != 1 {
		t.Fatalf("message count = %d, want 1", len(messages))
	}

	event := messages[0].GetCotEvent()
	if event.GetType() != DefaultCoTType || event.GetUid() != "node-1-MANET" {
		t.Fatalf("radio event = type %q uid %q", event.GetType(), event.GetUid())
	}

	if event.GetHae() != 65 || event.GetDetail().GetXmlDetail() != "" {
		t.Fatalf("radio event HAE/detail = %v/%q", event.GetHae(), event.GetDetail().GetXmlDetail())
	}

	if event.GetDetail().GetContact().GetCallsign() != "node-1-MANET" || event.GetDetail().GetTakv().GetPlatform() != "Raspberry Pi (OpenMANET)" {
		t.Fatalf("radio detail = %+v", event.GetDetail())
	}
}

func TestBuildNodeMessagesUsesCoTTypeOverride(t *testing.T) {
	t.Parallel()

	messages, err := BuildNodeMessages(testTime(), testPosition(), Node{
		UID:     "node-1",
		CoTType: "a-f-G-U-C",
	}, nil)
	if err != nil {
		t.Fatalf("BuildNodeMessages() error = %v", err)
	}

	if got := messages[0].GetCotEvent().GetType(); got != "a-f-G-U-C" {
		t.Fatalf("event type = %q, want override %q", got, "a-f-G-U-C")
	}
}

func TestBuildNodeMessagesWithCameraReplacesRadioMarker(t *testing.T) {
	t.Parallel()

	stream := &CameraStream{Address: "10.41.0.1", Port: 8554, Path: "/rpicamera"}

	messages, err := BuildNodeMessages(testTime(), testPosition(), Node{UID: "node-1", Callsign: "NODE & 1"}, stream)
	if err != nil {
		t.Fatalf("BuildNodeMessages() error = %v", err)
	}

	if len(messages) != 2 {
		t.Fatalf("message count = %d, want 2", len(messages))
	}

	video, sensor := messages[0].GetCotEvent(), messages[1].GetCotEvent()
	if video.GetType() != videoSourceType || video.GetUid() != "node-1-MANET-video" {
		t.Fatalf("video = type %q uid %q", video.GetType(), video.GetUid())
	}

	if sensor.GetType() != cameraSensorType || sensor.GetUid() != "node-1-MANET-camera" {
		t.Fatalf("sensor = type %q uid %q", sensor.GetType(), sensor.GetUid())
	}

	var videoDetail struct {
		Video struct {
			UID        string `xml:"uid,attr"`
			URL        string `xml:"url,attr"`
			Connection struct {
				Address  string `xml:"address,attr"`
				Alias    string `xml:"alias,attr"`
				Path     string `xml:"path,attr"`
				Port     int    `xml:"port,attr"`
				Protocol string `xml:"protocol,attr"`
				UID      string `xml:"uid,attr"`
			} `xml:"ConnectionEntry"`
		} `xml:"__video"`
	}
	decodeDetail(t, video.GetDetail().GetXmlDetail(), &videoDetail)

	if videoDetail.Video.UID != video.GetUid() || videoDetail.Video.Connection.UID != video.GetUid() {
		t.Fatalf("video UIDs = %q/%q, want %q", videoDetail.Video.UID, videoDetail.Video.Connection.UID, video.GetUid())
	}

	if videoDetail.Video.URL != "rtsp://10.41.0.1:8554/rpicamera" ||
		videoDetail.Video.Connection.Address != "10.41.0.1" ||
		videoDetail.Video.Connection.Path != "/rpicamera" ||
		videoDetail.Video.Connection.Port != 8554 ||
		videoDetail.Video.Connection.Protocol != rtspScheme ||
		videoDetail.Video.Connection.Alias != "NODE & 1 Video" {
		t.Fatalf("unexpected video detail: %+v", videoDetail.Video)
	}

	var sensorDetail struct {
		Sensor struct {
			HideFOV bool `xml:"hideFov,attr"`
		} `xml:"sensor"`
		Video struct {
			UID string `xml:"uid,attr"`
		} `xml:"__video"`
	}
	decodeDetail(t, sensor.GetDetail().GetXmlDetail(), &sensorDetail)

	if !sensorDetail.Sensor.HideFOV || sensorDetail.Video.UID != video.GetUid() {
		t.Fatalf("unexpected sensor detail: %+v", sensorDetail)
	}
}

func TestBuildNodeMessagesRoundTripsAsMeshPackets(t *testing.T) {
	t.Parallel()

	messages, err := BuildNodeMessages(testTime(), testPosition(), Node{UID: "node-1", Callsign: "NODE-1"}, &CameraStream{Address: "10.41.0.1", Port: 554, Path: "/rpicamera"})
	if err != nil {
		t.Fatalf("BuildNodeMessages() error = %v", err)
	}

	if len(messages) != 2 {
		t.Fatalf("message count = %d, want 2", len(messages))
	}

	for _, message := range messages {
		packet, err := cot.MakeProtoMeshPacketV1(message)
		if err != nil {
			t.Fatalf("MakeProtoMeshPacketV1() error = %v", err)
		}

		decoded, version, err := cot.ReadProtoMesh(bufio.NewReader(bytes.NewReader(packet)))
		if err != nil {
			t.Fatalf("ReadProtoMesh() error = %v", err)
		}

		if version != cot.ProtoVersion1 || decoded.GetCotEvent().GetUid() != message.GetCotEvent().GetUid() || decoded.GetCotEvent().GetType() != message.GetCotEvent().GetType() {
			t.Fatalf("decoded event = %+v, version = %d", decoded.GetCotEvent(), version)
		}
	}
}

func decodeDetail(t *testing.T, detail string, value any) {
	t.Helper()

	if err := xml.Unmarshal([]byte("<detail>"+detail+"</detail>"), value); err != nil {
		t.Fatalf("decode detail %q: %v", detail, err)
	}
}

func testTime() time.Time {
	return time.Date(2026, time.July, 31, 12, 0, 0, 0, time.UTC)
}

func testPosition() Position {
	return Position{Lat: 37.7749, Lon: -122.4194, Altitude: 50, GeoidSeparation: 15, Ce: 2, Le: 3, Speed: 5, Track: 90}
}

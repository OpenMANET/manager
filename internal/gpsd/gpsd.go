/*
Package gpsd is a streaming client for GPSD's JSON service and as such can be used only in
async manner unlike clients for other languages which support both async and sync modes.
*/
package gpsd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

const (
	// DefaultAddress of gpsd
	DefaultAddress = "localhost:2947"

	WatchCommand   = "WATCH"
	PollCommand    = "POLL"
	VersionCommand = "VERSION"
)

// Available gpsd messages formats.
const (
	formatJSON = "json"
	formatNMEA = "nmea"
)

// Message classes.
const (
	msgClassDevices = "DEVICES"
	msgClassPPS     = "PPS"
	msgClassError   = "ERROR"
	msgClassVersion = "VERSION"
	msgClassTPV     = "TPV"
	msgClassSKY     = "SKY"
	msgClassGST     = "GST"
	msgClassATT     = "ATT"
)

// Filter is a gpsd entry filter function (aka watcher or subscriber)
type Filter func(interface{})

// Session represents a connection to gpsd
type Session struct {
	address string
	socket  net.Conn
	reader  *bufio.Reader
	filters map[string][]Filter
	done    chan struct{}
}

// Dial opens a new connection to GPSD.
func Dial(address string) (*Session, error) {
	s := &Session{address: address}
	if err := s.dial(); err != nil {
		return nil, err
	}
	s.filters = make(map[string][]Filter)

	return s, nil
}

func (s *Session) dial() error {
	conn, err := net.Dial("tcp4", s.address)
	if err != nil {
		return err
	}

	s.socket = conn
	s.reader = bufio.NewReader(conn)
	_, err = s.reader.ReadString('\n')
	return err
}

// Close closes the connection to GPSD
func (s *Session) Close() error {
	s.Watch(map[string]bool{"enable": false})
	close(s.done)
	return s.socket.Close()
}

// Run starts monitoring the connection to GPSD
func (s *Session) Run(format string) {
	go s.run(format)
}

func (s *Session) run(format string) {
	s.done = make(chan struct{})

	for {
		select {
		case <-s.done:
			return
		default:
		}
		s.Watch(map[string]bool{"enable": true, format: true})

		switch format {
		case formatJSON:
			s.watchJSON()
		case formatNMEA:
			s.watchNMEA()
		}

		time.Sleep(time.Second)
		_ = s.dial()
	}
}

// VersionSync sends the version command and returns the version response string
func (s *Session) VersionSync() string {
	s.Version()
	line, _ := s.readLine()
	return line
}

// Version sends the version command to GPSD
func (s *Session) Version() {
	s.SendCommand(VersionCommand)
}

// PollSync sends the poll command returns the poll response string
func (s *Session) PollSync() string {
	s.Poll()
	line, _ := s.readLine()
	return line
}

// Poll sends the poll command to GPSD
func (s *Session) Poll() {
	s.SendCommand(PollCommand)
}

// WatchSync sends the watch command with an optional param object parsed into
// the payload and returns the watch response string
func (s *Session) WatchSync(watchObject ...map[string]bool) string {
	s.Watch(watchObject...)
	line, _ := s.readLine()
	return line
}

// Watch sends the watch command with an optional param object parsed into the payload
func (s *Session) Watch(watchObject ...map[string]bool) {
	objectString := ""
	if len(watchObject) == 1 {
		var values []string
		for k, v := range watchObject[0] {
			values = append(values, fmt.Sprintf(`"%s":%v`, k, v))
		}
		objectString = fmt.Sprintf(`={%s}`, strings.Join(values, ","))
	}
	s.SendCommand(WatchCommand + objectString)
}

// SendCommand sends a command to GPSD
func (s *Session) SendCommand(command string) {
	_, _ = fmt.Fprintf(s.socket, "?"+command+";")
}

// SendCommandSync sends a command to GPSD and returns the response string
func (s *Session) SendCommandSync(command string) string {
	s.SendCommand(command)
	line, _ := s.readLine()
	return line
}

func (s *Session) Subscribe(class string, f Filter) {
	s.filters[class] = append(s.filters[class], f)
}

func (s *Session) SubscribeAll(f Filter) {
	for class := range s.filters {
		s.filters[class] = append(s.filters[class], f)
	}
}

func (s *Session) deliverReport(class string, report interface{}) {
	for _, f := range s.filters[class] {
		f(report)
	}
}

// readLine reads a line from the reader and returns the string
func (s *Session) readLine() (line string, err error) {
	line, err = s.reader.ReadString('\n')
	if err != nil {
		if err == io.EOF {
		} else if op, ok := err.(*net.OpError); ok && strings.Contains(
			op.Err.Error(), "use of closed network connection") {
		} else {
			fmt.Printf("Stream reader error (is gpsd running?): %#v\n", err)
		}
	}
	return
}

// getClass returns the class string for the passed line in case of error, a blank string is returned
func getClass(line []byte) string {
	var reportPeek gpsdReport
	if err := json.Unmarshal(line, &reportPeek); err != nil {
		fmt.Printf("failed to parse class type: %s\n", err)
		return ""
	}
	return reportPeek.Class
}

func (s *Session) watchNMEA() {
	for {
		select {
		case <-s.done:
			return
		default:
		}
		line, err := s.readLine()
		if err != nil {
			return
		}

		if _, ok := s.filters[msgClassDevices]; ok {
			if strings.HasPrefix(line, `{"class":"DEVICES"`) {
				report, err := unmarshalReport(msgClassDevices, []byte(line))
				if err != nil {
					fmt.Printf("failed to unmarshal report: %s\n", err)
					continue
				}
				s.deliverReport(msgClassDevices, report)
				continue
			}
		}

		// NMEA reports are prefixed with "$" that we don't need to include in the class.
		// Next 5 characters are the class. Here is an example of a GGA report:
		// $GPGGA,123519,4807.038,N,01131.000,E,1,08,0.9,545.4,M,46.9,M,,*47
		// So, line[1:6] will give us "GPGGA".ss
		s.deliverReport(line[1:6], line)
	}
}

func (s *Session) watchJSON() {
	// We're not using a JSON decoder because we first need to inspect
	// the JSON string to determine its "class"
	for {
		select {
		case <-s.done:
			return
		default:
		}
		line, err := s.readLine()
		if err != nil {
			return
		}

		lineBytes := []byte(line)
		class := getClass(lineBytes)

		if len(s.filters[class]) == 0 {
			continue
		}

		report, err := unmarshalReport(class, lineBytes)
		if err != nil {
			fmt.Printf("failed to unmarshal report: %s\n", err)
			continue
		}

		s.deliverReport(class, report)
	}
}

func unmarshalReport(class string, bytes []byte) (r interface{}, err error) {
	switch class {
	case msgClassTPV:
		r = new(TPVReport)
	case msgClassSKY:
		r = new(SKYReport)
	case msgClassGST:
		r = new(GSTReport)
	case msgClassATT:
		r = new(ATTReport)
	case msgClassVersion:
		r = new(VERSIONReport)
	case msgClassDevices:
		r = new(DEVICESReport)
	case msgClassPPS:
		r = new(PPSReport)
	case msgClassError:
		r = new(ERRORReport)
	}
	return r, json.Unmarshal(bytes, &r)
}
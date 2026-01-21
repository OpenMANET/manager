package gpsd

import (
	"fmt"
	"time"
)

type gpsdReport struct {
	Class string `json:"class"`
}

/*
TPVReport is a Time-Position-Velocity report

The "class" and "mode" fields will reliably be present.
The "mode" field will be emitted before optional fields that may be absent when there is no fix.
Error estimates will be emitted after the fix components they're associated with.
Others may be reported or not depending on the fix quality.

All error estimates (epc, epd, epe, eph, ept, epv, epx, epy) are guessed to be 95% confidence,
may also be 50%, one sigma, or two sigma confidence. Many GNSS receivers do not specify a confidence
level. None specify how the value is calculated. Use error estimates with caution, and only as
relative "goodness" indicators. If the GPS reports a value to gpsd, then gpsd will report that value.
Otherwise gpsd will try to compute the value from the skyview.

example:

	{"class":"TPV","device":"/dev/pts/1",
		"time":"2005-06-08T10:34:48.283Z","ept":0.005,
		"lat":46.498293369,"lon":7.567411672,"alt":1343.127,
		"eph":36.000,"epv":32.321,
		"track":10.3788,"speed":0.091,"climb":-0.085,"mode":3}
*/
type TPVReport struct {
	// Fixed: "TPV"
	Class string `json:"class"`
	// todo: add status
	// todo: find out where tag is from and document
	Tag string `json:"tag"`
	// Name of the originating device.
	Device string `json:"device"`
	// NMEA mode: %d, 0=no mode value yet seen, 1=no fix, 2=2D, 3=3D.
	Mode Mode `json:"mode"`
	// Time/date stamp in ISO8601 format, UTC. May have a fractional part of up to .001sec precision.
	// May be absent if the mode is not 2D or 3D.
	Time time.Time `json:"time"`
	// todo: update for altHAE and alt MSL
	// Estimated timestamp error in seconds. Certainty unknown.
	Ept float64 `json:"ept"`
	// Latitude in degrees: +/- signifies North/South.
	Lat float64 `json:"lat"`
	// Longitude in degrees: +/- signifies East/West.
	Lon float64 `json:"lon"`
	// Longitude error estimate in meters. Certainty unknown.
	// Deprecated. Undefined. Use altHAE or altMSL.
	Alt float64 `json:"alt"`
	// Longitude error estimate in meters. Certainty unknown.
	Epx float64 `json:"epx"`
	// Latitude error estimate in meters. Certainty unknown.
	Epy float64 `json:"epy"`
	// Estimated vertical error in meters. Certainty unknown.
	Epv float64 `json:"epv"`
	// Course over ground, degrees from true north.
	Track float64 `json:"track"`
	// Speed over ground, meters per second.
	Speed float64 `json:"speed"`
	// Climb (positive) or sink (negative) rate, meters per second.
	Climb float64 `json:"climb"`
	// Estimated track (direction) error in degrees. Certainty unknown.
	Epd float64 `json:"epd"`
	// Estimated speed error in meters per second. Certainty unknown.
	Eps float64 `json:"eps"`
	// Estimated climb error in meters per second. Certainty unknown.
	Epc float64 `json:"epc"`
}

/*
SKYReport reports sky view of GPS satellites

A SKY object reports a sky view of the GPS satellite positions. If there is no GPS device available, or
no skyview has been reported yet, only the "class" field will reliably be present.

Many devices compute dilution of precision factors but do not include them in their reports.
Many that do report DOPs report only HDOP, two-dimensional circular error. gpsd always passes
through whatever the device reports, then attempts to fill in other DOPs by calculating the
appropriate determinants in a covariance matrix based on the satellite view. DOPs may be missing
if some of these determinants are singular. It can even happen that the device reports an error
estimate in meters when the corresponding DOP is unavailable; some devices use more sophisticated
error modeling than the covariance calculation.

example:

	{"class":"SKY","device":"/dev/pts/1",
		"time":"2005-07-08T11:28:07.114Z",
		"xdop":1.55,"hdop":1.24,"pdop":1.99,
		"satellites":[
			{"PRN":23,"el":6,"az":84,"ss":0,"used":false},
			{"PRN":28,"el":7,"az":160,"ss":0,"used":false},
			{"PRN":8,"el":66,"az":189,"ss":44,"used":true},
			{"PRN":29,"el":13,"az":273,"ss":0,"used":false},
			{"PRN":10,"el":51,"az":304,"ss":29,"used":true},
			{"PRN":4,"el":15,"az":199,"ss":36,"used":true},
			{"PRN":2,"el":34,"az":241,"ss":43,"used":true},
			{"PRN":27,"el":71,"az":76,"ss":43,"used":true}]}
*/
type SKYReport struct {
	// Fixed: "SKY"
	Class string `json:"class"`
	// todo: find out where tag is from and document
	Tag string `json:"tag"`
	// Name of originating device
	Device string `json:"device"`
	// Time/date stamp in ISO8601 format, UTC. May have a fractional part of up to .001sec precision.
	Time time.Time `json:"time"`
	// Longitudinal dilution of precision, a dimensionless factor which should be multiplied by a base
	//  UERE to get an error estimate.
	Xdop float64 `json:"xdop"`
	// Latitudinal dilution of precision, a dimensionless factor which should be multiplied by a base
	//  UERE to get an error estimate.
	Ydop float64 `json:"ydop"`
	// Vertical (altitude) dilution of precision, a dimensionless factor which should be multiplied by a base
	//  UERE to get an error estimate.
	Vdop float64 `json:"vdop"`
	// Time dilution of precision, a dimensionless factor which should be multiplied by a base
	//  UERE to get an error estimate.
	Tdop float64 `json:"tdop"`
	// Horizontal dilution of precision, a dimensionless factor which should be multiplied by a base
	//  UERE to get a circular error estimate.
	Hdop float64 `json:"hdop"`
	// Position (spherical/3D) dilution of precision, a dimensionless factor which should be multiplied by a base
	//  UERE to get an error estimate.
	Pdop float64 `json:"pdop"`
	// Geometric (hyperspherical) dilution of precision, a combination of PDOP and TDOP.
	// A dimensionless factor which should be multiplied by a base UERE to get an error estimate.
	Gdop float64 `json:"gdop"`
	// List of satellite objects in skyview
	Satellites []Satellite `json:"satellites"`
}

// Satellite describes a location of a GPS satellite
type Satellite struct {
	// PRN ID of the satellite. 1-63 are GNSS satellites, 64-96 are GLONASS satellites, 100-164 are SBAS satellites
	PRN float64 `json:"PRN"`
	// Azimuth, degrees from true north.
	Az float64 `json:"az"`
	// Elevation in degrees.
	El float64 `json:"el"`
	// Signal to Noise ratio in dBHz.
	Ss float64 `json:"ss"`
	// Used in current solution? (SBAS/WAAS/EGNOS satellites may be flagged used if the solution
	//  has corrections from them, but not all drivers make this information available.)
	Used bool `json:"used"`
}

/*
GSTReport is pseudorange noise report

pseudorange is the pseudo distance between a satellite and a navigation satellite receiver

example:

	{"class":"GST","device":"/dev/ttyUSB0",
			"time":"2010-12-07T10:23:07.096Z","rms":2.440,
			"major":1.660,"minor":1.120,"orient":68.989,
			"lat":1.600,"lon":1.200,"alt":2.520}
*/
type GSTReport struct {
	// Fixed: "GST"
	Class string `json:"class"`
	// todo: find out where tag is from and document
	Tag string `json:"tag"`
	// Name of originating device
	Device string `json:"device"`
	// Time/date stamp in ISO8601 format, UTC. May have a fractional part of up to .001sec precision.
	Time time.Time `json:"time"`
	// Value of the standard deviation of the range inputs to the navigation process
	//  (range inputs include pseudoranges and DGPS corrections).
	Rms float64 `json:"rms"`
	// Standard deviation of semi-major axis of error ellipse, in meters.
	Major float64 `json:"major"`
	// Standard deviation of semi-minor axis of error ellipse, in meters.
	Minor float64 `json:"minor"`
	// Orientation of semi-major axis of error ellipse, in degrees from true north.
	Orient float64 `json:"orient"`
	// Standard deviation of latitude error, in meters.
	Lat float64 `json:"lat"`
	// Standard deviation of longitude error, in meters.
	Lon float64 `json:"lon"`
	// Standard deviation of altitude error, in meters.
	Alt float64 `json:"alt"`
}

/*
ATTReport reports vehicle-attitude from the digital compass or the gyroscope

An ATT object is a vehicle-attitude report.
It is returned by digital-compass and gyroscope sensors; depending on device,
it may include: heading, pitch, roll, yaw, gyroscope, and magnetic-field readings.
Because such sensors are often bundled as part of marine-navigation systems, the ATT
response may also include water depth.

The "class" and "mode" fields will reliably be present. Others may be reported or not depending
on the specific device type.

The heading, pitch, and roll status codes (if present) vary by device.
For the TNT Revolution digital compasses, they are coded as follows:

C	magnetometer calibration alarm
L	low alarm
M	low warning
N	normal
O	high warning
P	high alarm
V	magnetometer voltage level alarm

example:

	{"class":"ATT","time":1270938096.843,
		"heading":14223.00,"mag_st":"N",
		"pitch":169.00,"pitch_st":"N", "roll":-43.00,"roll_st":"N",
		"dip":13641.000,"mag_x":2454.000}
*/
type ATTReport struct {
	// Fixed: "ATT"
	Class string `json:"class"`
	// todo: find out where tag is from and document
	Tag string `json:"tag"`
	// Name of originating device
	Device string `json:"device"`
	// Time/date stamp in ISO8601 format, UTC. May have a fractional part of up to .001sec precision.
	Time time.Time `json:"time"`
	// Heading, degrees from true north.
	Heading float64 `json:"heading"`
	// Magnetometer status.
	MagSt string `json:"mag_st"`
	// Pitch in degrees.
	Pitch float64 `json:"pitch"`
	// Pitch sensor status.
	PitchSt string `json:"pitch_st"`
	// Yaw in degrees
	Yaw float64 `json:"yaw"`
	// Yaw sensor status.
	YawSt string `json:"yaw_st"`
	// Roll in degrees.
	Roll float64 `json:"roll"`
	// Roll sensor status.
	RollSt string `json:"roll_st"`
	// Local magnetic inclination, degrees, positive when the magnetic field points downward (into the Earth).
	Dip float64 `json:"dip"`
	// Scalar magnetic field strength.
	MagLen float64 `json:"mag_len"`
	// X component of magnetic field strength.
	MagX float64 `json:"mag_x"`
	// Y component of magnetic field strength.
	MagY float64 `json:"mag_y"`
	// Z component of magnetic field strength.
	MagZ float64 `json:"mag_z"`
	// Scalar acceleration.
	AccLen float64 `json:"acc_len"`
	// X component of acceleration.
	AccX float64 `json:"acc_x"`
	// Y component of acceleration.
	AccY float64 `json:"acc_y"`
	// Z component of acceleration.
	AccZ float64 `json:"acc_z"`
	// X component of acceleration.
	GyroX float64 `json:"gyro_x"`
	// Y component of acceleration.
	GyroY float64 `json:"gyro_y"`
	// Water depth in meters.
	Depth float64 `json:"depth"`
	// Temperature at the sensor, degrees centigrade.
	Temperature float64 `json:"temperature"`
}

/*
VERSIONReport returns version details of gpsd client

response to "?VERSION" command

The daemon ships a VERSION response to each client when the client first connects to it.

example:

		{"class":"VERSION","version":"2.40dev",
			"rev":"06f62e14eae9886cde907dae61c124c53eb1101f",
			"proto_major":3,"proto_minor":1
	}
*/
type VERSIONReport struct {
	Class      string `json:"class"`
	Release    string `json:"release"`
	Rev        string `json:"rev"`
	ProtoMajor int    `json:"proto_major"`
	ProtoMinor int    `json:"proto_minor"`
	Remote     string `json:"remote"`
}

/*
DEVICESReport lists all devices connected to the system

response to "?DEVICES" command

example:

	{"class"="DEVICES","devices":[
		{"class":"DEVICE","path":"/dev/pts/1","flags":1,"driver":"SiRF binary"},
		{"class":"DEVICE","path":"/dev/pts/3","flags":4,"driver":"AIVDM"}]}

The daemon occasionally ships a bare DEVICE object to the client (that is, one not inside a DEVICES wrapper).
The data content of these objects will be described later as a response to the ?DEVICE command.
*/
type DEVICESReport struct {
	Class   string         `json:"class"`
	Devices []DEVICEReport `json:"devices"`
	Remote  string         `json:"remote"`
}

// DEVICEReport reports a state of a particular device
type DEVICEReport struct {
	Class     string  `json:"class"`
	Path      string  `json:"path"`
	Activated string  `json:"activated"`
	Flags     int     `json:"flags"`
	Driver    string  `json:"driver"`
	Subtype   string  `json:"subtype"`
	Bps       int     `json:"bps"`
	Parity    string  `json:"parity"`
	Stopbits  int     `json:"stopbits"`
	Native    int     `json:"native"`
	Cycle     float64 `json:"cycle"`
	Mincycle  float64 `json:"mincycle"`
}

// String implements fmt.Stringer interface to provide a human-readable representation of the DEVICEReport.
func (d DEVICEReport) String() string {
	return fmt.Sprintf(
		"path=%s, activated=%s, flags=%d, driver=%s, subtype=%s,"+
			" bps=%d, parity=%s, stopbits=%d, native=%d, cycle=%f, mincycle=%f",
		d.Path, d.Activated, d.Flags, d.Driver, d.Subtype,
		d.Bps, d.Parity, d.Stopbits, d.Native, d.Cycle, d.Mincycle,
	)
}

// PPSReport is triggered on each pulse-per-second strobe from a device
type PPSReport struct {
	Class      string  `json:"class"`
	Device     string  `json:"device"`
	RealSec    float64 `json:"real_sec"`
	RealMusec  float64 `json:"real_musec"`
	ClockSec   float64 `json:"clock_sec"`
	ClockMusec float64 `json:"clock_musec"`
}

// ERRORReport is an error response
type ERRORReport struct {
	Class   string `json:"class"`
	Message string `json:"message"`
}

// Mode describes status of a TPV report
type Mode byte

const (
	// NoValueSeen indicates no data has been received yet
	NoValueSeen Mode = 0
	// NoFix indicates fix has not been acquired yet
	NoFix Mode = 1
	// Mode2D represents quality of the fix
	Mode2D Mode = 2
	// Mode3D represents quality of the fix
	Mode3D Mode = 3
)

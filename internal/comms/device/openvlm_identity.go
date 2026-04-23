package device

import (
	"errors"
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

// HIDInputReader reads the current HID input report from the given hidraw
// device path. It is a seam for unit tests; production code passes a nil
// reader to CheckOpenVLMIdentity and gets the default implementation, which
// issues a HIDIOCGINPUT ioctl against /dev/hidrawN.
//
// The returned slice must be laid out as hidraw presents it: a leading
// Report ID byte followed by the four CM108 input-report bytes
// (IR0, IR1, IR2, IR3).
type HIDInputReader func(hidPath string) ([]byte, error)

// hidIOCGInput encodes HIDIOCGINPUT(5) from <linux/hidraw.h>:
//
//	_IOC(_IOC_READ|_IOC_WRITE, 'H', 0x0A, 5)
//
// Layout (Linux asm-generic): dir<<30 | size<<16 | type<<8 | nr, where the
// direction bits are _IOC_WRITE=1 and _IOC_READ=2. The size (5) covers one
// Report ID byte plus the four CM108 input-report bytes.
const hidIOCGInput uintptr = (1|2)<<30 | 5<<16 | 'H'<<8 | 0x0A

// CheckOpenVLMIdentity returns true when the CM108 described by d is
// positively identified as an OpenVLM module. Identification is performed
// by issuing a HID Get_Input_Report control transfer and inspecting GPIO1
// (bit 0 of HID_IR1). OpenVLM hardware wires GPIO1 high via a board strap;
// a generic CM108 USB audio dongle leaves it low.
//
// The datasheet (CM108B §7.4) guarantees IR1[3:0] reflects the live input
// state of GPIO4..GPIO1 only when IR0[7:6] == 2'b00. That is the default
// register state and this codebase never writes HID_OR0, so the helper
// requires IR0[7:6] == 0 and returns an error otherwise.
//
// read may be nil, in which case the default /dev/hidrawN ioctl reader is
// used. The device must have a non-empty HIDPath (populated by
// DiscoverCM108).
func CheckOpenVLMIdentity(d CM108Descriptor, read HIDInputReader) (bool, error) {
	if d.HIDPath == "" {
		return false, errors.New("device: descriptor has no HID path")
	}

	if read == nil {
		read = defaultHIDInputReader
	}

	report, err := read(d.HIDPath)
	if err != nil {
		return false, fmt.Errorf("device: probe OpenVLM identity: %w", err)
	}

	// Need at least [ReportID, IR0, IR1].
	if len(report) < 3 {
		return false, fmt.Errorf("device: HID input report too short: %d bytes", len(report))
	}

	// IR0[7:6] == 2'b00 is required for IR1[3:0] to reflect GPIO state.
	if report[1]&0xC0 != 0 {
		return false, fmt.Errorf("device: HID_IR0[7:6]=0x%x, IR1 is not in GPIO-input mode", (report[1]>>6)&0x3)
	}

	// GPIO1 is bit 0 of IR1. High = OpenVLM hardware strap present.
	return report[2]&0x01 != 0, nil
}

// defaultHIDInputReader opens path and issues a HIDIOCGINPUT ioctl to
// synchronously fetch the current HID input report.
//
// The ioctl performs a USB control transfer (Get_Report, type=Input,
// id=0), which the CM108B datasheet §7.4 documents as the supported way
// to poll GPIO state without waiting for a state-change interrupt.
func defaultHIDInputReader(path string) ([]byte, error) {
	// RDWR is required: HIDIOCGINPUT encodes both read and write direction
	// bits (it is a bidirectional control transfer from the kernel's
	// perspective) and the hidraw driver enforces this at open time.
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}

	defer func() { _ = f.Close() }()

	// Buffer layout for HIDIOCGINPUT: first byte is the Report ID to
	// request (0 for CM108, which has no numbered reports), remaining
	// bytes receive the report payload. CM108 returns IR0..IR3 (4 bytes),
	// so the total is 5.
	buf := make([]byte, 5)
	buf[0] = 0

	_, _, errno := unix.Syscall(unix.SYS_IOCTL, f.Fd(), hidIOCGInput, uintptr(unsafe.Pointer(&buf[0])))
	if errno != 0 {
		return nil, fmt.Errorf("HIDIOCGINPUT %s: %w", path, errno)
	}

	return buf, nil
}

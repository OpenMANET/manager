package device

import (
	evdev "github.com/gvalkov/golang-evdev"
)

// FindEvdev searches for a Linux input device whose name matches deviceName
// within the glob pattern devicePath. Returns nil if not found or on error.
func FindEvdev(devicePath, deviceName string) *evdev.InputDevice {
	devs, err := evdev.ListInputDevices(devicePath)
	if err != nil {
		return nil
	}

	for _, d := range devs {
		if d.Name == deviceName {
			return d
		}
	}

	return nil
}

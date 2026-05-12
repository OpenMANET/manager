// Package iwinfo retrieves wireless interface information from OpenWrt's
// rpcd-mod-iwinfo ubus plugin (ubus call iwinfo info/devices).
// All public functions have a ...WithExecutor variant that accepts a custom
// UbusExecutor, enabling fully offline unit tests without any runtime deps.
package iwinfo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
)

// UbusExecutor executes ubus commands.
type UbusExecutor interface {
	Execute(ctx context.Context, args ...string) ([]byte, error)
}

// DefaultUbusExecutor executes real ubus commands via os/exec.
type DefaultUbusExecutor struct{}

// Execute runs the ubus binary with the given arguments.
func (e *DefaultUbusExecutor) Execute(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "ubus", args...)

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ubus %v: %w", args, err)
	}

	return out, nil
}

// EncryptionInfo represents the encryption configuration of a wireless interface
// as returned by the iwinfo ubus plugin.
type EncryptionInfo struct {
	WPA            []int    `json:"wpa"`
	Authentication []string `json:"authentication"`
	Ciphers        []string `json:"ciphers"`
	Enabled        bool     `json:"enabled"`
}

// IsEnabled reports whether encryption is active on the interface.
func (e EncryptionInfo) IsEnabled() bool { return e.Enabled }

// GetWPA returns the active WPA versions (e.g. [3] for WPA3).
func (e EncryptionInfo) GetWPA() []int { return e.WPA }

// GetAuthentication returns the authentication methods (e.g. ["sae"]).
func (e EncryptionInfo) GetAuthentication() []string { return e.Authentication }

// GetCiphers returns the cipher suites in use (e.g. ["ccmp"]).
func (e EncryptionInfo) GetCiphers() []string { return e.Ciphers }

// HardwareInfo holds the hardware identification of a wireless PHY.
type HardwareInfo struct {
	Name string `json:"name"`
	ID   []int  `json:"id"`
}

// GetID returns the raw hardware ID integers.
func (h HardwareInfo) GetID() []int { return h.ID }

// GetName returns the human-readable hardware name, e.g. "MediaTek MT7916AN".
func (h HardwareInfo) GetName() string { return h.Name }

// InterfaceInfo is the full iwinfo data for one wireless interface,
// as returned by `ubus call iwinfo info '{"device":"<iface>"}'`.
// Numeric fields that are unavailable on the device are reported as zero.
type InterfaceInfo struct {
	SSID            string         `json:"ssid"`
	BSSID           string         `json:"bssid"`
	Country         string         `json:"country"`
	Mode            string         `json:"mode"`
	PHY             string         `json:"phy"`
	HTMode          string         `json:"htmode"`
	HWMode          string         `json:"hwmode"`
	HWModesText     string         `json:"hwmodes_text"`
	Hardware        HardwareInfo   `json:"hardware"`
	HWModes         []string       `json:"hwmodes"`
	HTModes         []string       `json:"htmodes"`
	Encryption      EncryptionInfo `json:"encryption"`
	CenterChan1     int            `json:"center_chan1"`
	QualityMax      int            `json:"quality_max"`
	Signal          int            `json:"signal"`
	Noise           int            `json:"noise"`
	Bitrate         int            `json:"bitrate"`
	Quality         int            `json:"quality"`
	TxPowerOffset   int            `json:"txpower_offset"`
	TxPower         int            `json:"txpower"`
	FrequencyOffset int            `json:"frequency_offset"`
	Frequency       int            `json:"frequency"`
	CenterChan2     int            `json:"center_chan2"`
	Channel         int            `json:"channel"`
}

// GetPHY returns the PHY name (e.g. "phy1").
func (i *InterfaceInfo) GetPHY() string { return i.PHY }

// GetSSID returns the ESSID the interface is associated with.
func (i *InterfaceInfo) GetSSID() string { return i.SSID }

// GetBSSID returns the BSSID of the associated access point or mesh node.
func (i *InterfaceInfo) GetBSSID() string { return i.BSSID }

// GetCountry returns the regulatory country code.
func (i *InterfaceInfo) GetCountry() string { return i.Country }

// GetMode returns the operating mode (e.g. "Mesh Point", "Client", "Master").
func (i *InterfaceInfo) GetMode() string { return i.Mode }

// GetChannel returns the primary channel number.
func (i *InterfaceInfo) GetChannel() int { return i.Channel }

// GetCenterChan1 returns the first center channel for HT/VHT/HE operation.
func (i *InterfaceInfo) GetCenterChan1() int { return i.CenterChan1 }

// GetCenterChan2 returns the second center channel (80+80 MHz). Zero if unused.
func (i *InterfaceInfo) GetCenterChan2() int { return i.CenterChan2 }

// GetFrequency returns the channel center frequency in MHz.
func (i *InterfaceInfo) GetFrequency() int { return i.Frequency }

// GetFrequencyOffset returns the frequency offset in MHz.
func (i *InterfaceInfo) GetFrequencyOffset() int { return i.FrequencyOffset }

// GetTxPower returns the transmit power in dBm. Zero indicates unknown/unavailable.
func (i *InterfaceInfo) GetTxPower() int { return i.TxPower }

// GetTxPowerOffset returns the transmit power offset in dBm.
func (i *InterfaceInfo) GetTxPowerOffset() int { return i.TxPowerOffset }

// GetQuality returns the link quality value.
func (i *InterfaceInfo) GetQuality() int { return i.Quality }

// GetQualityMax returns the maximum possible link quality value.
func (i *InterfaceInfo) GetQualityMax() int { return i.QualityMax }

// GetSignal returns the received signal strength in dBm. Zero indicates unknown.
func (i *InterfaceInfo) GetSignal() int { return i.Signal }

// GetNoise returns the noise floor in dBm. Zero indicates unknown.
func (i *InterfaceInfo) GetNoise() int { return i.Noise }

// GetBitrate returns the current bit rate in kbit/s. Zero indicates unknown.
func (i *InterfaceInfo) GetBitrate() int { return i.Bitrate }

// GetHTMode returns the channel width mode string (e.g. "HT20", "VHT80").
func (i *InterfaceInfo) GetHTMode() string { return i.HTMode }

// GetHWMode returns the active hardware mode letter (e.g. "n", "ac", "ax").
func (i *InterfaceInfo) GetHWMode() string { return i.HWMode }

// GetHWModesText returns the combined hardware modes string (e.g. "ax/b/g/n").
func (i *InterfaceInfo) GetHWModesText() string { return i.HWModesText }

// GetHWModes returns the supported hardware mode letters (e.g. ["b","g","n","ax"]).
func (i *InterfaceInfo) GetHWModes() []string { return i.HWModes }

// GetHTModes returns the supported HT/VHT/HE channel-width modes.
func (i *InterfaceInfo) GetHTModes() []string { return i.HTModes }

// GetHardwareName returns the human-readable chip name, e.g. "MediaTek MT7916AN".
func (i *InterfaceInfo) GetHardwareName() string { return i.Hardware.Name }

// GetHardwareID returns the raw hardware ID integers.
func (i *InterfaceInfo) GetHardwareID() []int { return i.Hardware.ID }

// DevicesResponse is the response from `ubus call iwinfo devices`.
type DevicesResponse struct {
	Devices []string `json:"devices"`
}

// IwinfoProvider is the interface satisfied by *Client.
// Consumers in other packages (e.g. mgmt, handlers) should depend on this
// interface rather than *Client directly to enable test mocking.
type IwinfoProvider interface {
	GetDevices(ctx context.Context) ([]string, error)
	GetInfo(ctx context.Context, device string) (*InterfaceInfo, error)
	GetInfoForAll(ctx context.Context) (map[string]*InterfaceInfo, error)
}

// Client provides access to iwinfo data via ubus and satisfies IwinfoProvider.
type Client struct {
	exec UbusExecutor
}

// NewClient returns a Client backed by the real ubus binary.
func NewClient() *Client {
	return &Client{exec: &DefaultUbusExecutor{}}
}

// NewClientWithExecutor returns a Client using a custom executor (useful for testing).
func NewClientWithExecutor(exec UbusExecutor) *Client {
	return &Client{exec: exec}
}

// GetDevices returns all wireless interface names known to iwinfo.
func (c *Client) GetDevices(ctx context.Context) ([]string, error) {
	return GetDevicesWithExecutor(ctx, c.exec)
}

// GetInfo returns the full iwinfo data for the named wireless interface.
func (c *Client) GetInfo(ctx context.Context, device string) (*InterfaceInfo, error) {
	return GetInfoWithExecutor(ctx, c.exec, device)
}

// GetInfoForAll returns iwinfo data for every interface returned by GetDevices.
// Partial results are returned alongside any per-device errors.
func (c *Client) GetInfoForAll(ctx context.Context) (map[string]*InterfaceInfo, error) {
	return GetInfoForAllWithExecutor(ctx, c.exec)
}

// GetDevices returns all wireless interface names known to iwinfo.
func GetDevices(ctx context.Context) ([]string, error) {
	return GetDevicesWithExecutor(ctx, &DefaultUbusExecutor{})
}

// GetDevicesWithExecutor returns wireless interface names using a custom executor.
func GetDevicesWithExecutor(ctx context.Context, exec UbusExecutor) ([]string, error) {
	out, err := exec.Execute(ctx, "call", "iwinfo", "devices")
	if err != nil {
		return nil, fmt.Errorf("iwinfo devices: %w", err)
	}

	var resp DevicesResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("parse iwinfo devices: %w", err)
	}

	return resp.Devices, nil
}

// GetInfo returns the full iwinfo data for the named wireless interface.
func GetInfo(ctx context.Context, device string) (*InterfaceInfo, error) {
	return GetInfoWithExecutor(ctx, &DefaultUbusExecutor{}, device)
}

// GetInfoWithExecutor returns iwinfo data for a device using a custom executor.
func GetInfoWithExecutor(ctx context.Context, exec UbusExecutor, device string) (*InterfaceInfo, error) {
	arg := fmt.Sprintf(`{"device":"%s"}`, device)

	out, err := exec.Execute(ctx, "call", "iwinfo", "info", arg)
	if err != nil {
		return nil, fmt.Errorf("iwinfo info %s: %w", device, err)
	}

	var info InterfaceInfo
	if err := json.Unmarshal(out, &info); err != nil {
		return nil, fmt.Errorf("parse iwinfo info %s: %w", device, err)
	}

	return &info, nil
}

// GetInfoForAll returns iwinfo data for every interface returned by GetDevices.
// If one or more devices fail, the partial map is returned alongside a joined error.
func GetInfoForAll(ctx context.Context) (map[string]*InterfaceInfo, error) {
	return GetInfoForAllWithExecutor(ctx, &DefaultUbusExecutor{})
}

// GetInfoForAllWithExecutor returns iwinfo data for all devices using a custom executor.
// Successfully fetched devices are included in the map even when other devices fail.
func GetInfoForAllWithExecutor(ctx context.Context, exec UbusExecutor) (map[string]*InterfaceInfo, error) {
	devices, err := GetDevicesWithExecutor(ctx, exec)
	if err != nil {
		return nil, err
	}

	result := make(map[string]*InterfaceInfo, len(devices))

	var errs []error

	for _, device := range devices {
		info, err := GetInfoWithExecutor(ctx, exec, device)
		if err != nil {
			errs = append(errs, err)

			continue
		}

		result[device] = info
	}

	if len(errs) > 0 {
		return result, errors.Join(errs...)
	}

	return result, nil
}

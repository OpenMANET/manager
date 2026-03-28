package handlers

import (
	"context"
	"errors"
	"fmt"
	"time"

	"connectrpc.com/connect"
	v1 "github.com/openmanet/openmanetd/internal/api/openmanet/dashboard/v1"
	batmanadv "github.com/openmanet/openmanetd/internal/batman-adv"
	"github.com/openmanet/openmanetd/internal/mgmt"
	"github.com/openmanet/openmanetd/internal/network"
	"github.com/openmanet/openmanetd/internal/system"
	"github.com/openmanet/openmanetd/internal/util/board"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// BoardProvider abstracts board configuration retrieval.
type BoardProvider interface {
	GetBoard() (*board.Board, error)
}

// DefaultBoardProvider reads /etc/board.json.
type DefaultBoardProvider struct{}

func (p *DefaultBoardProvider) GetBoard() (*board.Board, error) {
	return board.NewBoardConfigInfo()
}

// TailscaleStatusProvider abstracts Tailscale connectivity for the dashboard.
type TailscaleStatusProvider interface {
	IsRunning() bool
}

// DashboardService implements the dashboardv1connect.DashboardServiceHandler.
type DashboardService struct {
	Log         zerolog.Logger
	Board       BoardProvider
	SysInfo     system.SysInfoProvider
	Firmware    system.FirmwareProvider
	Interfaces  InterfaceProvider
	Wifi        WifiStationProvider
	Originators batmanadv.OriginatorProvider
	Tailscale   TailscaleStatusProvider
	Services    system.ServiceChecker
	Actions     system.QuickActionExecutor

	// MonitoredServices overrides the default service list.
	MonitoredServices []string
}

// WifiStationProvider abstracts WiFi mesh station info for the dashboard.
type WifiStationProvider interface {
	GetMeshNeighborCount() (int, error)
}

// DefaultWifiStationProvider uses the wireless subsystem to count mesh neighbors.
type DefaultWifiStationProvider struct {
	Wifi mgmt.WirelessProvider
}

// GetMeshNeighborCount returns the total number of connected mesh stations.
func (p *DefaultWifiStationProvider) GetMeshNeighborCount() (int, error) {
	meshIfaces, err := p.Wifi.GetMeshInterfaces()
	if err != nil {
		return 0, fmt.Errorf("get mesh interfaces: %w", err)
	}

	var total int

	for _, iface := range meshIfaces {
		stations, err := p.Wifi.StationInfo(iface)
		if err != nil {
			return 0, fmt.Errorf("station info for %s: %w", iface.Name, err)
		}

		total += len(stations)
	}

	return total, nil
}

func (d *DashboardService) monitoredServices() []string {
	if len(d.MonitoredServices) > 0 {
		return d.MonitoredServices
	}

	return system.DefaultMonitoredServices()
}

// GetDashboardStatus aggregates all dashboard data into a single response.
func (d *DashboardService) GetDashboardStatus(ctx context.Context, _ *emptypb.Empty) (*v1.GetDashboardStatusResponse, error) {
	d.Log.Debug().Msg("GetDashboardStatus request received")

	resp := &v1.GetDashboardStatusResponse{}

	// Device info
	deviceInfo, err := d.buildDeviceInfo()
	if err != nil {
		d.Log.Warn().Err(err).Msg("Partial failure collecting device info")
	}

	resp.DeviceInfo = deviceInfo

	// System resources
	sysResources, err := d.buildSystemResources()
	if err != nil {
		d.Log.Warn().Err(err).Msg("Partial failure collecting system resources")
	}

	resp.SystemResources = sysResources

	// Network summary
	netSummary, err := d.buildNetworkSummary()
	if err != nil {
		d.Log.Warn().Err(err).Msg("Partial failure collecting network summary")
	}

	resp.NetworkSummary = netSummary

	// Active services
	services, err := d.Services.CheckServices(ctx, d.monitoredServices())
	if err != nil {
		d.Log.Error().Err(err).Msg("Failed to check services")

		return nil, connect.NewError(connect.CodeInternal, err)
	}

	resp.ActiveServices = make([]*v1.ServiceInfo, 0, len(services))
	for _, svc := range services {
		resp.ActiveServices = append(resp.ActiveServices, &v1.ServiceInfo{
			Name:   svc.Name,
			Status: serviceStateToProto(svc.State),
			Pid:    int32(svc.PID),
		})
	}

	return resp, nil
}

// ExecuteQuickAction dispatches an administrative action.
func (d *DashboardService) ExecuteQuickAction(ctx context.Context, req *v1.ExecuteQuickActionRequest) (*v1.ExecuteQuickActionResponse, error) {
	d.Log.Info().Str("action", req.Action.String()).Msg("ExecuteQuickAction request received")

	var (
		err error
		msg string
	)

	switch req.Action {
	case v1.QuickAction_QUICK_ACTION_REBOOT_DEVICE:
		err = d.Actions.Reboot(ctx)
		msg = "Reboot initiated"
	case v1.QuickAction_QUICK_ACTION_RESTART_NETWORK:
		err = d.Actions.RestartNetwork(ctx)
		msg = "Network restart initiated"
	case v1.QuickAction_QUICK_ACTION_RESTART_OPENMANETD:
		err = d.Actions.RestartOpenmanetd(ctx)
		msg = "openmanetd restart initiated"
	default:
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("unknown action: %s", req.Action))
	}

	if err != nil {
		d.Log.Error().Err(err).Str("action", req.Action.String()).Msg("Quick action failed")

		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("action %s failed: %w", req.Action, err))
	}

	return &v1.ExecuteQuickActionResponse{
		Success: true,
		Message: &msg,
	}, nil
}

func (d *DashboardService) buildDeviceInfo() (*v1.DeviceInfo, error) {
	info := &v1.DeviceInfo{}

	var errs []error

	if hostname, err := d.SysInfo.GetHostname(); err != nil {
		errs = append(errs, err)
	} else {
		info.Hostname = hostname
	}

	if b, err := d.Board.GetBoard(); err != nil {
		errs = append(errs, err)
	} else {
		info.Model = b.Model.Name
	}

	if fw, err := d.Firmware.GetFirmwareInfo(); err != nil {
		errs = append(errs, err)
	} else {
		info.Firmware = fw.Description
	}

	if kernel, err := d.SysInfo.GetKernelVersion(); err != nil {
		errs = append(errs, err)
	} else {
		info.Kernel = kernel
	}

	if arch, err := d.SysInfo.GetArchitecture(); err != nil {
		errs = append(errs, err)
	} else {
		info.Architecture = arch
	}

	return info, errors.Join(errs...)
}

func (d *DashboardService) buildSystemResources() (*v1.SystemResources, error) {
	res := &v1.SystemResources{}

	var errs []error

	if uptime, err := d.SysInfo.GetUptime(); err != nil {
		errs = append(errs, err)
	} else {
		res.Uptime = durationpb.New(uptime)
	}

	res.LocalTime = timestamppb.New(time.Now())

	if cpuLoad, err := d.SysInfo.GetCPULoadPercent(); err != nil {
		errs = append(errs, err)
	} else {
		res.CpuLoadPercent = cpuLoad
	}

	if mem, err := d.SysInfo.GetMemoryInfo(); err != nil {
		errs = append(errs, err)
	} else {
		res.MemoryTotalBytes = mem.TotalBytes
		res.MemoryUsedBytes = mem.UsedBytes()
	}

	if overlay, err := d.SysInfo.GetOverlayUsage(); err != nil {
		errs = append(errs, err)
	} else {
		res.OverlayTotalBytes = overlay.TotalBytes
		res.OverlayUsedBytes = overlay.UsedBytes
	}

	return res, errors.Join(errs...)
}

func (d *DashboardService) buildNetworkSummary() (*v1.NetworkSummary, error) {
	summary := &v1.NetworkSummary{}

	var errs []error

	// Get all network interfaces
	ifaces, err := d.Interfaces.ListAll()
	if err != nil {
		return summary, fmt.Errorf("list interfaces: %w", err)
	}

	// WAN (eth0)
	if entry := d.buildWANEntry(ifaces); entry != nil {
		summary.Entries = append(summary.Entries, entry)
	}

	// LAN (br-ahwlan or br-lan)
	if entry := d.buildLANEntry(ifaces); entry != nil {
		summary.Entries = append(summary.Entries, entry)
	}

	// HaLow Mesh
	if entry, meshErr := d.buildMeshEntry(ifaces); meshErr != nil {
		errs = append(errs, meshErr)
	} else if entry != nil {
		summary.Entries = append(summary.Entries, entry)
	}

	// BATMAN (bat0)
	if entry, batErr := d.buildBatmanEntry(ifaces); batErr != nil {
		errs = append(errs, batErr)
	} else if entry != nil {
		summary.Entries = append(summary.Entries, entry)
	}

	// Tailscale
	if entry := d.buildTailscaleEntry(ifaces); entry != nil {
		summary.Entries = append(summary.Entries, entry)
	}

	return summary, errors.Join(errs...)
}

func (d *DashboardService) buildWANEntry(ifaces []network.NetworkInterfaceInfo) *v1.NetworkSummaryEntry {
	for _, iface := range ifaces {
		if iface.Name == "eth0" {
			entry := &v1.NetworkSummaryEntry{
				InterfaceName: iface.Name,
				DisplayName:   "WAN (eth0)",
			}
			if iface.State == network.OperStateUp && iface.IP != "" {
				entry.State = v1.NetworkInterfaceState_NETWORK_INTERFACE_STATE_CONNECTED
				entry.Detail = iface.IP
			} else {
				entry.State = v1.NetworkInterfaceState_NETWORK_INTERFACE_STATE_DISCONNECTED
				entry.Detail = "Disconnected"
			}

			return entry
		}
	}

	return nil
}

func (d *DashboardService) buildLANEntry(ifaces []network.NetworkInterfaceInfo) *v1.NetworkSummaryEntry {
	for _, iface := range ifaces {
		if iface.Name == "br-ahwlan" || iface.Name == "br-lan" {
			entry := &v1.NetworkSummaryEntry{
				InterfaceName: iface.Name,
				DisplayName:   fmt.Sprintf("LAN (%s)", iface.Name),
			}
			if iface.State == network.OperStateUp && iface.IP != "" {
				entry.State = v1.NetworkInterfaceState_NETWORK_INTERFACE_STATE_CONNECTED
				entry.Detail = iface.IP
			} else {
				entry.State = v1.NetworkInterfaceState_NETWORK_INTERFACE_STATE_DISCONNECTED
				entry.Detail = "Disconnected"
			}

			return entry
		}
	}

	return nil
}

func (d *DashboardService) buildMeshEntry(ifaces []network.NetworkInterfaceInfo) (*v1.NetworkSummaryEntry, error) {
	// Find any HaLow mesh interface
	for _, iface := range ifaces {
		if iface.LinkType == network.LinkTypeHaLowMesh {
			entry := &v1.NetworkSummaryEntry{
				InterfaceName: iface.Name,
				DisplayName:   fmt.Sprintf("HaLow Mesh (%s)", iface.Name),
			}

			count, err := d.Wifi.GetMeshNeighborCount()
			if err != nil {
				entry.State = v1.NetworkInterfaceState_NETWORK_INTERFACE_STATE_DISCONNECTED
				entry.Detail = "Unknown"

				return entry, fmt.Errorf("mesh neighbor count: %w", err)
			}

			if count > 0 {
				entry.State = v1.NetworkInterfaceState_NETWORK_INTERFACE_STATE_CONNECTED
				entry.Detail = fmt.Sprintf("Connected — %d neighbors", count)
			} else {
				entry.State = v1.NetworkInterfaceState_NETWORK_INTERFACE_STATE_DISCONNECTED
				entry.Detail = "No neighbors"
			}

			return entry, nil
		}
	}

	return nil, nil
}

func (d *DashboardService) buildBatmanEntry(ifaces []network.NetworkInterfaceInfo) (*v1.NetworkSummaryEntry, error) {
	for _, iface := range ifaces {
		if iface.LinkType == network.LinkTypeBatman {
			entry := &v1.NetworkSummaryEntry{
				InterfaceName: iface.Name,
				DisplayName:   fmt.Sprintf("BATMAN (%s)", iface.Name),
			}

			originators, err := d.Originators.GetOriginators()
			if err != nil {
				entry.State = v1.NetworkInterfaceState_NETWORK_INTERFACE_STATE_CONNECTED
				entry.Detail = "Unknown"

				return entry, fmt.Errorf("originators: %w", err)
			}

			count := batmanadv.GetOriginatorCount(originators)
			if count > 0 {
				entry.State = v1.NetworkInterfaceState_NETWORK_INTERFACE_STATE_CONNECTED
				entry.Detail = fmt.Sprintf("%d originators", count)
			} else {
				entry.State = v1.NetworkInterfaceState_NETWORK_INTERFACE_STATE_DISCONNECTED
				entry.Detail = "No originators"
			}

			return entry, nil
		}
	}

	return nil, nil
}

func (d *DashboardService) buildTailscaleEntry(ifaces []network.NetworkInterfaceInfo) *v1.NetworkSummaryEntry {
	// Find tailscale interface
	for _, iface := range ifaces {
		if iface.Name == "tailscale0" {
			entry := &v1.NetworkSummaryEntry{
				InterfaceName: iface.Name,
				DisplayName:   "Tailscale (tailscale0)",
			}

			if d.Tailscale != nil && d.Tailscale.IsRunning() {
				entry.State = v1.NetworkInterfaceState_NETWORK_INTERFACE_STATE_CONNECTED
				entry.Detail = iface.IP
			} else {
				entry.State = v1.NetworkInterfaceState_NETWORK_INTERFACE_STATE_NOT_CONNECTED
				entry.Detail = "Not connected"
			}

			return entry
		}
	}

	// Even if no tailscale0 interface exists, we may still want to show the entry
	if d.Tailscale != nil {
		entry := &v1.NetworkSummaryEntry{
			InterfaceName: "tailscale0",
			DisplayName:   "Tailscale (tailscale0)",
			State:         v1.NetworkInterfaceState_NETWORK_INTERFACE_STATE_NOT_CONNECTED,
			Detail:        "Not connected",
		}

		return entry
	}

	return nil
}

func serviceStateToProto(s system.ServiceState) v1.ServiceStatus {
	switch s {
	case system.ServiceStateRunning:
		return v1.ServiceStatus_SERVICE_STATUS_RUNNING
	case system.ServiceStateStopped:
		return v1.ServiceStatus_SERVICE_STATUS_STOPPED
	default:
		return v1.ServiceStatus_SERVICE_STATUS_UNSPECIFIED
	}
}

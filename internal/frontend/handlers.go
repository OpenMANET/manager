package frontend

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// statusKey is the JSON field name used in simple status responses
// (e.g. {"status": "rebooting"}) emitted by the legacy HTTP control endpoints.
const statusKey = "status"

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

type systemInfo struct {
	Hostname    string  `json:"hostname"`
	LoadAvg     string  `json:"load_avg"`
	Board       string  `json:"board"`
	Model       string  `json:"model"`
	Version     string  `json:"version"`
	Uptime      int64   `json:"uptime"`
	MemTotal    int64   `json:"mem_total"`
	MemFree     int64   `json:"mem_free"`
	MemBuffered int64   `json:"mem_buffered"`
	CPUUsage    float64 `json:"cpu_usage"`
}

type processInfo struct {
	User string `json:"user"`
	Stat string `json:"stat"`
	Cmd  string `json:"command"`
	PID  int    `json:"pid"`
	VSZ  int64  `json:"vsz"`
}

type storageEntry struct {
	Filesystem string `json:"filesystem"`
	Size       string `json:"size"`
	Used       string `json:"used"`
	Available  string `json:"available"`
	UsePercent string `json:"use_percent"`
	MountedOn  string `json:"mounted_on"`
}

type logEntry struct {
	Line string `json:"line"`
}

type restartServiceRequest struct {
	Service string `json:"service"`
}

type networkInterface struct {
	Name      string `json:"name"`
	Operstate string `json:"operstate"`
	RxBytes   int64  `json:"rx_bytes"`
	RxPackets int64  `json:"rx_packets"`
	RxErrors  int64  `json:"rx_errors"`
	TxBytes   int64  `json:"tx_bytes"`
	TxPackets int64  `json:"tx_packets"`
	TxErrors  int64  `json:"tx_errors"`
	MTU       int    `json:"mtu"`
}

type wifiStatus struct {
	Interface string `json:"interface"`
	SSID      string `json:"ssid"`
	Mode      string `json:"mode"`
	BitRate   string `json:"bitrate"`
	Channel   int    `json:"channel"`
	Signal    int    `json:"signal"`
	Clients   int    `json:"clients"`
}

type routeEntry struct {
	Destination string `json:"destination"`
	Gateway     string `json:"gateway"`
	Device      string `json:"device"`
	Protocol    string `json:"protocol"`
	Scope       string `json:"scope"`
	Metric      int    `json:"metric"`
}

type batmanOriginator struct {
	Originator string  `json:"originator"`
	LastSeen   string  `json:"last_seen"`
	NextHop    string  `json:"next_hop"`
	OutgoingIf string  `json:"outgoing_if"`
	TQuality   float64 `json:"tq"`
}

type batmanInfo struct {
	Originators []batmanOriginator `json:"originators"`
	Gateways    []string           `json:"gateways"`
}

type hostnameResponse struct {
	Hostname string `json:"hostname"`
}

type hostnameRequest struct {
	Hostname string `json:"hostname"`
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func runCmd(ctx context.Context, name string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, name, args...).Output() //nolint:gosec // callers supply validated command names
	if err != nil {
		return "", fmt.Errorf("exec %s: %w", name, err)
	}

	return strings.TrimSpace(string(out)), nil
}

func readFileString(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(data))
}

func parseMeminfoField(content, field string) int64 {
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, field) {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				v, _ := strconv.ParseInt(parts[1], 10, 64)

				return v * 1024 // kB -> bytes
			}
		}
	}

	return 0
}

// writeErrorStatus writes an error JSON response with the given status code.
func (s *Server) writeErrorStatus(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)

	_ = json.NewEncoder(w).Encode(errorResponse{Error: msg})
}

// safeServiceName validates a service name to prevent command injection.
var safeServiceRe = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`) //nolint:gochecknoglobals

// ---------------------------------------------------------------------------
// System Handlers
// ---------------------------------------------------------------------------

func (s *Server) handleSystemInfo(w http.ResponseWriter, _ *http.Request) {
	info := systemInfo{}

	// Hostname
	info.Hostname = readFileString("/proc/sys/kernel/hostname")

	// Uptime
	if raw := readFileString("/proc/uptime"); raw != "" {
		parts := strings.Fields(raw)
		if len(parts) >= 1 {
			if secs, err := strconv.ParseFloat(parts[0], 64); err == nil {
				info.Uptime = int64(secs)
			}
		}
	}

	// Load average
	info.LoadAvg = readFileString("/proc/loadavg")

	// Memory
	if memRaw, err := os.ReadFile("/proc/meminfo"); err == nil {
		content := string(memRaw)
		info.MemTotal = parseMeminfoField(content, "MemTotal:")
		info.MemFree = parseMeminfoField(content, "MemFree:")
		info.MemBuffered = parseMeminfoField(content, "Buffers:")
	}

	// CPU usage — served from the background sampler so the request path
	// no longer pays a /proc/stat read + sleep on every call.
	info.CPUUsage = s.cpu.Get()

	// Board info – try /etc/board.json first, fall back to sysinfo.
	info.Board, info.Model = parseBoardInfo()

	// Version from /etc/openwrt_release
	info.Version = readDistribRelease()

	s.writeJSON(w, info)
}

func (s *Server) handleSystemProcesses(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	out, err := runCmd(ctx, "ps", "-eo", "pid,user,vsz,stat,comm", "--sort=-vsz")
	if err != nil {
		// Fallback: simpler ps for busybox.
		out, err = runCmd(ctx, "ps", "w")
		if err != nil {
			s.writeErrorStatus(w, http.StatusInternalServerError, "failed to list processes")

			return
		}
	}

	var procs []processInfo

	lines := strings.Split(out, "\n")
	for i, line := range lines {
		if i == 0 { // skip header
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}

		pid, _ := strconv.Atoi(fields[0])
		user := fields[1]
		vsz, _ := strconv.ParseInt(fields[2], 10, 64)
		stat := fields[3]
		cmd := strings.Join(fields[4:], " ")

		procs = append(procs, processInfo{
			PID:  pid,
			User: user,
			VSZ:  vsz,
			Stat: stat,
			Cmd:  cmd,
		})
	}

	if procs == nil {
		procs = []processInfo{}
	}

	s.writeJSON(w, procs)
}

func (s *Server) handleSystemStorage(w http.ResponseWriter, r *http.Request) {
	out, err := runCmd(r.Context(), "df", "-h")
	if err != nil {
		s.writeErrorStatus(w, http.StatusInternalServerError, "failed to get storage info")

		return
	}

	var entries []storageEntry

	for i, line := range strings.Split(out, "\n") {
		if i == 0 {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 6 {
			continue
		}

		entries = append(entries, storageEntry{
			Filesystem: fields[0],
			Size:       fields[1],
			Used:       fields[2],
			Available:  fields[3],
			UsePercent: fields[4],
			MountedOn:  fields[5],
		})
	}

	if entries == nil {
		entries = []storageEntry{}
	}

	s.writeJSON(w, entries)
}

func (s *Server) handleSystemLogs(w http.ResponseWriter, r *http.Request) {
	linesParam := r.URL.Query().Get("lines")
	numLines := 50

	if linesParam != "" {
		if n, err := strconv.Atoi(linesParam); err == nil && n > 0 {
			numLines = n
		}
	}

	ctx := r.Context()

	out, err := runCmd(ctx, "logread")
	if err != nil {
		// Fallback: try dmesg.
		out, err = runCmd(ctx, "dmesg")
		if err != nil {
			s.writeErrorStatus(w, http.StatusInternalServerError, "failed to read logs")

			return
		}
	}

	allLines := strings.Split(out, "\n")
	start := 0

	if len(allLines) > numLines {
		start = len(allLines) - numLines
	}

	entries := make([]logEntry, 0, numLines)

	for _, l := range allLines[start:] {
		if l != "" {
			entries = append(entries, logEntry{Line: l})
		}
	}

	s.writeJSON(w, entries)
}

func (s *Server) handleSystemReboot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeErrorStatus(w, http.StatusMethodNotAllowed, "method not allowed")

		return
	}

	s.writeJSON(w, map[string]string{statusKey: "rebooting"})

	go func() {
		time.Sleep(1 * time.Second)

		_ = exec.CommandContext(context.Background(), "/sbin/reboot").Run() //nolint:gosec
	}()
}

func (s *Server) handleSystemRestartService(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeErrorStatus(w, http.StatusMethodNotAllowed, "method not allowed")

		return
	}

	var req restartServiceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeErrorStatus(w, http.StatusBadRequest, "invalid request body")

		return
	}

	if req.Service == "" || !safeServiceRe.MatchString(req.Service) {
		s.writeErrorStatus(w, http.StatusBadRequest, "invalid service name")

		return
	}

	initScript := filepath.Join("/etc/init.d", req.Service)
	if _, err := os.Stat(initScript); os.IsNotExist(err) {
		s.writeErrorStatus(w, http.StatusNotFound, "service not found")

		return
	}

	if _, err := runCmd(r.Context(), initScript, "restart"); err != nil {
		s.writeErrorStatus(w, http.StatusInternalServerError, fmt.Sprintf("failed to restart service: %v", err))

		return
	}

	s.writeJSON(w, map[string]string{statusKey: "restarted", "service": req.Service})
}

// ---------------------------------------------------------------------------
// Network Handlers
// ---------------------------------------------------------------------------

func (s *Server) handleNetworkInterfaces(w http.ResponseWriter, _ *http.Request) {
	entries, err := os.ReadDir("/sys/class/net")
	if err != nil {
		s.writeErrorStatus(w, http.StatusInternalServerError, "failed to read network interfaces")

		return
	}

	// Parse /proc/net/dev for stats.
	devData, _ := os.ReadFile("/proc/net/dev")
	devStats := parseProcNetDev(string(devData))

	var ifaces []networkInterface

	for _, entry := range entries {
		name := entry.Name()
		iface := networkInterface{Name: name}

		if stats, ok := devStats[name]; ok {
			iface.RxBytes = stats.rxBytes
			iface.RxPackets = stats.rxPackets
			iface.RxErrors = stats.rxErrors
			iface.TxBytes = stats.txBytes
			iface.TxPackets = stats.txPackets
			iface.TxErrors = stats.txErrors
		}

		iface.Operstate = readFileString(fmt.Sprintf("/sys/class/net/%s/operstate", name))

		if mtuStr := readFileString(fmt.Sprintf("/sys/class/net/%s/mtu", name)); mtuStr != "" {
			iface.MTU, _ = strconv.Atoi(mtuStr)
		}

		ifaces = append(ifaces, iface)
	}

	if ifaces == nil {
		ifaces = []networkInterface{}
	}

	s.writeJSON(w, ifaces)
}

type netDevStats struct {
	rxBytes, rxPackets, rxErrors int64
	txBytes, txPackets, txErrors int64
}

func parseProcNetDev(content string) map[string]netDevStats {
	result := make(map[string]netDevStats)

	for i, line := range strings.Split(content, "\n") {
		if i < 2 { // skip headers
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}

		name := strings.TrimSpace(parts[0])
		fields := strings.Fields(parts[1])

		if len(fields) < 10 {
			continue
		}

		rxB, _ := strconv.ParseInt(fields[0], 10, 64)
		rxP, _ := strconv.ParseInt(fields[1], 10, 64)
		rxE, _ := strconv.ParseInt(fields[2], 10, 64)
		txB, _ := strconv.ParseInt(fields[8], 10, 64)
		txP, _ := strconv.ParseInt(fields[9], 10, 64)
		txE, _ := strconv.ParseInt(fields[10], 10, 64)

		result[name] = netDevStats{
			rxBytes: rxB, rxPackets: rxP, rxErrors: rxE,
			txBytes: txB, txPackets: txP, txErrors: txE,
		}
	}

	return result
}

func (s *Server) handleNetworkWifi(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Try iwinfo first.
	out, err := runCmd(ctx, "iwinfo")
	if err == nil && out != "" {
		statuses := parseIwinfo(out)
		s.writeJSON(w, statuses)

		return
	}

	// Fallback: enumerate wlan interfaces.
	matches, _ := filepath.Glob("/sys/class/net/wlan*")
	statuses := make([]wifiStatus, 0, len(matches))

	for _, m := range matches {
		name := filepath.Base(m)
		ws := wifiStatus{Interface: name}

		// Try iw dev <name> info
		if info, err2 := runCmd(ctx, "iw", "dev", name, "info"); err2 == nil {
			for _, line := range strings.Split(info, "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "ssid ") {
					ws.SSID = strings.TrimPrefix(line, "ssid ")
				}

				if strings.HasPrefix(line, "type ") {
					ws.Mode = strings.TrimPrefix(line, "type ")
				}

				if strings.HasPrefix(line, "channel ") {
					parts := strings.Fields(line)
					if len(parts) >= 2 {
						ws.Channel, _ = strconv.Atoi(parts[1])
					}
				}
			}
		}

		// Try iw dev <name> station dump for client count.
		if dump, err2 := runCmd(ctx, "iw", "dev", name, "station", "dump"); err2 == nil {
			ws.Clients = strings.Count(dump, "Station")
		}

		statuses = append(statuses, ws)
	}

	s.writeJSON(w, statuses)
}

func parseIwinfo(output string) []wifiStatus {
	var statuses []wifiStatus

	blocks := strings.Split(output, "\n\n")
	for _, block := range blocks {
		if strings.TrimSpace(block) == "" {
			continue
		}

		ws := parseIwinfoBlock(block)
		if ws.Interface != "" {
			statuses = append(statuses, ws)
		}
	}

	if statuses == nil {
		statuses = []wifiStatus{}
	}

	return statuses
}

func parseIwinfoBlock(block string) wifiStatus {
	ws := wifiStatus{}

	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		parseIwinfoLine(&ws, line)
	}

	return ws
}

func parseIwinfoLine(ws *wifiStatus, line string) {
	switch {
	case strings.Contains(line, "ESSID:"):
		parts := strings.SplitN(line, "ESSID:", 2)
		if len(parts) == 2 {
			ws.SSID = strings.Trim(strings.TrimSpace(parts[1]), "\"")
		}

		ws.Interface = strings.TrimSpace(parts[0])

	case strings.Contains(line, "Mode:"):
		if idx := strings.Index(line, "Mode:"); idx >= 0 {
			rest := line[idx+5:]
			ws.Mode = strings.Fields(rest)[0]
		}

	case strings.Contains(line, "Channel:"):
		if idx := strings.Index(line, "Channel:"); idx >= 0 {
			parts := strings.Fields(line[idx+8:])
			if len(parts) >= 1 {
				ws.Channel, _ = strconv.Atoi(parts[0])
			}
		}

	case strings.Contains(line, "Signal:"):
		if idx := strings.Index(line, "Signal:"); idx >= 0 {
			parts := strings.Fields(line[idx+7:])
			if len(parts) >= 1 {
				ws.Signal, _ = strconv.Atoi(parts[0])
			}
		}

	case strings.Contains(line, "Bit Rate:"):
		if idx := strings.Index(line, "Bit Rate:"); idx >= 0 {
			rest := line[idx+9:]
			ws.BitRate = strings.TrimSpace(strings.Split(rest, "\n")[0])
		}
	}
}

func (s *Server) handleNetworkRoutes(w http.ResponseWriter, r *http.Request) {
	out, err := runCmd(r.Context(), "ip", "route", "show")
	if err != nil {
		s.writeErrorStatus(w, http.StatusInternalServerError, "failed to get routes")

		return
	}

	var routes []routeEntry

	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}

		route := parseRouteLine(line)
		routes = append(routes, route)
	}

	if routes == nil {
		routes = []routeEntry{}
	}

	s.writeJSON(w, routes)
}

func parseRouteLine(line string) routeEntry {
	fields := strings.Fields(line)
	r := routeEntry{}

	if len(fields) == 0 {
		return r
	}

	r.Destination = fields[0]

	for i := 1; i < len(fields)-1; i++ {
		switch fields[i] {
		case "via":
			r.Gateway = fields[i+1]
		case "dev":
			r.Device = fields[i+1]
		case "proto":
			r.Protocol = fields[i+1]
		case "scope":
			r.Scope = fields[i+1]
		case "metric":
			r.Metric, _ = strconv.Atoi(fields[i+1])
		}
	}

	return r
}

func (s *Server) handleNetworkBatman(w http.ResponseWriter, r *http.Request) {
	info := batmanInfo{
		Originators: []batmanOriginator{},
		Gateways:    []string{},
	}

	ctx := r.Context()

	if out, err := runCmd(ctx, "batctl", "o"); err == nil {
		info.Originators = parseBatctlOriginators(out)
	} else {
		// Fallback: read from debugfs.
		data := readFileString("/sys/kernel/debug/batman_adv/bat0/originators")
		if data != "" {
			info.Originators = parseBatctlOriginators(data)
		}
	}

	if out, err := runCmd(ctx, "batctl", "gwl"); err == nil {
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "[") && !strings.Contains(line, "Gateway") {
				info.Gateways = append(info.Gateways, line)
			}
		}
	} else {
		data := readFileString("/sys/kernel/debug/batman_adv/bat0/gateways")
		if data != "" {
			for _, line := range strings.Split(data, "\n") {
				line = strings.TrimSpace(line)
				if line != "" && !strings.HasPrefix(line, "[") {
					info.Gateways = append(info.Gateways, line)
				}
			}
		}
	}

	s.writeJSON(w, info)
}

func parseBatctlOriginators(output string) []batmanOriginator {
	var result []batmanOriginator

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "[") || strings.HasPrefix(line, "Originator") {
			continue
		}

		// Typical format: "aa:bb:cc:dd:ee:ff  0.500s   (200) aa:bb:cc:dd:ee:ff [  mesh0]"
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}

		orig := batmanOriginator{Originator: fields[0]}

		if strings.HasSuffix(fields[1], "s") {
			orig.LastSeen = fields[1]
		}

		// Parse TQ value – it may be in parentheses.
		for _, f := range fields {
			f = strings.Trim(f, "()")
			if v, err := strconv.ParseFloat(f, 64); err == nil && v > 0 {
				orig.TQuality = v

				break
			}
		}

		if len(fields) >= 4 {
			orig.NextHop = fields[3]
		}

		if len(fields) >= 5 {
			orig.OutgoingIf = strings.Trim(fields[len(fields)-1], "[]")
		}

		result = append(result, orig)
	}

	if result == nil {
		result = []batmanOriginator{}
	}

	return result
}

// ---------------------------------------------------------------------------
// Settings Handlers
// ---------------------------------------------------------------------------

const openmanetdConfigPath = "/etc/openmanetd/config.yml"

func (s *Server) handleSettingsConfigGet(w http.ResponseWriter, _ *http.Request) {
	data, err := os.ReadFile(openmanetdConfigPath)
	if err != nil {
		s.writeErrorStatus(w, http.StatusInternalServerError, fmt.Sprintf("failed to read config: %v", err))

		return
	}

	var cfg map[string]any
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		s.writeErrorStatus(w, http.StatusInternalServerError, fmt.Sprintf("failed to parse config: %v", err))

		return
	}

	s.writeJSON(w, cfg)
}

func (s *Server) handleSettingsConfigPost(w http.ResponseWriter, r *http.Request) {
	var incoming map[string]any
	if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
		s.writeErrorStatus(w, http.StatusBadRequest, "invalid JSON body")

		return
	}

	// Read the existing config so we merge changes into it
	// instead of replacing the entire file.
	existing := make(map[string]any)
	if data, err := os.ReadFile(openmanetdConfigPath); err == nil {
		_ = yaml.Unmarshal(data, &existing)
	}

	// Deep merge incoming changes into existing config.
	deepMerge(existing, incoming)

	// Remove the restart_service flag before writing — it's a control field, not config.
	shouldRestart := false

	if restart, ok := existing["restart_service"]; ok {
		if b, ok2 := restart.(bool); ok2 {
			shouldRestart = b
		}

		delete(existing, "restart_service")
	}

	yamlData, err := yaml.Marshal(existing)
	if err != nil {
		s.writeErrorStatus(w, http.StatusInternalServerError, fmt.Sprintf("failed to marshal config: %v", err))

		return
	}

	if err := os.MkdirAll(filepath.Dir(openmanetdConfigPath), 0o755); err != nil {
		s.writeErrorStatus(w, http.StatusInternalServerError, fmt.Sprintf("failed to create config directory: %v", err))

		return
	}

	if err := os.WriteFile(openmanetdConfigPath, yamlData, 0o644); err != nil {
		s.writeErrorStatus(w, http.StatusInternalServerError, fmt.Sprintf("failed to write config: %v", err))

		return
	}

	if shouldRestart {
		go func() {
			_, _ = runCmd(context.Background(), "/etc/init.d/openmanetd", "restart")
		}()
	}

	s.writeJSON(w, map[string]string{statusKey: "saved"})
}

// deepMerge recursively merges src into dst. Values in src overwrite dst.
// Nested maps are merged recursively rather than replaced.
func deepMerge(dst, src map[string]any) {
	for key, srcVal := range src {
		dstVal, exists := dst[key]
		if !exists {
			dst[key] = srcVal

			continue
		}

		// If both are maps, merge recursively.
		srcMap, srcOk := srcVal.(map[string]any)

		dstMap, dstOk := dstVal.(map[string]any)
		if srcOk && dstOk {
			deepMerge(dstMap, srcMap)

			continue
		}

		// Otherwise overwrite.
		dst[key] = srcVal
	}
}

func (s *Server) handleSettingsConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleSettingsConfigGet(w, r)
	case http.MethodPost:
		s.handleSettingsConfigPost(w, r)
	default:
		s.writeErrorStatus(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleSettingsHostname(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		hostname := readFileString("/proc/sys/kernel/hostname")
		s.writeJSON(w, hostnameResponse{Hostname: hostname})

	case http.MethodPost:
		var req hostnameRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.writeErrorStatus(w, http.StatusBadRequest, "invalid JSON body")

			return
		}

		if req.Hostname == "" || !safeServiceRe.MatchString(req.Hostname) {
			s.writeErrorStatus(w, http.StatusBadRequest, "invalid hostname")

			return
		}

		// Set via uci on OpenWrt.
		cmd := fmt.Sprintf("uci set system.@system[0].hostname='%s' && uci commit system && echo '%s' > /proc/sys/kernel/hostname",
			req.Hostname, req.Hostname)

		if _, err := runCmd(r.Context(), "sh", "-c", cmd); err != nil {
			s.writeErrorStatus(w, http.StatusInternalServerError, fmt.Sprintf("failed to set hostname: %v", err))

			return
		}

		s.writeJSON(w, hostnameResponse(req))

	default:
		s.writeErrorStatus(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// ---------------------------------------------------------------------------
// Board / Version Helpers
// ---------------------------------------------------------------------------

func parseBoardInfo() (board, model string) {
	data, err := os.ReadFile("/etc/board.json")
	if err != nil {
		return readFileString("/tmp/sysinfo/board_name"), readFileString("/tmp/sysinfo/model")
	}

	var parsed map[string]any
	if json.Unmarshal(data, &parsed) != nil {
		return "", ""
	}

	if v, ok := parsed["board_name"]; ok {
		board = fmt.Sprintf("%v", v)
	}

	if m, ok := parsed["model"].(map[string]any); ok {
		if name, ok2 := m["name"]; ok2 {
			model = fmt.Sprintf("%v", name)
		}
	} else if v, ok := parsed["model"]; ok {
		model = fmt.Sprintf("%v", v)
	}

	return board, model
}

func readDistribRelease() string {
	data, err := os.ReadFile("/etc/openwrt_release")
	if err != nil {
		return ""
	}

	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "DISTRIB_RELEASE=") {
			return strings.Trim(strings.TrimPrefix(line, "DISTRIB_RELEASE="), "'\"")
		}
	}

	return ""
}

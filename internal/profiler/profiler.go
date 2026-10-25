package profiler

import (
	"fmt"
	"net"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

type HardwareProfile struct {
	OS                    string   `json:"os"`
	Arch                  string   `json:"arch"`
	CPUCores              int      `json:"cpu_cores"`
	TotalRAMMB            uint64   `json:"total_ram_mb"`
	AvailRAMMB            uint64   `json:"avail_ram_mb"`
	MemoryLoadPercent     uint32   `json:"memory_load_percent"`
	VCRedistInstalled     bool     `json:"vc_redist_installed"`
	RecommendedPHPMemory  string   `json:"recommended_php_memory"`
	RecommendedPostMax    string   `json:"recommended_post_max"`
	RecommendedInnoDBSize string   `json:"recommended_innodb_size"`
	RecommendedMaxConn    int      `json:"recommended_max_connections"`
	LocalIPs              []string `json:"local_ips"`
}

type PortScanResult struct {
	Port        string `json:"port"`
	Available   bool   `json:"available"`
	ProcessID   string `json:"process_id,omitempty"`
	ProcessName string `json:"process_name,omitempty"`
}

type LiveConnection struct {
	Proto      string `json:"proto"`
	LocalPort  string `json:"local_port"`
	RemoteIP   string `json:"remote_ip"`
	RemotePort string `json:"remote_port"`
	State      string `json:"state"`
	Service    string `json:"service"`
}

type LiveMetrics struct {
	CPUPercent        int              `json:"cpu_percent"`
	MemoryLoadPercent uint32           `json:"memory_load_percent"`
	TotalRAMMB        uint64           `json:"total_ram_mb"`
	AvailRAMMB        uint64           `json:"avail_ram_mb"`
	UsedRAMMB         uint64           `json:"used_ram_mb"`
	ApacheConnections int              `json:"apache_connections"`
	MySQLConnections  int              `json:"mysql_connections"`
	ControlConns      int              `json:"control_connections"`
	TotalConnections  int              `json:"total_connections"`
	ActiveConnections []LiveConnection `json:"active_connections"`
	HistoryTimestamps []string         `json:"history_timestamps"`
	HistoryCPU        []int            `json:"history_cpu"`
	HistoryRAM        []int            `json:"history_ram"`
	HistoryConns      []int            `json:"history_conns"`
}

type memoryStatusEx struct {
	cbSize                  uint32
	dwMemoryLoad            uint32
	ullTotalPhys            uint64
	ullAvailPhys            uint64
	ullTotalPageFile        uint64
	ullAvailPageFile        uint64
	ullTotalVirtual         uint64
	ullAvailVirtual         uint64
	ullAvailExtendedVirtual uint64
}

type fileTime struct {
	dwLowDateTime  uint32
	dwHighDateTime uint32
}

var (
	kernel32Dll             = syscall.NewLazyDLL("kernel32.dll")
	procGlobalMemoryStatusEx = kernel32Dll.NewProc("GlobalMemoryStatusEx")
	procGetSystemTimes       = kernel32Dll.NewProc("GetSystemTimes")

	metricsMu         sync.RWMutex
	lastSampleTime    time.Time
	lastIdleTime      uint64
	lastKernelTime    uint64
	lastUserTime      uint64
	lastCPUPercent    int
	cachedMetrics     *LiveMetrics
	historyTimestamps []string
	historyCPU        []int
	historyRAM        []int
	historyConns      []int
)

func InspectSystem() HardwareProfile {
	var totalMB, availMB uint64 = 8192, 4096
	var memLoad uint32 = 50

	var mem memoryStatusEx
	mem.cbSize = uint32(unsafe.Sizeof(mem))
	ret, _, _ := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&mem)))
	if ret != 0 {
		totalMB = mem.ullTotalPhys / (1024 * 1024)
		availMB = mem.ullAvailPhys / (1024 * 1024)
		memLoad = mem.dwMemoryLoad
	}

	phpMem := "256M"
	postMax := "64M"
	innoDB := "256M"
	maxConn := 100

	switch {
	case totalMB >= 30000: // 32GB+
		phpMem = "1024M"
		postMax = "256M"
		innoDB = "2048M"
		maxConn = 300
	case totalMB >= 15000: // 16GB+
		phpMem = "512M"
		postMax = "128M"
		innoDB = "1024M"
		maxConn = 200
	case totalMB >= 7500: // 8GB+
		phpMem = "256M"
		postMax = "64M"
		innoDB = "512M"
		maxConn = 150
	default: // <= 4GB
		phpMem = "128M"
		postMax = "32M"
		innoDB = "128M"
		maxConn = 75
	}

	return HardwareProfile{
		OS:                    runtime.GOOS,
		Arch:                  runtime.GOARCH,
		CPUCores:              runtime.NumCPU(),
		TotalRAMMB:            totalMB,
		AvailRAMMB:            availMB,
		MemoryLoadPercent:     memLoad,
		VCRedistInstalled:     checkVCRedist(),
		RecommendedPHPMemory:  phpMem,
		RecommendedPostMax:    postMax,
		RecommendedInnoDBSize: innoDB,
		RecommendedMaxConn:    maxConn,
		LocalIPs:              getLocalIPs(),
	}
}

func checkVCRedist() bool {
	system32 := `C:\Windows\System32\vcruntime140.dll`
	cmd := exec.Command("cmd", "/c", "if exist "+system32+" (echo 1) else (echo 0)")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		return true
	}
	return strings.TrimSpace(string(out)) == "1"
}

func getLocalIPs() []string {
	var ips []string
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return []string{"127.0.0.1"}
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				ips = append(ips, ipnet.IP.String())
			}
		}
	}
	if len(ips) == 0 {
		ips = append(ips, "127.0.0.1")
	}
	return ips
}

func IsPortAvailable(port string) bool {
	// 1. Check active TCP listeners via netstat to prevent Windows SO_REUSEADDR false positives
	cmd := exec.Command("cmd", "/c", fmt.Sprintf("netstat -ano | findstr LISTENING | findstr :%s", port))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if out, err := cmd.Output(); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			fields := strings.Fields(strings.TrimSpace(line))
			if len(fields) >= 2 {
				localAddr := fields[1]
				if strings.HasSuffix(localAddr, ":"+port) {
					return false
				}
			}
		}
	}

	// 2. Also verify actual socket bind on IPv4 and IPv6
	for _, host := range []string{"0.0.0.0", "127.0.0.1", "[::]"} {
		ln, err := net.Listen("tcp", host+":"+port)
		if err != nil {
			if host == "[::]" {
				continue
			}
			return false
		}
		_ = ln.Close()
	}
	return true
}

func FindSmartPort(preferred string, fallbacks []string, startRange int) (string, PortScanResult) {
	info := InspectPort(preferred)
	if info.Available {
		return preferred, info
	}
	for _, fb := range fallbacks {
		if IsPortAvailable(fb) {
			return fb, info
		}
	}
	portNum := startRange
	for {
		candidate := strconv.Itoa(portNum)
		if IsPortAvailable(candidate) {
			return candidate, info
		}
		portNum++
		if portNum > 65000 {
			break
		}
	}
	return preferred, info
}

func InspectPort(port string) PortScanResult {
	res := PortScanResult{
		Port:      port,
		Available: IsPortAvailable(port),
	}
	if res.Available {
		return res
	}

	cmd := exec.Command("cmd", "/c", fmt.Sprintf("netstat -ano | findstr :%s", port))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		return res
	}

	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "LISTENING") {
			fields := strings.Fields(line)
			if len(fields) >= 5 {
				pid := fields[len(fields)-1]
				res.ProcessID = pid
				res.ProcessName = getProcessNameByPID(pid)
				break
			}
		}
	}
	return res
}

func getProcessNameByPID(pid string) string {
	if pid == "4" {
		return "System (IIS / HTTP.sys)"
	}
	cmd := exec.Command("tasklist", "/FI", "PID eq "+pid, "/NH", "/FO", "CSV")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		return "Unknown"
	}
	parts := strings.Split(strings.TrimSpace(string(out)), ",")
	if len(parts) > 0 {
		return strings.Trim(parts[0], `"`)
	}
	return "Unknown"
}

func init() {
	sampleCPU()
	now := time.Now()
	for i := 15; i >= 1; i-- {
		t := now.Add(-time.Duration(i*2) * time.Second)
		historyTimestamps = append(historyTimestamps, t.Format("15:04:05"))
		historyCPU = append(historyCPU, 0)
		historyRAM = append(historyRAM, 0)
		historyConns = append(historyConns, 0)
	}
}

func sampleCPU() int {
	var idleFt, kernelFt, userFt fileTime
	r, _, _ := procGetSystemTimes.Call(
		uintptr(unsafe.Pointer(&idleFt)),
		uintptr(unsafe.Pointer(&kernelFt)),
		uintptr(unsafe.Pointer(&userFt)),
	)
	if r == 0 {
		return lastCPUPercent
	}

	idle := uint64(idleFt.dwLowDateTime) | (uint64(idleFt.dwHighDateTime) << 32)
	kernel := uint64(kernelFt.dwLowDateTime) | (uint64(kernelFt.dwHighDateTime) << 32)
	user := uint64(userFt.dwLowDateTime) | (uint64(userFt.dwHighDateTime) << 32)

	if lastIdleTime != 0 {
		idleDelta := idle - lastIdleTime
		kernelDelta := kernel - lastKernelTime
		userDelta := user - lastUserTime
		totalDelta := kernelDelta + userDelta
		if totalDelta > 0 {
			if totalDelta > idleDelta {
				pct := int(((totalDelta - idleDelta) * 100) / totalDelta)
				if pct > 100 {
					pct = 100
				}
				lastCPUPercent = pct
			} else {
				lastCPUPercent = 0
			}
		}
	}

	lastIdleTime = idle
	lastKernelTime = kernel
	lastUserTime = user
	return lastCPUPercent
}

func sampleMemory() (totalMB, availMB, usedMB uint64, loadPercent uint32) {
	var mem memoryStatusEx
	mem.cbSize = uint32(unsafe.Sizeof(mem))
	ret, _, _ := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&mem)))
	if ret != 0 {
		totalMB = mem.ullTotalPhys / (1024 * 1024)
		availMB = mem.ullAvailPhys / (1024 * 1024)
		usedMB = totalMB - availMB
		loadPercent = mem.dwMemoryLoad
	} else {
		totalMB, availMB, usedMB, loadPercent = 8192, 4096, 4096, 50
	}
	return
}

func scanActiveConnections(apachePort, mysqlPort, controlPort string) ([]LiveConnection, int, int, int, int) {
	cmd := exec.Command("cmd", "/c", "netstat -n -p tcp")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		return nil, 0, 0, 0, 0
	}

	ports := map[string]string{
		apachePort:  "Apache HTTP",
		"443":       "Apache HTTPS",
		mysqlPort:   "MySQL / MariaDB",
		controlPort: "AXAMP Dashboard",
	}

	var active []LiveConnection
	var apacheCount, mysqlCount, controlCount, totalCount int

	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		proto := fields[0]
		local := fields[1]
		remote := fields[2]
		state := fields[3]

		lastColon := strings.LastIndex(local, ":")
		if lastColon == -1 {
			continue
		}
		lport := local[lastColon+1:]

		svc, ok := ports[lport]
		if !ok {
			continue
		}

		if state == "LISTENING" {
			continue
		}

		totalCount++
		if strings.HasPrefix(svc, "Apache") {
			apacheCount++
		} else if strings.HasPrefix(svc, "MySQL") {
			mysqlCount++
		} else if strings.HasPrefix(svc, "AXAMP") {
			controlCount++
		}

		remoteIP := remote
		remotePort := ""
		rColon := strings.LastIndex(remote, ":")
		if rColon != -1 {
			remoteIP = remote[:rColon]
			remotePort = remote[rColon+1:]
		}

		remoteIP = strings.TrimPrefix(remoteIP, "[")
		remoteIP = strings.TrimSuffix(remoteIP, "]")

		active = append(active, LiveConnection{
			Proto:      proto,
			LocalPort:  lport,
			RemoteIP:   remoteIP,
			RemotePort: remotePort,
			State:      state,
			Service:    svc,
		})
	}

	return active, apacheCount, mysqlCount, controlCount, totalCount
}

func GetLiveMetrics(apachePort, mysqlPort, controlPort string) LiveMetrics {
	metricsMu.Lock()
	defer metricsMu.Unlock()

	now := time.Now()
	if cachedMetrics != nil && now.Sub(lastSampleTime) < 900*time.Millisecond {
		return *cachedMetrics
	}
	lastSampleTime = now

	cpu := sampleCPU()
	totMB, avMB, usMB, memLoad := sampleMemory()
	conns, apCount, myCount, ctCount, totConns := scanActiveConnections(apachePort, mysqlPort, controlPort)

	ts := now.Format("15:04:05")
	historyTimestamps = append(historyTimestamps, ts)
	historyCPU = append(historyCPU, cpu)
	historyRAM = append(historyRAM, int(memLoad))
	historyConns = append(historyConns, totConns)

	const maxHistory = 25
	if len(historyTimestamps) > maxHistory {
		historyTimestamps = historyTimestamps[len(historyTimestamps)-maxHistory:]
		historyCPU = historyCPU[len(historyCPU)-maxHistory:]
		historyRAM = historyRAM[len(historyRAM)-maxHistory:]
		historyConns = historyConns[len(historyConns)-maxHistory:]
	}

	res := LiveMetrics{
		CPUPercent:        cpu,
		MemoryLoadPercent: memLoad,
		TotalRAMMB:        totMB,
		AvailRAMMB:        avMB,
		UsedRAMMB:         usMB,
		ApacheConnections: apCount,
		MySQLConnections:  myCount,
		ControlConns:      ctCount,
		TotalConnections:  totConns,
		ActiveConnections: conns,
		HistoryTimestamps: append([]string(nil), historyTimestamps...),
		HistoryCPU:        append([]int(nil), historyCPU...),
		HistoryRAM:        append([]int(nil), historyRAM...),
		HistoryConns:      append([]int(nil), historyConns...),
	}
	cachedMetrics = &res
	return res
}


package profiler

import (
	"fmt"
	"net"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

type HardwareProfile struct {
	OS                    string `json:"os"`
	Arch                  string `json:"arch"`
	CPUCores              int    `json:"cpu_cores"`
	TotalRAMMB            uint64 `json:"total_ram_mb"`
	AvailRAMMB            uint64 `json:"avail_ram_mb"`
	MemoryLoadPercent     uint32 `json:"memory_load_percent"`
	VCRedistInstalled     bool   `json:"vc_redist_installed"`
	RecommendedPHPMemory  string `json:"recommended_php_memory"`
	RecommendedPostMax    string `json:"recommended_post_max"`
	RecommendedInnoDBSize string `json:"recommended_innodb_size"`
	RecommendedMaxConn    int    `json:"recommended_max_connections"`
	LocalIPs              []string `json:"local_ips"`
}

type PortScanResult struct {
	Port        string `json:"port"`
	Available   bool   `json:"available"`
	ProcessID   string `json:"process_id,omitempty"`
	ProcessName string `json:"process_name,omitempty"`
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

func InspectSystem() HardwareProfile {
	var totalMB, availMB uint64 = 8192, 4096
	var memLoad uint32 = 50

	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	globalMemoryStatusEx := kernel32.NewProc("GlobalMemoryStatusEx")

	var mem memoryStatusEx
	mem.cbSize = uint32(unsafe.Sizeof(mem))
	ret, _, _ := globalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&mem)))
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

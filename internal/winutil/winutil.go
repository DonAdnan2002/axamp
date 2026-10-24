package winutil

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

const runRegistryKey = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
const appRegistryName = "AXAMP_SmartStack"

func SetWindowsAutoStart(enable bool) error {
	exePath, err := os.Executable()
	if err != nil {
		return err
	}
	if enable {
		val := fmt.Sprintf(`"%s" --minimized`, exePath)
		cmd := exec.Command("reg", "add", runRegistryKey, "/v", appRegistryName, "/t", "REG_SZ", "/d", val, "/f")
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		return cmd.Run()
	}
	cmd := exec.Command("reg", "delete", runRegistryKey, "/v", appRegistryName, "/f")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	_ = cmd.Run()
	return nil
}

func OpenFolder(path string) error {
	cmd := exec.Command("explorer.exe", path)
	return cmd.Start()
}

func OpenBrowser(url string) error {
	cmd := exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	return cmd.Start()
}

func OpenAppWindow(url string) error {
	// Attempt to open in sleek Edge App mode (native-like desktop window on Windows 10/11)
	edgePaths := []string{
		`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
		`C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
	}
	for _, ep := range edgePaths {
		if _, err := os.Stat(ep); err == nil {
			cmd := exec.Command(ep, "--app="+url, "--window-size=1220,820")
			return cmd.Start()
		}
	}
	return OpenBrowser(url)
}

func SyncLocalHostsDomain(domain string, add bool) error {
	hostsPath := filepath.Join(os.Getenv("SystemRoot"), "System32", "drivers", "etc", "hosts")
	content, err := os.ReadFile(hostsPath)
	if err != nil {
		return err
	}
	lines := strings.Split(string(content), "\n")
	var updated []string
	entry := fmt.Sprintf("127.0.0.1 %s # AXAMP-AUTO", domain)
	found := false

	for _, line := range lines {
		if strings.Contains(line, domain) && strings.Contains(line, "AXAMP-AUTO") {
			found = true
			if !add {
				continue
			}
		}
		updated = append(updated, line)
	}

	if add && !found {
		updated = append(updated, entry)
	}

	return os.WriteFile(hostsPath, []byte(strings.Join(updated, "\n")), 0644)
}

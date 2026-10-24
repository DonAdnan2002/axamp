package supervisor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"axamp/internal/config"
	"axamp/internal/generator"
	"axamp/internal/profiler"
)

type ServiceState string

const (
	StateStopped  ServiceState = "stopped"
	StateStarting ServiceState = "starting"
	StateRunning  ServiceState = "running"
	StateMissing  ServiceState = "binary_missing"
	StateError    ServiceState = "error"
)

type ServiceStatus struct {
	Name              string       `json:"name"`
	State             ServiceState `json:"state"`
	PID               int          `json:"pid"`
	Port              string       `json:"port"`
	UptimeSec         int64        `json:"uptime_sec"`
	Restarts          int          `json:"restarts"`
	LastError         string       `json:"last_error,omitempty"`
	BinaryPath        string       `json:"binary_path"`
	WinServiceInstalled bool       `json:"win_service_installed"`
	WinServiceRunning   bool       `json:"win_service_running"`
}

type Manager struct {
	mu          sync.Mutex
	cfg         *config.AppConfig
	apacheCmd   *exec.Cmd
	mysqlCmd    *exec.Cmd
	apacheStart time.Time
	mysqlStart  time.Time
	apacheRest  int
	mysqlRest   int
	apacheErr   string
	mysqlErr    string
	stopApache  bool
	stopMySQL   bool
	events      []string
}

func NewManager(cfg *config.AppConfig) *Manager {
	m := &Manager{
		cfg:        cfg,
		stopApache: true,
		stopMySQL:  true,
		events:     []string{},
	}
	m.RotateLogsIfNeeded()
	m.logEvent("AXAMP Supervisor initialized")
	return m
}

func (m *Manager) logEvent(msg string) {
	ts := time.Now().Format("15:04:05")
	entry := fmt.Sprintf("[%s] %s", ts, msg)
	m.events = append([]string{entry}, m.events...)
	if len(m.events) > 100 {
		m.events = m.events[:100]
	}
}

func (m *Manager) GetEvents() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.events))
	copy(out, m.events)
	return out
}

func (m *Manager) RotateLogsIfNeeded() {
	logsDir := filepath.Join(m.cfg.RootDir, "logs")
	entries, err := os.ReadDir(logsDir)
	if err != nil {
		return
	}
	maxBytes := int64(m.cfg.MaxLogSizeMB) * 1024 * 1024
	if maxBytes <= 0 {
		maxBytes = 50 * 1024 * 1024
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".log" {
			continue
		}
		full := filepath.Join(logsDir, e.Name())
		if info, err := os.Stat(full); err == nil && info.Size() > maxBytes {
			backup := full + ".old"
			_ = os.Remove(backup)
			_ = os.Rename(full, backup)
		}
	}
}

func (m *Manager) ApacheBinary() string {
	return filepath.Join(m.cfg.RootDir, "bin", "apache", "bin", "httpd.exe")
}

func (m *Manager) MySQLBinary() string {
	return filepath.Join(m.cfg.RootDir, "bin", "mysql", "bin", "mysqld.exe")
}

// buildIsolatedEnv ensures Apache, PHP, and MySQL load their DLLs and OpenSSL config exclusively from AXAMP's portable directory
func (m *Manager) buildIsolatedEnv() []string {
	phpDir := filepath.Join(m.cfg.RootDir, "bin", "php")
	apacheBin := filepath.Join(m.cfg.RootDir, "bin", "apache", "bin")
	mysqlBin := filepath.Join(m.cfg.RootDir, "bin", "mysql", "bin")
	confDir := filepath.Join(m.cfg.RootDir, "conf")

	opensslCnf := filepath.Join(m.cfg.RootDir, "bin", "apache", "conf", "openssl.cnf")
	if _, err := os.Stat(opensslCnf); os.IsNotExist(err) {
		opensslCnf = filepath.Join(phpDir, "extras", "ssl", "openssl.cnf")
	}

	sysRoot := os.Getenv("SystemRoot")
	if sysRoot == "" {
		sysRoot = `C:\Windows`
	}
	cleanPath := strings.Join([]string{
		phpDir,
		apacheBin,
		mysqlBin,
		filepath.Join(sysRoot, "System32"),
		sysRoot,
		filepath.Join(sysRoot, "System32", "Wbem"),
	}, ";")

	var env []string
	for _, kv := range os.Environ() {
		upper := strings.ToUpper(kv)
		if strings.HasPrefix(upper, "PATH=") ||
			strings.HasPrefix(upper, "OPENSSL_CONF=") ||
			strings.HasPrefix(upper, "PHPRC=") {
			continue
		}
		env = append(env, kv)
	}

	env = append(env,
		"PATH="+cleanPath,
		"PHPRC="+confDir,
		"OPENSSL_CONF="+opensslCnf,
		"AXAMP_CREATOR=Adnan Sultan Hassan (donadnan2002@gmail.com)",
	)
	return env
}

func queryWinServiceState(serviceName string) (installed bool, running bool) {
	cmd := exec.Command("sc.exe", "query", serviceName)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		return false, false
	}
	text := string(out)
	if strings.Contains(text, "STATE") {
		return true, strings.Contains(text, "RUNNING")
	}
	return false, false
}

func (m *Manager) Status() map[string]ServiceStatus {
	m.mu.Lock()
	defer m.mu.Unlock()

	apacheBin := m.ApacheBinary()
	mysqlBin := m.MySQLBinary()

	apSvcInst, apSvcRun := queryWinServiceState("AXAMP_Apache")
	mySvcInst, mySvcRun := queryWinServiceState("AXAMP_MySQL")

	apacheSt := ServiceStatus{
		Name:                "Apache HTTP Server",
		Port:                m.cfg.ApachePort,
		Restarts:            m.apacheRest,
		LastError:           m.apacheErr,
		BinaryPath:          apacheBin,
		State:               StateStopped,
		WinServiceInstalled: apSvcInst,
		WinServiceRunning:   apSvcRun,
	}
	if _, err := os.Stat(apacheBin); os.IsNotExist(err) {
		apacheSt.State = StateMissing
	} else if (m.apacheCmd != nil && m.apacheCmd.Process != nil) || apSvcRun {
		apacheSt.State = StateRunning
		if m.apacheCmd != nil && m.apacheCmd.Process != nil {
			apacheSt.PID = m.apacheCmd.Process.Pid
			apacheSt.UptimeSec = int64(time.Since(m.apacheStart).Seconds())
		}
	}

	mysqlSt := ServiceStatus{
		Name:                "MySQL / MariaDB Server",
		Port:                m.cfg.MySQLPort,
		Restarts:            m.mysqlRest,
		LastError:           m.mysqlErr,
		BinaryPath:          mysqlBin,
		State:               StateStopped,
		WinServiceInstalled: mySvcInst,
		WinServiceRunning:   mySvcRun,
	}
	if _, err := os.Stat(mysqlBin); os.IsNotExist(err) {
		mysqlSt.State = StateMissing
	} else if (m.mysqlCmd != nil && m.mysqlCmd.Process != nil) || mySvcRun {
		mysqlSt.State = StateRunning
		if m.mysqlCmd != nil && m.mysqlCmd.Process != nil {
			mysqlSt.PID = m.mysqlCmd.Process.Pid
			mysqlSt.UptimeSec = int64(time.Since(m.mysqlStart).Seconds())
		}
	}

	return map[string]ServiceStatus{
		"apache": apacheSt,
		"mysql":  mysqlSt,
	}
}

func (m *Manager) EnsureSmartPortsAndConfigs() error {
	hw := profiler.InspectSystem()

	if !profiler.IsPortAvailable(m.cfg.ApachePort) && (m.apacheCmd == nil || m.apacheCmd.Process == nil) {
		newPort, conflict := profiler.FindSmartPort(m.cfg.ApachePort, []string{"8080", "8081", "8888"}, 8082)
		if newPort != m.cfg.ApachePort {
			m.mu.Lock()
			m.logEvent(fmt.Sprintf("Port %s occupied by %s (PID %s) -> Auto-switched Apache to %s",
				m.cfg.ApachePort, conflict.ProcessName, conflict.ProcessID, newPort))
			m.cfg.ApachePort = newPort
			m.mu.Unlock()
			_ = m.cfg.Save()
		}
	}

	if !profiler.IsPortAvailable(m.cfg.MySQLPort) && (m.mysqlCmd == nil || m.mysqlCmd.Process == nil) {
		newPort, conflict := profiler.FindSmartPort(m.cfg.MySQLPort, []string{"3307", "3308", "3309"}, 3310)
		if newPort != m.cfg.MySQLPort {
			m.mu.Lock()
			m.logEvent(fmt.Sprintf("Port %s occupied by %s (PID %s) -> Auto-switched MySQL to %s",
				m.cfg.MySQLPort, conflict.ProcessName, conflict.ProcessID, newPort))
			m.cfg.MySQLPort = newPort
			m.mu.Unlock()
			_ = m.cfg.Save()
		}
	}

	return generator.GenerateAll(m.cfg, hw)
}

func (m *Manager) ensureMySQLDataInitialized() {
	sysMysqlDir := filepath.Join(m.cfg.RootDir, "data", "mysql")
	if info, err := os.Stat(sysMysqlDir); err == nil && info.IsDir() {
		return
	}

	mysqlBinDir := filepath.Join(m.cfg.RootDir, "bin", "mysql", "bin")
	dataDir := filepath.Join(m.cfg.RootDir, "data")
	confFile := filepath.Join(m.cfg.RootDir, "conf", "my.ini")

	// Check for MariaDB mysql_install_db.exe first
	mariaInit := filepath.Join(mysqlBinDir, "mysql_install_db.exe")
	if _, err := os.Stat(mariaInit); err == nil {
		m.logEvent("Initializing fresh MariaDB data directory...")
		cmd := exec.Command(mariaInit, "--datadir="+dataDir)
		cmd.Dir = filepath.Join(m.cfg.RootDir, "bin", "mysql")
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		_ = cmd.Run()
		return
	}

	// Otherwise use MySQL mysqld --initialize-insecure
	mysqldBin := m.MySQLBinary()
	if _, err := os.Stat(mysqldBin); err == nil {
		m.logEvent("Initializing fresh MySQL data directory (--initialize-insecure)...")
		cmd := exec.Command(mysqldBin, "--defaults-file="+confFile, "--initialize-insecure")
		cmd.Dir = filepath.Join(m.cfg.RootDir, "bin", "mysql")
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		_ = cmd.Run()
	}
}

func (m *Manager) StartApache() error {
	if err := m.EnsureSmartPortsAndConfigs(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.apacheCmd != nil && m.apacheCmd.Process != nil {
		return nil
	}

	bin := m.ApacheBinary()
	if _, err := os.Stat(bin); os.IsNotExist(err) {
		m.apacheErr = "httpd.exe غير موجود في bin/apache/bin (استخدم مدير الحزم لتثبيته أو استنساخه)"
		m.logEvent("Apache start skipped: " + m.apacheErr)
		return fmt.Errorf("%s", m.apacheErr)
	}

	confFile := filepath.Join(m.cfg.RootDir, "conf", "httpd.conf")
	cmd := exec.Command(bin, "-f", confFile)
	cmd.Dir = filepath.Join(m.cfg.RootDir, "bin", "apache")
	cmd.Env = m.buildIsolatedEnv()
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	if err := cmd.Start(); err != nil {
		m.apacheErr = err.Error()
		m.logEvent("Failed to start Apache: " + err.Error())
		return err
	}

	m.apacheCmd = cmd
	m.apacheStart = time.Now()
	m.apacheErr = ""
	m.stopApache = false
	m.logEvent(fmt.Sprintf("Apache started on port %s (PID %d)", m.cfg.ApachePort, cmd.Process.Pid))

	go m.watchApache(cmd)
	return nil
}

func (m *Manager) watchApache(cmd *exec.Cmd) {
	err := cmd.Wait()
	m.mu.Lock()
	wasStoppedIntentionally := m.stopApache
	m.apacheCmd = nil
	if err != nil && !wasStoppedIntentionally {
		m.apacheErr = err.Error()
		m.logEvent("Apache exited unexpectedly: " + err.Error())
	} else {
		m.logEvent("Apache stopped")
	}
	autoHeal := m.cfg.AutoHeal && !wasStoppedIntentionally
	if autoHeal {
		m.apacheRest++
		m.logEvent("Auto-Heal: Restarting Apache in 2s...")
	}
	m.mu.Unlock()

	if autoHeal {
		time.Sleep(2 * time.Second)
		_ = m.StartApache()
	}
}

func (m *Manager) StopApache() error {
	m.mu.Lock()
	m.stopApache = true
	cmd := m.apacheCmd
	m.apacheCmd = nil
	m.mu.Unlock()

	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	return nil
}

func (m *Manager) StartMySQL() error {
	if err := m.EnsureSmartPortsAndConfigs(); err != nil {
		return err
	}

	bin := m.MySQLBinary()
	if _, err := os.Stat(bin); os.IsNotExist(err) {
		m.mu.Lock()
		m.mysqlErr = "mysqld.exe غير موجود في bin/mysql/bin (استخدم مدير الحزم لتثبيته أو استنساخه)"
		m.logEvent("MySQL start skipped: " + m.mysqlErr)
		m.mu.Unlock()
		return fmt.Errorf("%s", m.mysqlErr)
	}

	m.ensureMySQLDataInitialized()

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.mysqlCmd != nil && m.mysqlCmd.Process != nil {
		return nil
	}

	confFile := filepath.Join(m.cfg.RootDir, "conf", "my.ini")
	cmd := exec.Command(bin, "--defaults-file="+confFile)
	cmd.Dir = filepath.Join(m.cfg.RootDir, "bin", "mysql")
	cmd.Env = m.buildIsolatedEnv()
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	if err := cmd.Start(); err != nil {
		m.mysqlErr = err.Error()
		m.logEvent("Failed to start MySQL: " + err.Error())
		return err
	}

	m.mysqlCmd = cmd
	m.mysqlStart = time.Now()
	m.mysqlErr = ""
	m.stopMySQL = false
	m.logEvent(fmt.Sprintf("MySQL started on port %s (PID %d)", m.cfg.MySQLPort, cmd.Process.Pid))

	go m.watchMySQL(cmd)
	return nil
}

func (m *Manager) watchMySQL(cmd *exec.Cmd) {
	err := cmd.Wait()
	m.mu.Lock()
	wasStoppedIntentionally := m.stopMySQL
	m.mysqlCmd = nil
	if err != nil && !wasStoppedIntentionally {
		m.mysqlErr = err.Error()
		m.logEvent("MySQL exited unexpectedly: " + err.Error())
	} else {
		m.logEvent("MySQL stopped")
	}
	autoHeal := m.cfg.AutoHeal && !wasStoppedIntentionally
	if autoHeal {
		m.mysqlRest++
		m.logEvent("Auto-Heal: Restarting MySQL in 2s...")
	}
	m.mu.Unlock()

	if autoHeal {
		time.Sleep(2 * time.Second)
		_ = m.StartMySQL()
	}
}

func (m *Manager) StopMySQL() error {
	m.mu.Lock()
	m.stopMySQL = true
	cmd := m.mysqlCmd
	m.mysqlCmd = nil
	m.mu.Unlock()

	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	return nil
}

// ManageWindowsService installs, removes, starts, or stops native Windows Services (AXAMP_Apache / AXAMP_MySQL)
func (m *Manager) ManageWindowsService(target, action string) error {
	_ = m.EnsureSmartPortsAndConfigs()

	svcName := "AXAMP_Apache"
	binPath := fmt.Sprintf(`"%s" -k runservice -f "%s"`,
		m.ApacheBinary(),
		filepath.Join(m.cfg.RootDir, "conf", "httpd.conf"))

	if target == "mysql" {
		svcName = "AXAMP_MySQL"
		binPath = fmt.Sprintf(`"%s" --defaults-file="%s" %s`,
			m.MySQLBinary(),
			filepath.Join(m.cfg.RootDir, "conf", "my.ini"),
			svcName)
	}

	var cmd *exec.Cmd
	switch action {
	case "install":
		cmd = exec.Command("sc.exe", "create", svcName, "binPath=", binPath, "start=", "auto", "DisplayName=", svcName)
	case "remove":
		_ = exec.Command("sc.exe", "stop", svcName).Run()
		cmd = exec.Command("sc.exe", "delete", svcName)
	case "start":
		cmd = exec.Command("sc.exe", "start", svcName)
	case "stop":
		cmd = exec.Command("sc.exe", "stop", svcName)
	default:
		return fmt.Errorf("إجراء خدمة ويندوز غير معروف: %s", action)
	}

	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	if err != nil {
		m.mu.Lock()
		m.logEvent(fmt.Sprintf("WinService (%s %s) error: %s", svcName, action, strings.TrimSpace(string(out))))
		m.mu.Unlock()
		return fmt.Errorf("فشل تنفيذ الأمر (قد يتطلب تشغيل اللانشر كمسؤول Run as Administrator): %s", strings.TrimSpace(string(out)))
	}

	m.mu.Lock()
	m.logEvent(fmt.Sprintf("WinService %s -> %s succeeded", svcName, action))
	m.mu.Unlock()
	return nil
}

func (m *Manager) StartAll() {
	_ = m.StartMySQL()
	_ = m.StartApache()
}

func (m *Manager) StopAll() {
	_ = m.StopApache()
	_ = m.StopMySQL()
}

func (m *Manager) RestartAll() {
	m.StopAll()
	time.Sleep(600 * time.Millisecond)
	m.StartAll()
}

package odbc

import (
	"fmt"
	"os/exec"
	"strings"
	"syscall"
)

type DriverInfo struct {
	Name         string `json:"name"`
	Architecture string `json:"architecture"`
}

type DSNEntry struct {
	Name         string `json:"name"`
	Driver       string `json:"driver"`
	Scope        string `json:"scope"` // User or System
	Architecture string `json:"architecture"`
	Server       string `json:"server,omitempty"`
	Database     string `json:"database,omitempty"`
}

type ODBCDiagnostics struct {
	Drivers           []DriverInfo `json:"drivers"`
	DSNs              []DSNEntry   `json:"dsns"`
	PHPArch           string       `json:"php_arch"`
	ArchMismatchRisks []string     `json:"arch_mismatch_risks"`
}

func InspectODBC() ODBCDiagnostics {
	diag := ODBCDiagnostics{
		Drivers:           []DriverInfo{},
		DSNs:              []DSNEntry{},
		PHPArch:           "64-bit (x64)",
		ArchMismatchRisks: []string{},
	}

	// 1. Query 64-bit ODBC Drivers
	drivers64 := queryRegValues(`HKLM\SOFTWARE\ODBC\ODBCINST.INI\ODBC Drivers`)
	for k := range drivers64 {
		diag.Drivers = append(diag.Drivers, DriverInfo{Name: k, Architecture: "64-bit"})
	}

	// 2. Query 32-bit ODBC Drivers (WOW6432Node)
	drivers32 := queryRegValues(`HKLM\SOFTWARE\WOW6432Node\ODBC\ODBCINST.INI\ODBC Drivers`)
	for k := range drivers32 {
		diag.Drivers = append(diag.Drivers, DriverInfo{Name: k, Architecture: "32-bit"})
	}

	// 3. Query User DSNs (64-bit)
	userDSNs := queryRegValues(`HKCU\Software\ODBC\ODBC.INI\ODBC Data Sources`)
	for name, drv := range userDSNs {
		diag.DSNs = append(diag.DSNs, DSNEntry{
			Name:         name,
			Driver:       drv,
			Scope:        "User DSN",
			Architecture: "64-bit",
		})
	}

	// 4. Query System DSNs (64-bit)
	sysDSNs64 := queryRegValues(`HKLM\SOFTWARE\ODBC\ODBC.INI\ODBC Data Sources`)
	for name, drv := range sysDSNs64 {
		diag.DSNs = append(diag.DSNs, DSNEntry{
			Name:         name,
			Driver:       drv,
			Scope:        "System DSN",
			Architecture: "64-bit",
		})
	}

	// 5. Query System DSNs (32-bit)
	sysDSNs32 := queryRegValues(`HKLM\SOFTWARE\WOW6432Node\ODBC\ODBC.INI\ODBC Data Sources`)
	for name, drv := range sysDSNs32 {
		diag.DSNs = append(diag.DSNs, DSNEntry{
			Name:         name,
			Driver:       drv,
			Scope:        "System DSN",
			Architecture: "32-bit",
		})
		diag.ArchMismatchRisks = append(diag.ArchMismatchRisks,
			fmt.Sprintf("تحذير معمارية: مصدر البيانات '%s' (%s) مسجل كـ 32-bit بينما PHP يعمل بـ 64-bit. أنشئ نسخة 64-bit منه لتجنب خطأ Data source name not found.", name, drv))
	}

	return diag
}

func CreateUserDSN(name, driver, server, database string) error {
	if name == "" || driver == "" {
		return fmt.Errorf("اسم DSN والتعريف (Driver) مطلوبان")
	}

	// Register in ODBC Data Sources list
	if err := regAdd(`HKCU\Software\ODBC\ODBC.INI\ODBC Data Sources`, name, driver); err != nil {
		return err
	}

	dsnKey := fmt.Sprintf(`HKCU\Software\ODBC\ODBC.INI\%s`, name)
	if server != "" {
		_ = regAdd(dsnKey, "Server", server)
	}
	if database != "" {
		_ = regAdd(dsnKey, "Database", database)
		_ = regAdd(dsnKey, "DBQ", database) // Useful for Microsoft Access (.mdb/.accdb)
	}
	return nil
}

func DeleteUserDSN(name string) error {
	if name == "" {
		return fmt.Errorf("اسم DSN مطلوب")
	}
	_ = execHidden("reg", "delete", `HKCU\Software\ODBC\ODBC.INI\ODBC Data Sources`, "/v", name, "/f")
	_ = execHidden("reg", "delete", fmt.Sprintf(`HKCU\Software\ODBC\ODBC.INI\%s`, name), "/f")
	return nil
}

func queryRegValues(keyPath string) map[string]string {
	res := make(map[string]string)
	cmd := exec.Command("reg", "query", keyPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		return res
	}

	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "HKEY_") {
			continue
		}
		parts := strings.Split(line, "REG_SZ")
		if len(parts) == 2 {
			valName := strings.TrimSpace(parts[0])
			valData := strings.TrimSpace(parts[1])
			if valName != "" {
				res[valName] = valData
			}
		}
	}
	return res
}

func regAdd(keyPath, valueName, data string) error {
	return execHidden("reg", "add", keyPath, "/v", valueName, "/t", "REG_SZ", "/d", data, "/f")
}

func execHidden(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Run()
}

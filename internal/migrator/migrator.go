package migrator

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"axamp/internal/config"
)

type XamppScanReport struct {
	Path              string   `json:"path"`
	Valid             bool     `json:"valid"`
	Projects          []string `json:"projects"`
	Databases         []string `json:"databases"`
	EnabledExtensions []string `json:"enabled_extensions"`
	VirtualHosts      []string `json:"virtual_hosts"`
	PHPVersionHint    string   `json:"php_version_hint"`
}

type MigrationOptions struct {
	SourcePath        string   `json:"source_path"`
	CopyProjects      bool     `json:"copy_projects"`
	SelectedProjects  []string `json:"selected_projects"`
	CopyDatabases     bool     `json:"copy_databases"`
	SelectedDatabases []string `json:"selected_databases"`
	ImportExtensions  bool     `json:"import_extensions"`
}

type MigrationResult struct {
	CopiedProjects     []string `json:"copied_projects"`
	CopiedDatabases    []string `json:"copied_databases"`
	ImportedExtensions []string `json:"imported_extensions"`
	Warnings           []string `json:"warnings"`
}

var defaultXamppFolders = map[string]bool{
	"dashboard":              true,
	"img":                    true,
	"webalizer":              true,
	"xampp":                  true,
	"forbidden":              true,
	"restricted":             true,
	"bitnami.css":            true,
	"applications.html":      true,
	"favicon.ico":            true,
	"index.php":              true,
}

var systemMySQLDatabases = map[string]bool{
	"mysql":              true,
	"performance_schema": true,
	"information_schema": true,
	"sys":                true,
	"phpmyadmin":         true,
	"test":               true,
}

func ScanXamppInstallation(xamppPath string) XamppScanReport {
	report := XamppScanReport{
		Path:              xamppPath,
		Projects:          []string{},
		Databases:         []string{},
		EnabledExtensions: []string{},
		VirtualHosts:      []string{},
	}

	htdocsPath := filepath.Join(xamppPath, "htdocs")
	mysqlDataPath := filepath.Join(xamppPath, "mysql", "data")
	phpIniPath := filepath.Join(xamppPath, "php", "php.ini")
	vhostsPath := filepath.Join(xamppPath, "apache", "conf", "extra", "httpd-vhosts.conf")

	if info, err := os.Stat(htdocsPath); err == nil && info.IsDir() {
		report.Valid = true
		entries, _ := os.ReadDir(htdocsPath)
		for _, e := range entries {
			name := e.Name()
			if defaultXamppFolders[strings.ToLower(name)] {
				continue
			}
			if e.IsDir() {
				report.Projects = append(report.Projects, name)
			}
		}
	}

	if info, err := os.Stat(mysqlDataPath); err == nil && info.IsDir() {
		report.Valid = true
		entries, _ := os.ReadDir(mysqlDataPath)
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			name := e.Name()
			if systemMySQLDatabases[strings.ToLower(name)] {
				continue
			}
			report.Databases = append(report.Databases, name)
		}
	}

	if f, err := os.Open(phpIniPath); err == nil {
		defer f.Close()
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(line, "extension=") || strings.HasPrefix(line, "extension =") {
				parts := strings.SplitN(line, "=", 2)
				if len(parts) == 2 {
					ext := strings.TrimSpace(parts[1])
					ext = strings.TrimPrefix(ext, "php_")
					ext = strings.TrimSuffix(ext, ".dll")
					report.EnabledExtensions = append(report.EnabledExtensions, ext)
				}
			}
		}
	}

	if f, err := os.Open(vhostsPath); err == nil {
		defer f.Close()
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(strings.ToLower(line), "servername ") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					report.VirtualHosts = append(report.VirtualHosts, fields[1])
				}
			}
		}
	}

	return report
}

func ExecuteMigration(cfg *config.AppConfig, opts MigrationOptions) (MigrationResult, error) {
	res := MigrationResult{
		CopiedProjects:     []string{},
		CopiedDatabases:    []string{},
		ImportedExtensions: []string{},
		Warnings:           []string{},
	}

	scan := ScanXamppInstallation(opts.SourcePath)
	if !scan.Valid {
		return res, fmt.Errorf("المسار المحدد لا يحتوي على تثبيت XAMPP صالح: %s", opts.SourcePath)
	}

	// 1. Migrate htdocs projects
	if opts.CopyProjects {
		projectsToCopy := opts.SelectedProjects
		if len(projectsToCopy) == 0 {
			projectsToCopy = scan.Projects
		}
		for _, proj := range projectsToCopy {
			src := filepath.Join(opts.SourcePath, "htdocs", proj)
			dst := filepath.Join(cfg.RootDir, "htdocs", proj)
			if err := copyDirectory(src, dst); err != nil {
				res.Warnings = append(res.Warnings, fmt.Sprintf("تعذر نسخ المشروع %s: %v", proj, err))
			} else {
				res.CopiedProjects = append(res.CopiedProjects, proj)
			}
		}
	}

	// 2. Migrate MySQL database folders safely
	if opts.CopyDatabases {
		dbsToCopy := opts.SelectedDatabases
		if len(dbsToCopy) == 0 {
			dbsToCopy = scan.Databases
		}
		for _, db := range dbsToCopy {
			src := filepath.Join(opts.SourcePath, "mysql", "data", db)
			dst := filepath.Join(cfg.RootDir, "data", db)
			if err := copyDirectory(src, dst); err != nil {
				res.Warnings = append(res.Warnings, fmt.Sprintf("تعذر نسخ قاعدة البيانات %s: %v", db, err))
			} else {
				res.CopiedDatabases = append(res.CopiedDatabases, db)
			}
		}
		// Also copy ibdata1 if destination doesn't have one yet
		ibSrc := filepath.Join(opts.SourcePath, "mysql", "data", "ibdata1")
		ibDst := filepath.Join(cfg.RootDir, "data", "ibdata1")
		if _, err := os.Stat(ibDst); os.IsNotExist(err) {
			if _, err := os.Stat(ibSrc); err == nil {
				_ = copyFile(ibSrc, ibDst)
			}
		}
	}

	// 3. Import enabled PHP extensions
	if opts.ImportExtensions && len(scan.EnabledExtensions) > 0 {
		for _, ext := range scan.EnabledExtensions {
			cfg.PHPExtensions[ext] = true
			res.ImportedExtensions = append(res.ImportedExtensions, ext)
		}
		_ = cfg.Save()
	}

	return res, nil
}

func copyDirectory(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

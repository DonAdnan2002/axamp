package pkgmanager

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"axamp/internal/config"
)

type PackageStatus struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Installed   bool   `json:"installed"`
	BinaryPath  string `json:"binary_path"`
	DownloadURL string `json:"download_url"`
	StatusText  string `json:"status_text"`
	Progress    int    `json:"progress"`
}

type Manager struct {
	mu       sync.Mutex
	cfg      *config.AppConfig
	progress map[string]int
	status   map[string]string
}

func NewManager(cfg *config.AppConfig) *Manager {
	return &Manager{
		cfg:      cfg,
		progress: make(map[string]int),
		status:   make(map[string]string),
	}
}

func (m *Manager) InspectPackages() []PackageStatus {
	m.mu.Lock()
	defer m.mu.Unlock()

	pkgs := []PackageStatus{
		{
			ID:          "apache",
			Name:        "Apache HTTP Server (2.4.69 VS18 x64 - Oct 2026)",
			BinaryPath:  filepath.Join(m.cfg.RootDir, "bin", "apache", "bin", "httpd.exe"),
			DownloadURL: "https://www.apachelounge.com/download/VS18/binaries/httpd-2.4.69-261002-Win64-VS18.zip",
		},
		{
			ID:          "php",
			Name:        "PHP Runtime (8.5.12 TS VS17 x64 - Security Release Oct 2026)",
			BinaryPath:  filepath.Join(m.cfg.RootDir, "bin", "php", "php.exe"),
			DownloadURL: "https://downloads.php.net/~windows/releases/archives/php-8.5.12-Win32-vs17-x64.zip",
		},
		{
			ID:          "mysql",
			Name:        "MariaDB / MySQL Server (11.4.13 LTS x64 - Oct 2026)",
			BinaryPath:  filepath.Join(m.cfg.RootDir, "bin", "mysql", "bin", "mysqld.exe"),
			DownloadURL: "https://archive.mariadb.org/mariadb-11.4.13/winx64-packages/mariadb-11.4.13-winx64.zip",
		},
		{
			ID:          "phpmyadmin",
			Name:        "phpMyAdmin (5.2.3 Full Web Suite)",
			BinaryPath:  filepath.Join(m.cfg.RootDir, "tools", "phpmyadmin", "vendor", "webmozart", "assert", "src", "Assert.php"),
			DownloadURL: "https://files.phpmyadmin.net/phpMyAdmin/5.2.3/phpMyAdmin-5.2.3-all-languages.zip",
		},
	}

	for i := range pkgs {
		id := pkgs[i].ID
		if _, err := os.Stat(pkgs[i].BinaryPath); err == nil {
			pkgs[i].Installed = true
			pkgs[i].StatusText = "مثبت وجاهز (Offline Ready)"
			pkgs[i].Progress = 100
		} else {
			pkgs[i].Installed = false
			if st, ok := m.status[id]; ok {
				pkgs[i].StatusText = st
				pkgs[i].Progress = m.progress[id]
			} else {
				pkgs[i].StatusText = "غير مثبت (متاح عبر ZIP محلي أو استنساخ XAMPP أو تحميل مباشر)"
			}
		}
	}
	return pkgs
}

func (m *Manager) setProgress(id, text string, pct int) {
	m.mu.Lock()
	m.status[id] = text
	m.progress[id] = pct
	m.mu.Unlock()
}

// InstallFromLocalZip extracts a local offline .zip archive into the target component directory
func (m *Manager) InstallFromLocalZip(pkgID, zipPath string) error {
	if _, err := os.Stat(zipPath); err != nil {
		return fmt.Errorf("ملف الأرشيف غير موجود: %s", zipPath)
	}

	targetDir := m.resolveTargetDir(pkgID)
	m.setProgress(pkgID, "جاري فك الضغط من الملف المحلي...", 25)

	if err := extractSmartZip(zipPath, targetDir, pkgID); err != nil {
		m.setProgress(pkgID, "فشل فك الضغط: "+err.Error(), 0)
		return err
	}

	m.setProgress(pkgID, "تم التثبيت المحلي بنجاح!", 100)
	return nil
}

// CloneBinariesFromXampp copies Apache, PHP, MySQL, and phpMyAdmin binaries from an existing local XAMPP folder (100% Offline)
func (m *Manager) CloneBinariesFromXampp(xamppDir string) ([]string, error) {
	var cloned []string
	pairs := []struct {
		id  string
		src string
		dst string
	}{
		{"apache", filepath.Join(xamppDir, "apache"), filepath.Join(m.cfg.RootDir, "bin", "apache")},
		{"php", filepath.Join(xamppDir, "php"), filepath.Join(m.cfg.RootDir, "bin", "php")},
		{"mysql", filepath.Join(xamppDir, "mysql"), filepath.Join(m.cfg.RootDir, "bin", "mysql")},
		{"phpmyadmin", filepath.Join(xamppDir, "phpMyAdmin"), filepath.Join(m.cfg.RootDir, "tools", "phpmyadmin")},
	}

	for _, p := range pairs {
		if info, err := os.Stat(p.src); err == nil && info.IsDir() {
			m.setProgress(p.id, "جاري استنساخ المحرك محلياً من XAMPP...", 40)
			if err := copyDirRecursive(p.src, p.dst); err == nil {
				m.setProgress(p.id, "تم الاستنساخ المحلي بنجاح!", 100)
				cloned = append(cloned, p.id)
			}
		}
	}

	if len(cloned) == 0 {
		return nil, fmt.Errorf("لم يتم العثور على مجلدات apache/php/mysql صالحة داخل %s", xamppDir)
	}
	return cloned, nil
}

// DownloadAndInstall downloads the package from URL (or custom mirror URL) and extracts it
func (m *Manager) DownloadAndInstall(pkgID, customURL string) {
	go func() {
		url := customURL
		if url == "" {
			for _, p := range m.InspectPackages() {
				if p.ID == pkgID {
					url = p.DownloadURL
					break
				}
			}
		}
		if url == "" {
			m.setProgress(pkgID, "رابط التحميل غير محدد", 0)
			return
		}

		m.setProgress(pkgID, "جاري الاتصال بالخادم والتحميل...", 5)
		tmpZip := filepath.Join(m.cfg.RootDir, "tmp", pkgID+"_pkg.zip")
		_ = os.MkdirAll(filepath.Dir(tmpZip), 0755)

		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			m.setProgress(pkgID, "خطأ في الرابط: "+err.Error(), 0)
			return
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AXAMP-PackageManager/1.0")

		resp, err := http.DefaultClient.Do(req)
		if err != nil || resp.StatusCode >= 400 {
			errStr := "تعذر التحميل (تأكد من الاتصال أو استخدم ملف ZIP محلي)"
			if err != nil {
				errStr = err.Error()
			} else {
				errStr = fmt.Sprintf("HTTP %d", resp.StatusCode)
			}
			m.setProgress(pkgID, "فشل التحميل: "+errStr, 0)
			return
		}
		defer resp.Body.Close()

		out, err := os.Create(tmpZip)
		if err != nil {
			m.setProgress(pkgID, "خطأ كتابة الملف المؤقت", 0)
			return
		}

		total := resp.ContentLength
		var downloaded int64
		buf := make([]byte, 64*1024)
		for {
			n, readErr := resp.Body.Read(buf)
			if n > 0 {
				_, _ = out.Write(buf[:n])
				downloaded += int64(n)
				if total > 0 {
					pct := int((downloaded * 80) / total)
					m.setProgress(pkgID, fmt.Sprintf("جاري التحميل... (%d MB)", downloaded/(1024*1024)), pct)
				}
			}
			if readErr != nil {
				if readErr == io.EOF {
					break
				}
				_ = out.Close()
				m.setProgress(pkgID, "انقطع التحميل: "+readErr.Error(), 0)
				return
			}
		}
		_ = out.Close()

		m.setProgress(pkgID, "جاري فك الضغط والتركيب الذكي...", 85)
		targetDir := m.resolveTargetDir(pkgID)
		if err := extractSmartZip(tmpZip, targetDir, pkgID); err != nil {
			m.setProgress(pkgID, "فشل فك الضغط: "+err.Error(), 0)
			return
		}
		_ = os.Remove(tmpZip)
		m.setProgress(pkgID, "تم التحميل والتثبيت بنجاح!", 100)
	}()
}

func (m *Manager) resolveTargetDir(pkgID string) string {
	switch pkgID {
	case "apache":
		return filepath.Join(m.cfg.RootDir, "bin", "apache")
	case "php":
		return filepath.Join(m.cfg.RootDir, "bin", "php")
	case "mysql":
		return filepath.Join(m.cfg.RootDir, "bin", "mysql")
	case "phpmyadmin":
		return filepath.Join(m.cfg.RootDir, "tools", "phpmyadmin")
	default:
		return filepath.Join(m.cfg.RootDir, "bin", pkgID)
	}
}

func extractSmartZip(zipPath, destDir, pkgID string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	// Detect if all files share a single top-level root folder (e.g., Apache24/, mariadb-11.4.3-winx64/, phpMyAdmin-5.2.1/)
	prefixToStrip := detectCommonRootPrefix(r.File)

	for _, f := range r.File {
		relName := strings.ReplaceAll(f.Name, `\`, `/`)
		if prefixToStrip != "" && strings.HasPrefix(relName, prefixToStrip) {
			relName = strings.TrimPrefix(relName, prefixToStrip)
		}
		relName = strings.TrimPrefix(relName, "/")
		if relName == "" {
			continue
		}

		fpath := filepath.Join(destDir, filepath.FromSlash(relName))
		if f.FileInfo().IsDir() {
			_ = os.MkdirAll(fpath, 0755)
			continue
		}

		if err := os.MkdirAll(filepath.Dir(fpath), 0755); err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			return err
		}
		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			rc.Close()
			return err
		}
		_, _ = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()
	}
	return nil
}

func detectCommonRootPrefix(files []*zip.File) string {
	// If all files start with Apache24/, strip Apache24/ even if Readme.txt exists at root
	apacheCount := 0
	for _, f := range files {
		clean := strings.TrimPrefix(strings.ReplaceAll(f.Name, `\`, `/`), "/")
		if strings.HasPrefix(clean, "Apache24/") {
			apacheCount++
		}
	}
	if apacheCount > 50 {
		return "Apache24/"
	}

	var rootCandidate string
	for _, f := range files {
		clean := strings.TrimPrefix(strings.ReplaceAll(f.Name, `\`, `/`), "/")
		parts := strings.SplitN(clean, "/", 2)
		if len(parts) < 2 {
			// There is a top-level file (like in PHP zip archives), so do not strip root folder
			return ""
		}
		top := parts[0] + "/"
		if rootCandidate == "" {
			rootCandidate = top
		} else if rootCandidate != top {
			return ""
		}
	}
	return rootCandidate
}

func copyDirRecursive(src, dst string) error {
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
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.Create(target)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
}

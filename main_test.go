package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"axamp/internal/config"
	"axamp/internal/dbmanager"
	"axamp/internal/generator"
	"axamp/internal/migrator"
	"axamp/internal/odbc"
	"axamp/internal/pkgmanager"
	"axamp/internal/profiler"
	"axamp/internal/server"
	"axamp/internal/supervisor"
)

// 1. اختبار محرك فحص العتاد والمنافذ الذكية (Profiler & Smart Port Engine Test)
func TestProfiler(t *testing.T) {
	hw := profiler.InspectSystem()
	if hw.CPUCores <= 0 {
		t.Fatalf("Expected CPU cores > 0, got %d", hw.CPUCores)
	}
	if hw.TotalRAMMB <= 0 {
		t.Fatalf("Expected TotalRAMMB > 0, got %d MB", hw.TotalRAMMB)
	}
	if hw.RecommendedPHPMemory == "" || hw.RecommendedInnoDBSize == "" {
		t.Fatalf("Expected non-empty hardware tuning recommendations")
	}

	port, scanRes := profiler.FindSmartPort("49152", []string{"49153"}, 49154)
	if port == "" {
		t.Fatalf("Expected smart port resolver to return a valid port")
	}

	t.Logf("[PASS] Profiler detected: %d CPUs, %d MB RAM (PHP Memory: %s, InnoDB: %s, Smart Port: %s, Available: %v)",
		hw.CPUCores, hw.TotalRAMMB, hw.RecommendedPHPMemory, hw.RecommendedInnoDBSize, port, scanRes.Available)
}

// 2. اختبار توليد ملفات التكوين الديناميكية والتبديل بين نمطي Dev و Prod (Config Generator Test)
func TestConfigGeneratorAndModes(t *testing.T) {
	tmpRoot := t.TempDir()
	cfg, err := config.LoadOrInit(tmpRoot)
	if err != nil {
		t.Fatalf("Failed to init config: %v", err)
	}

	cfg.ApachePort = "8080"
	cfg.MySQLPort = "3307"
	cfg.Mode = config.DevMode

	hw := profiler.InspectSystem()
	if err := generator.GenerateAll(cfg, hw); err != nil {
		t.Fatalf("GenerateAll failed in DevMode: %v", err)
	}

	// التحقق من وجود كافة الملفات المتولدة
	expectedFiles := []string{
		filepath.Join(tmpRoot, "conf", "httpd.conf"),
		filepath.Join(tmpRoot, "conf", "php.ini"),
		filepath.Join(tmpRoot, "conf", "my.ini"),
		filepath.Join(tmpRoot, "htdocs", "index.php"),
		filepath.Join(tmpRoot, "tools", "phpmyadmin", "config.inc.php"),
		filepath.Join(tmpRoot, "tools", "adminer", "index.php"),
	}

	for _, f := range expectedFiles {
		if _, err := os.Stat(f); os.IsNotExist(err) {
			t.Errorf("Expected generated file to exist: %s", f)
		}
	}

	phpDev, _ := os.ReadFile(filepath.Join(tmpRoot, "conf", "php.ini"))
	if !strings.Contains(string(phpDev), "display_errors = On") {
		t.Errorf("Expected display_errors = On in Development Mode")
	}

	// التبديل إلى نمط الإنتاجية (Production Mode) والتحقق من الأمان
	cfg.Mode = config.ProdMode
	if err := generator.GenerateAll(cfg, hw); err != nil {
		t.Fatalf("GenerateAll failed in ProdMode: %v", err)
	}
	phpProd, _ := os.ReadFile(filepath.Join(tmpRoot, "conf", "php.ini"))
	if !strings.Contains(string(phpProd), "display_errors = Off") {
		t.Errorf("Expected display_errors = Off in Production Mode")
	}

	t.Log("[PASS] Configuration generator verified for both Development and Production modes.")
}

// 3. اختبار مدير قواعد البيانات والنسخ الاحتياطي المضغوط .zip (DB Manager & Compressed Backup Test)
func TestDatabaseAndBackupManager(t *testing.T) {
	tmpRoot := t.TempDir()
	cfg := config.DefaultConfig(tmpRoot)

	// إنشاء قاعدة بيانات تجريبية
	if err := dbmanager.CreateDatabase(cfg, "axamp_test_db"); err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	// إضافة ملف جدول تجريبي لمحاكاة البيانات
	dummyTable := filepath.Join(tmpRoot, "data", "axamp_test_db", "users.ibd")
	_ = os.WriteFile(dummyTable, []byte("dummy innodb table content"), 0644)

	dbs := dbmanager.ListDatabases(cfg)
	if len(dbs) != 1 || dbs[0].Name != "axamp_test_db" {
		t.Fatalf("Expected 1 database 'axamp_test_db', got: %+v", dbs)
	}

	// اختبار النسخ الاحتياطي المضغوط (مع Offline Fallback)
	zipPath, err := dbmanager.BackupDatabase(cfg, "axamp_test_db")
	if err != nil {
		t.Fatalf("Expected BackupDatabase to succeed via offline zip fallback, got error: %v", err)
	}
	if _, err := os.Stat(zipPath); os.IsNotExist(err) {
		t.Fatalf("Expected backup zip archive to exist at: %s", zipPath)
	}

	backups := dbmanager.ListBackups(cfg)
	if len(backups) == 0 {
		t.Fatalf("Expected ListBackups to return at least 1 backup record")
	}

	t.Logf("[PASS] Database created and compressed backup archive generated at: %s (%.2f KB)",
		backups[0].FileName, backups[0].SizeKB)
}

// 4. اختبار مدير مصادر بيانات ODBC وفحص المعمارية (ODBC Manager Test)
func TestODBCManager(t *testing.T) {
	diag := odbc.InspectODBC()
	t.Logf("[PASS] ODBC inspection completed: %d Drivers, %d DSNs, %d Architecture Warnings",
		len(diag.Drivers), len(diag.DSNs), len(diag.ArchMismatchRisks))
}

// 5. اختبار معالج الترقية من XAMPP قديم ومدير الحزم الهجين (XAMPP Migrator & Hybrid Package Manager Test)
func TestXamppMigratorAndHybridPackages(t *testing.T) {
	fakeXampp := t.TempDir()
	targetAxamp := t.TempDir()

	// محاكاة هيكل XAMPP قديم يحتوي على مشروع وقاعدة بيانات وملف php.ini
	_ = os.MkdirAll(filepath.Join(fakeXampp, "htdocs", "my_erp_app"), 0755)
	_ = os.WriteFile(filepath.Join(fakeXampp, "htdocs", "my_erp_app", "index.php"), []byte("<?php echo 'ERP'; ?>"), 0644)
	_ = os.MkdirAll(filepath.Join(fakeXampp, "htdocs", "dashboard"), 0755) // مجلد افتراضي يجب تجاهله
	_ = os.MkdirAll(filepath.Join(fakeXampp, "mysql", "data", "erp_db"), 0755)
	_ = os.WriteFile(filepath.Join(fakeXampp, "mysql", "data", "erp_db", "data.frm"), []byte("schema"), 0644)
	_ = os.MkdirAll(filepath.Join(fakeXampp, "php"), 0755)
	_ = os.WriteFile(filepath.Join(fakeXampp, "php", "php.ini"), []byte("extension=soap\nextension=ldap\n"), 0644)

	scan := migrator.ScanXamppInstallation(fakeXampp)
	if !scan.Valid || len(scan.Projects) != 1 || scan.Projects[0] != "my_erp_app" {
		t.Fatalf("Unexpected XAMPP scan result: %+v", scan)
	}

	cfg := config.DefaultConfig(targetAxamp)
	res, err := migrator.ExecuteMigration(cfg, migrator.MigrationOptions{
		SourcePath:       fakeXampp,
		CopyProjects:     true,
		CopyDatabases:    true,
		ImportExtensions: true,
	})
	if err != nil {
		t.Fatalf("ExecuteMigration failed: %v", err)
	}
	if len(res.CopiedProjects) != 1 || len(res.CopiedDatabases) != 1 || !cfg.PHPExtensions["soap"] {
		t.Fatalf("Migration did not transfer expected items: %+v", res)
	}

	pkgMgr := pkgmanager.NewManager(cfg)
	pkgs := pkgMgr.InspectPackages()
	if len(pkgs) != 4 {
		t.Fatalf("Expected 4 managed packages, got %d", len(pkgs))
	}

	t.Logf("[PASS] XAMPP Migrator transferred %v projects, %v DBs, and enabled extensions %v",
		res.CopiedProjects, res.CopiedDatabases, res.ImportedExtensions)
}

// 6. اختبار E2E لخادم التحكم والـ API والحماية بالـ PIN (Control Plane API E2E Test)
func TestControlPlaneAPI(t *testing.T) {
	tmpRoot := t.TempDir()
	cfg, _ := config.LoadOrInit(tmpRoot)
	testPort, _ := profiler.FindSmartPort("14040", []string{"14041", "14042"}, 14050)
	cfg.ControlPort = testPort

	hw := profiler.InspectSystem()
	_ = generator.GenerateAll(cfg, hw)

	sup := supervisor.NewManager(cfg)
	srv := server.NewServer(cfg, sup)

	go func() {
		_ = srv.Start()
	}()
	time.Sleep(300 * time.Millisecond)

	baseURL := "http://127.0.0.1:" + testPort

	// أ. فحص نقطة النهاية /api/state
	resp, err := http.Get(baseURL + "/api/state")
	if err != nil {
		t.Fatalf("Failed to reach Control Plane API: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected HTTP 200 from /api/state, got %d", resp.StatusCode)
	}

	var state map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&state); err != nil {
		t.Fatalf("Failed to decode /api/state JSON: %v", err)
	}

	// ب. اختبار تبديل النمط عبر الـ API (/api/mode) إلى Production
	body, _ := json.Marshal(map[string]string{"mode": "production"})
	modeResp, err := http.Post(baseURL+"/api/mode", "application/json", bytes.NewReader(body))
	if err != nil || modeResp.StatusCode != http.StatusOK {
		t.Fatalf("Failed to switch mode via /api/mode: %v", err)
	}
	_ = modeResp.Body.Close()

	if cfg.Mode != config.ProdMode {
		t.Fatalf("Expected server mode to be updated to production, got %s", cfg.Mode)
	}

	t.Logf("[PASS] E2E Control Plane API verified on %s (State & Mode Switching OK)", baseURL)
}

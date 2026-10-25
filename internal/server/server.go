package server

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"axamp/internal/config"
	"axamp/internal/dbmanager"
	"axamp/internal/generator"
	"axamp/internal/migrator"
	"axamp/internal/odbc"
	"axamp/internal/phptransform"
	"axamp/internal/pkgmanager"
	"axamp/internal/profiler"
	"axamp/internal/secguard"
	"axamp/internal/supervisor"
	"axamp/internal/upgrader"
	"axamp/internal/winutil"
)

//go:embed ui/*
var uiFS embed.FS

type Server struct {
	cfg *config.AppConfig
	sup *supervisor.Manager
	pkg *pkgmanager.Manager
}

func NewServer(cfg *config.AppConfig, sup *supervisor.Manager) *Server {
	return &Server{
		cfg: cfg,
		sup: sup,
		pkg: pkgmanager.NewManager(cfg),
	}
}

func (s *Server) Start() error {
	mux := http.NewServeMux()

	// API endpoints with Remote PIN protection for non-localhost clients
	mux.HandleFunc("/api/state", s.authMiddleware(s.handleState))
	mux.HandleFunc("/api/service", s.authMiddleware(s.handleServiceAction))
	mux.HandleFunc("/api/winservice", s.authMiddleware(s.handleWinService))
	mux.HandleFunc("/api/packages", s.authMiddleware(s.handlePackages))
	mux.HandleFunc("/api/mode", s.authMiddleware(s.handleModeSwitch))
	mux.HandleFunc("/api/php/extensions", s.authMiddleware(s.handlePHPExtension))
	mux.HandleFunc("/api/odbc", s.authMiddleware(s.handleODBC))
	mux.HandleFunc("/api/migrate/scan", s.authMiddleware(s.handleMigrateScan))
	mux.HandleFunc("/api/migrate/execute", s.authMiddleware(s.handleMigrateExecute))
	mux.HandleFunc("/api/databases", s.authMiddleware(s.handleDatabases))
	mux.HandleFunc("/api/vhosts", s.authMiddleware(s.handleVHosts))
	mux.HandleFunc("/api/logs", s.authMiddleware(s.handleLogs))
	mux.HandleFunc("/api/security", s.authMiddleware(s.handleSecurity))
	mux.HandleFunc("/api/live-stats", s.authMiddleware(s.handleLiveStats))
	mux.HandleFunc("/api/upgrader", s.authMiddleware(s.handleUpgrader))
	mux.HandleFunc("/api/settings", s.authMiddleware(s.handleSettings))
	mux.HandleFunc("/api/quick-open", s.authMiddleware(s.handleQuickOpen))
	mux.HandleFunc("/api/shutdown", s.authMiddleware(s.handleShutdown))

	subUI, _ := fs.Sub(uiFS, "ui")
	mux.Handle("/", http.FileServer(http.FS(subUI)))

	addr := "0.0.0.0:" + s.cfg.ControlPort
	return http.ListenAndServe(addr, mux)
}

func isLocalRequest(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return host == "127.0.0.1" || host == "::1" || host == "localhost"
}

func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !isLocalRequest(r) {
			if !s.cfg.RemoteAccess {
				http.Error(w, `{"error":"الوصول البعيد غير مفعل في إعدادات اللانشر"}`, http.StatusForbidden)
				return
			}
			pin := r.Header.Get("X-AXAMP-PIN")
			if pin == "" {
				pin = r.URL.Query().Get("pin")
			}
			if pin != s.cfg.RemotePIN {
				http.Error(w, `{"error":"رمز الوصول البعيد (PIN) غير صحيح"}`, http.StatusUnauthorized)
				return
			}
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		next(w, r)
	}
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	hw := profiler.InspectSystem()
	projects := listHtdocsProjects(s.cfg.RootDir)
	phpVersions := listPHPVersions(s.cfg.RootDir)
	live := profiler.GetLiveMetrics(s.cfg.ApachePort, s.cfg.MySQLPort, s.cfg.ControlPort)

	payload := map[string]interface{}{
		"is_local":     isLocalRequest(r),
		"config":       s.cfg,
		"hardware":     hw,
		"live_metrics": live,
		"services":     s.sup.Status(),
		"packages":     s.pkg.InspectPackages(),
		"events":       s.sup.GetEvents(),
		"projects":     projects,
		"php_versions": phpVersions,
		"databases":    dbmanager.ListDatabases(s.cfg),
		"backups":      dbmanager.ListBackups(s.cfg),
		"security":     secguard.AuditStack(s.cfg),
	}
	_ = json.NewEncoder(w).Encode(payload)
}

func (s *Server) handleLiveStats(w http.ResponseWriter, r *http.Request) {
	live := profiler.GetLiveMetrics(s.cfg.ApachePort, s.cfg.MySQLPort, s.cfg.ControlPort)
	_ = json.NewEncoder(w).Encode(live)
}

func (s *Server) handleServiceAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Target string `json:"target"` // apache, mysql, all
		Action string `json:"action"` // start, stop, restart
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	var err error
	switch req.Target {
	case "apache":
		if req.Action == "start" {
			err = s.sup.StartApache()
		} else if req.Action == "stop" {
			err = s.sup.StopApache()
		} else {
			_ = s.sup.StopApache()
			err = s.sup.StartApache()
		}
	case "mysql":
		if req.Action == "start" {
			err = s.sup.StartMySQL()
		} else if req.Action == "stop" {
			err = s.sup.StopMySQL()
		} else {
			_ = s.sup.StopMySQL()
			err = s.sup.StartMySQL()
		}
	case "all":
		if req.Action == "start" {
			s.sup.StartAll()
		} else if req.Action == "stop" {
			s.sup.StopAll()
		} else {
			s.sup.RestartAll()
		}
	}

	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":       err == nil,
		"error":    errMsg,
		"services": s.sup.Status(),
	})
}

func (s *Server) handleWinService(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		return
	}
	var req struct {
		Target string `json:"target"` // apache, mysql
		Action string `json:"action"` // install, remove, start, stop
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	err := s.sup.ManageWindowsService(req.Target, req.Action)
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":       err == nil,
		"error":    errMsg,
		"services": s.sup.Status(),
	})
}

func (s *Server) handlePackages(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		_ = json.NewEncoder(w).Encode(s.pkg.InspectPackages())
		return
	}
	if r.Method == http.MethodPost {
		var req struct {
			Action    string `json:"action"` // download, local_zip, clone_xampp
			PackageID string `json:"package_id"`
			ZipPath   string `json:"zip_path"`
			CustomURL string `json:"custom_url"`
			XamppPath string `json:"xampp_path"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)

		var err error
		var cloned []string
		switch req.Action {
		case "download":
			s.pkg.DownloadAndInstall(req.PackageID, req.CustomURL)
		case "local_zip":
			err = s.pkg.InstallFromLocalZip(req.PackageID, req.ZipPath)
		case "clone_xampp":
			cloned, err = s.pkg.CloneBinariesFromXampp(req.XamppPath)
		}

		errMsg := ""
		if err != nil {
			errMsg = err.Error()
		} else {
			hw := profiler.InspectSystem()
			_ = generator.GenerateAll(s.cfg, hw)
		}

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":       err == nil,
			"error":    errMsg,
			"cloned":   cloned,
			"packages": s.pkg.InspectPackages(),
		})
	}
}

func (s *Server) handleModeSwitch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		return
	}
	var req struct {
		Mode       string `json:"mode"`
		PHPVersion string `json:"php_version"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	if req.Mode == string(config.ProdMode) {
		s.cfg.Mode = config.ProdMode
	} else if req.Mode == string(config.DevMode) {
		s.cfg.Mode = config.DevMode
	}
	if req.PHPVersion != "" {
		s.cfg.ActivePHPVersion = req.PHPVersion
	}

	_ = s.cfg.Save()
	hw := profiler.InspectSystem()
	_ = generator.GenerateAll(s.cfg, hw)

	// Hot-reload Apache if running
	st := s.sup.Status()
	if st["apache"].State == supervisor.StateRunning {
		_ = s.sup.StopApache()
		_ = s.sup.StartApache()
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":     true,
		"config": s.cfg,
	})
}

func (s *Server) handlePHPExtension(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		return
	}
	var req struct {
		Extension string `json:"extension"`
		Enabled   bool   `json:"enabled"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Extension != "" {
		s.cfg.PHPExtensions[req.Extension] = req.Enabled
		_ = s.cfg.Save()
		hw := profiler.InspectSystem()
		_ = generator.GeneratePhpIni(s.cfg, hw)
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":         true,
		"extensions": s.cfg.PHPExtensions,
	})
}

func (s *Server) handleODBC(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		_ = json.NewEncoder(w).Encode(odbc.InspectODBC())
		return
	}
	if r.Method == http.MethodPost {
		var req struct {
			Action   string `json:"action"` // create, delete
			Name     string `json:"name"`
			Driver   string `json:"driver"`
			Server   string `json:"server"`
			Database string `json:"database"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		var err error
		if req.Action == "delete" {
			err = odbc.DeleteUserDSN(req.Name)
		} else {
			err = odbc.CreateUserDSN(req.Name, req.Driver, req.Server, req.Database)
		}
		errMsg := ""
		if err != nil {
			errMsg = err.Error()
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":    err == nil,
			"error": errMsg,
			"odbc":  odbc.InspectODBC(),
		})
	}
}

func (s *Server) handleMigrateScan(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		path = `C:\xampp`
	}
	report := migrator.ScanXamppInstallation(path)
	_ = json.NewEncoder(w).Encode(report)
}

func (s *Server) handleMigrateExecute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		return
	}
	var opts migrator.MigrationOptions
	_ = json.NewDecoder(r.Body).Decode(&opts)
	res, err := migrator.ExecuteMigration(s.cfg, opts)
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	} else {
		hw := profiler.InspectSystem()
		_ = generator.GenerateAll(s.cfg, hw)
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":     err == nil,
		"error":  errMsg,
		"result": res,
	})
}

func (s *Server) handleDatabases(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		_ = json.NewEncoder(w).Encode(dbmanager.ListDatabases(s.cfg))
		return
	}
	if r.Method == http.MethodPost {
		var req struct {
			Action string `json:"action"` // create, backup
			Name   string `json:"name"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Action == "create" {
			err := dbmanager.CreateDatabase(s.cfg, req.Name)
			errMsg := ""
			if err != nil {
				errMsg = err.Error()
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"ok":        err == nil,
				"error":     errMsg,
				"databases": dbmanager.ListDatabases(s.cfg),
			})
			return
		}
		if req.Action == "backup" {
			path, err := dbmanager.BackupDatabase(s.cfg, req.Name)
			errMsg := ""
			if err != nil {
				errMsg = err.Error()
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"ok":          err == nil,
				"error":       errMsg,
				"backup_path": path,
				"backups":     dbmanager.ListBackups(s.cfg),
			})
			return
		}
		if req.Action == "backup_all" {
			files, err := dbmanager.BackupAllDatabases(s.cfg)
			errMsg := ""
			if err != nil {
				errMsg = err.Error()
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"ok":      err == nil,
				"error":   errMsg,
				"files":   files,
				"backups": dbmanager.ListBackups(s.cfg),
			})
			return
		}
	}
}

func (s *Server) handleVHosts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		return
	}
	var req struct {
		Action       string `json:"action"` // add, remove
		Domain       string `json:"domain"`
		DocumentRoot string `json:"document_root"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Action == "add" && req.Domain != "" {
		s.cfg.VirtualHosts = append(s.cfg.VirtualHosts, config.VirtualHost{
			Domain:       req.Domain,
			DocumentRoot: req.DocumentRoot,
		})
		_ = winutil.SyncLocalHostsDomain(req.Domain, true)
	} else if req.Action == "remove" {
		var filtered []config.VirtualHost
		for _, v := range s.cfg.VirtualHosts {
			if v.Domain != req.Domain {
				filtered = append(filtered, v)
			}
		}
		s.cfg.VirtualHosts = filtered
		_ = winutil.SyncLocalHostsDomain(req.Domain, false)
	}
	_ = s.cfg.Save()
	hw := profiler.InspectSystem()
	_ = generator.GenerateHttpdConf(s.cfg, hw)

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":            true,
		"virtual_hosts": s.cfg.VirtualHosts,
	})
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	logType := r.URL.Query().Get("type")
	filename := "apache_error.log"
	switch logType {
	case "apache_access":
		filename = "apache_access.log"
	case "php_error":
		filename = "php_error.log"
	case "mysql_error":
		filename = "mysql_error.log"
	}
	fullPath := filepath.Join(s.cfg.RootDir, "logs", filename)
	if r.Method == http.MethodDelete {
		_ = os.WriteFile(fullPath, []byte(""), 0644)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
		return
	}

	data, err := os.ReadFile(fullPath)
	content := ""
	if err == nil {
		lines := strings.Split(string(data), "\n")
		if len(lines) > 120 {
			lines = lines[len(lines)-120:]
		}
		content = strings.Join(lines, "\n")
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"file":    filename,
		"content": content,
		"events":  s.sup.GetEvents(),
	})
}

func (s *Server) handleSecurity(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		_ = json.NewEncoder(w).Encode(secguard.AuditStack(s.cfg))
		return
	}
	if r.Method == http.MethodPost {
		var req struct {
			Action string `json:"action"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		err := secguard.ApplyAutoFix(s.cfg, req.Action)
		errMsg := ""
		if err != nil {
			errMsg = err.Error()
		} else {
			st := s.sup.Status()
			if st["apache"].State == supervisor.StateRunning {
				_ = s.sup.StopApache()
				_ = s.sup.StartApache()
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":       err == nil,
			"error":    errMsg,
			"security": secguard.AuditStack(s.cfg),
		})
	}
}

func (s *Server) handleUpgrader(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		return
	}
	var req struct {
		Action        string `json:"action"` // snippet, folder, launch_studio
		Code          string `json:"code"`
		Folder        string `json:"folder"`
		TargetVersion string `json:"target_version"`
		DryRun        bool   `json:"dry_run"`
		Backup        bool   `json:"backup"`
		StrictTypes   bool   `json:"strict_types"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	if req.Action == "launch_studio" {
		err := upgrader.LaunchStandaloneStudio()
		errMsg := ""
		if err != nil {
			errMsg = err.Error()
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":    err == nil,
			"error": errMsg,
		})
		return
	}

	opts := phptransform.Options{
		TargetVersion:          req.TargetVersion,
		AddStrictTypes:         req.StrictTypes,
		ArrayShortSyntax:       true,
		ListShortSyntax:        true,
		VarToPublic:            true,
		FixCurlyOffsets:        true,
		FixPhp4Constructors:    true,
		ConstructorPromotion:   false,
		FixDeprecatedFunctions: true,
		AllowDynamicProperties: true,
	}

	if req.Action == "snippet" {
		out := upgrader.UpgradeSnippet(s.cfg, req.Code, opts)
		_ = json.NewEncoder(w).Encode(out)
		return
	}

	batch := upgrader.UpgradeProjectFolder(s.cfg, req.Folder, opts, req.DryRun, req.Backup)
	_ = json.NewEncoder(w).Encode(batch)
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		return
	}
	var req struct {
		QuickPortOnly     bool   `json:"quick_port_only"`
		ApachePort        string `json:"apache_port"`
		MySQLPort         string `json:"mysql_port"`
		AutoStartWindows  bool   `json:"auto_start_windows"`
		AutoStartServices bool   `json:"auto_start_services"`
		AutoHeal          bool   `json:"auto_heal"`
		RemoteAccess      bool   `json:"remote_access"`
		RegeneratePIN     bool   `json:"regenerate_pin"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	var actionErr error
	if req.RegeneratePIN {
		s.cfg.RegeneratePIN()
	} else if req.QuickPortOnly {
		restartApache := false
		restartMySQL := false
		if req.ApachePort != "" {
			s.cfg.ApachePort = strings.TrimSpace(req.ApachePort)
			restartApache = true
		}
		if req.MySQLPort != "" {
			s.cfg.MySQLPort = strings.TrimSpace(req.MySQLPort)
			restartMySQL = true
		}
		_ = s.cfg.Save()
		hw := profiler.InspectSystem()
		_ = generator.GenerateAll(s.cfg, hw)
		if restartApache {
			_ = s.sup.StopApache()
			actionErr = s.sup.StartApache()
		}
		if restartMySQL {
			_ = s.sup.StopMySQL()
			if err := s.sup.StartMySQL(); err != nil && actionErr == nil {
				actionErr = err
			}
		}
	} else {
		if req.ApachePort != "" {
			s.cfg.ApachePort = strings.TrimSpace(req.ApachePort)
		}
		if req.MySQLPort != "" {
			s.cfg.MySQLPort = strings.TrimSpace(req.MySQLPort)
		}
		s.cfg.AutoStartWindows = req.AutoStartWindows
		s.cfg.AutoStartServices = req.AutoStartServices
		s.cfg.AutoHeal = req.AutoHeal
		s.cfg.RemoteAccess = req.RemoteAccess
		_ = winutil.SetWindowsAutoStart(req.AutoStartWindows)
		_ = s.cfg.Save()
		hw := profiler.InspectSystem()
		_ = generator.GenerateAll(s.cfg, hw)

		if req.ApachePort != "" {
			_ = s.sup.StopApache()
			actionErr = s.sup.StartApache()
		}
		if req.MySQLPort != "" {
			_ = s.sup.StopMySQL()
			if err := s.sup.StartMySQL(); err != nil && actionErr == nil {
				actionErr = err
			}
		}
	}

	errMsg := ""
	if actionErr != nil {
		errMsg = actionErr.Error()
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":       actionErr == nil,
		"error":    errMsg,
		"config":   s.cfg,
		"services": s.sup.Status(),
	})
}

func (s *Server) handleQuickOpen(w http.ResponseWriter, r *http.Request) {
	if !isLocalRequest(r) {
		http.Error(w, `{"error":"متاح فقط من الجهاز المحلي"}`, http.StatusForbidden)
		return
	}
	target := r.URL.Query().Get("target")
	switch target {
	case "htdocs":
		_ = winutil.OpenFolder(filepath.Join(s.cfg.RootDir, "htdocs"))
	case "conf":
		_ = winutil.OpenFolder(filepath.Join(s.cfg.RootDir, "conf"))
	case "logs":
		_ = winutil.OpenFolder(filepath.Join(s.cfg.RootDir, "logs"))
	case "backups":
		_ = os.MkdirAll(filepath.Join(s.cfg.RootDir, "backups"), 0755)
		_ = winutil.OpenFolder(filepath.Join(s.cfg.RootDir, "backups"))
	case "localhost":
		_ = winutil.OpenBrowser("http://localhost:" + s.cfg.ApachePort)
	case "phpmyadmin":
		_ = winutil.OpenBrowser("http://localhost:" + s.cfg.ApachePort + "/phpmyadmin")
	case "adminer":
		_ = winutil.OpenBrowser("http://localhost:" + s.cfg.ApachePort + "/adminer")
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
}

func listHtdocsProjects(rootDir string) []string {
	entries, err := os.ReadDir(filepath.Join(rootDir, "htdocs"))
	if err != nil {
		return []string{}
	}
	var list []string
	for _, e := range entries {
		if e.IsDir() {
			list = append(list, e.Name())
		}
	}
	return list
}

func listPHPVersions(rootDir string) []string {
	versions := []string{"default"}
	entries, err := os.ReadDir(filepath.Join(rootDir, "bin", "php"))
	if err != nil {
		return versions
	}
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(strings.ToLower(e.Name()), "php") {
			versions = append(versions, e.Name())
		}
	}
	return versions
}

func (s *Server) handleShutdown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":      true,
		"message": "Shutting down AXAMP and stopping all managed services...",
	})

	go func() {
		time.Sleep(250 * time.Millisecond)
		s.sup.StopAll()
		os.Exit(0)
	}()
}


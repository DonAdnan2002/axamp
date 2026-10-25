package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

type StackMode string

const (
	DevMode  StackMode = "development"
	ProdMode StackMode = "production"
)

type AppConfig struct {
	mu                      sync.RWMutex    `json:"-"`
	Version                 string          `json:"version"`
	CreatorName             string          `json:"creator_name"`
	CreatorEmail            string          `json:"creator_email"`
	RootDir                 string          `json:"root_dir"`
	Mode                   StackMode       `json:"mode"`
	ApachePort             string          `json:"apache_port"`
	ApacheSSLPort          string          `json:"apache_ssl_port"`
	MySQLPort              string          `json:"mysql_port"`
	ControlPort            string          `json:"control_port"`
	ActivePHPVersion       string          `json:"active_php_version"`
	AutoStartWindows       bool            `json:"auto_start_windows"`
	AutoStartServices      bool            `json:"auto_start_services"`
	AutoHeal               bool            `json:"auto_heal"`
	RemoteAccess           bool            `json:"remote_access"`
	RemotePIN              string          `json:"remote_pin"`
	AutoBackupEnabled      bool            `json:"auto_backup_enabled"`
	AutoBackupIntervalHours int            `json:"auto_backup_interval_hours"`
	AutoBackupKeepCount    int             `json:"auto_backup_keep_count"`
	PHPExtensions          map[string]bool `json:"php_extensions"`
	VirtualHosts           []VirtualHost   `json:"virtual_hosts"`
	MaxLogSizeMB           int             `json:"max_log_size_mb"`
}

type VirtualHost struct {
	Domain       string `json:"domain"`
	DocumentRoot string `json:"document_root"`
	EnableSSL    bool   `json:"enable_ssl"`
}

func generatePIN() string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func DefaultConfig(rootDir string) *AppConfig {
	return &AppConfig{
		Version:                 "2.0.0-stable",
		CreatorName:             "عدنان سلطان حسن (Adnan Sultan Hassan)",
		CreatorEmail:            "donadnan2002@gmail.com",
		RootDir:                 rootDir,
		Mode:                    DevMode,
		ApachePort:              "80",
		ApacheSSLPort:           "443",
		MySQLPort:               "3306",
		ControlPort:             "4040",
		ActivePHPVersion:        "default",
		AutoStartWindows:        false,
		AutoStartServices:       false,
		AutoHeal:                true,
		RemoteAccess:            false,
		RemotePIN:               generatePIN(),
		AutoBackupEnabled:       true,
		AutoBackupIntervalHours: 24,
		AutoBackupKeepCount:     10,
		MaxLogSizeMB:            50,
		PHPExtensions: map[string]bool{
			"curl":       true,
			"fileinfo":   true,
			"gd":         true,
			"intl":       true,
			"mbstring":   true,
			"mysqli":     true,
			"odbc":       true,
			"openssl":    true,
			"pdo_mysql":  true,
			"pdo_odbc":   true,
			"pdo_sqlite": true,
			"sqlite3":    true,
			"zip":        true,
		},
		VirtualHosts: []VirtualHost{},
	}
}

func LoadOrInit(rootDir string) (*AppConfig, error) {
	confDir := filepath.Join(rootDir, "conf")
	_ = os.MkdirAll(confDir, 0755)
	cfgPath := filepath.Join(confDir, "axamp.json")

	cfg := DefaultConfig(rootDir)
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		if os.IsNotExist(err) {
			_ = cfg.Save()
			return cfg, nil
		}
		return cfg, err
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return cfg, err
	}
	cfg.RootDir = rootDir
	cfg.Version = "1.0.0-stable"
	cfg.CreatorName = "عدنان سلطان حسن (Adnan Sultan Hassan)"
	cfg.CreatorEmail = "donadnan2002@gmail.com"
	if cfg.RemotePIN == "" {
		cfg.RemotePIN = generatePIN()
	}
	if cfg.AutoBackupIntervalHours <= 0 {
		cfg.AutoBackupIntervalHours = 24
	}
	if cfg.AutoBackupKeepCount <= 0 {
		cfg.AutoBackupKeepCount = 10
	}
	if cfg.PHPExtensions == nil {
		cfg.PHPExtensions = DefaultConfig(rootDir).PHPExtensions
	}
	return cfg, nil
}

func (c *AppConfig) Save() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	confDir := filepath.Join(c.RootDir, "conf")
	_ = os.MkdirAll(confDir, 0755)
	cfgPath := filepath.Join(confDir, "axamp.json")

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(cfgPath, data, 0644)
}

func (c *AppConfig) RegeneratePIN() string {
	c.mu.Lock()
	c.RemotePIN = generatePIN()
	pin := c.RemotePIN
	c.mu.Unlock()
	_ = c.Save()
	return pin
}

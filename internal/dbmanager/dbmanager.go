package dbmanager

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"axamp/internal/config"
)

type DatabaseInfo struct {
	Name      string  `json:"name"`
	SizeMB    float64 `json:"size_mb"`
	FileCount int     `json:"file_count"`
}

type BackupRecord struct {
	FileName  string  `json:"file_name"`
	FullPath  string  `json:"full_path"`
	SizeKB    float64 `json:"size_kb"`
	CreatedAt string  `json:"created_at"`
}

var systemDBSkip = map[string]bool{
	"information_schema": true,
	"performance_schema": true,
	"mysql":              true,
	"sys":                true,
}

func ListDatabases(cfg *config.AppConfig) []DatabaseInfo {
	dataDir := filepath.Join(cfg.RootDir, "data")
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return []DatabaseInfo{}
	}

	var list []DatabaseInfo
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dbPath := filepath.Join(dataDir, e.Name())
		var totalBytes int64
		var count int
		_ = filepath.Walk(dbPath, func(_ string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() {
				totalBytes += info.Size()
				count++
			}
			return nil
		})
		list = append(list, DatabaseInfo{
			Name:      e.Name(),
			SizeMB:    float64(totalBytes) / (1024.0 * 1024.0),
			FileCount: count,
		})
	}
	return list
}

func ListBackups(cfg *config.AppConfig) []BackupRecord {
	backupDir := filepath.Join(cfg.RootDir, "backups")
	_ = os.MkdirAll(backupDir, 0755)
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return []BackupRecord{}
	}

	var records []BackupRecord
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		records = append(records, BackupRecord{
			FileName:  e.Name(),
			FullPath:  filepath.Join(backupDir, e.Name()),
			SizeKB:    float64(info.Size()) / 1024.0,
			CreatedAt: info.ModTime().Format("2006-01-02 15:04:05"),
		})
	}

	sort.Slice(records, func(i, j int) bool {
		return records[i].CreatedAt > records[j].CreatedAt
	})
	return records
}

func CreateDatabase(cfg *config.AppConfig, name string) error {
	if name == "" {
		return fmt.Errorf("اسم قاعدة البيانات مطلوب")
	}
	mysqlClient := filepath.Join(cfg.RootDir, "bin", "mysql", "bin", "mysql.exe")
	if _, err := os.Stat(mysqlClient); err == nil {
		cmd := exec.Command(mysqlClient, "-h", "127.0.0.1", "-P", cfg.MySQLPort, "-u", "root", "-e",
			fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;", name))
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		if err := cmd.Run(); err == nil {
			return nil
		}
	}
	dbDir := filepath.Join(cfg.RootDir, "data", name)
	return os.MkdirAll(dbDir, 0755)
}

// BackupDatabase creates a compressed .zip backup (either via mysqldump .sql.zip or raw data folder archive if offline)
func BackupDatabase(cfg *config.AppConfig, dbName string) (string, error) {
	backupDir := filepath.Join(cfg.RootDir, "backups")
	_ = os.MkdirAll(backupDir, 0755)
	ts := time.Now().Format("20060102_150405")

	dumpBin := filepath.Join(cfg.RootDir, "bin", "mysql", "bin", "mysqldump.exe")
	sqlFile := filepath.Join(backupDir, fmt.Sprintf("%s_%s.sql", dbName, ts))
	zipFile := sqlFile + ".zip"

	if _, err := os.Stat(dumpBin); err == nil {
		cmd := exec.Command(dumpBin, "-h", "127.0.0.1", "-P", cfg.MySQLPort, "-u", "root", dbName, "--result-file="+sqlFile)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		if err := cmd.Run(); err == nil {
			if err := compressSingleFileToZip(sqlFile, zipFile); err == nil {
				_ = os.Remove(sqlFile)
				pruneOldBackups(cfg)
				return zipFile, nil
			}
			return sqlFile, nil
		}
	}

	// Fallback: Offline direct directory ZIP backup from data/<dbName>
	srcDir := filepath.Join(cfg.RootDir, "data", dbName)
	if _, err := os.Stat(srcDir); err != nil {
		return "", fmt.Errorf("قاعدة البيانات غير موجودة: %s", dbName)
	}
	offlineZip := filepath.Join(backupDir, fmt.Sprintf("%s_%s_raw.zip", dbName, ts))
	if err := compressDirectoryToZip(srcDir, offlineZip); err != nil {
		return "", err
	}
	pruneOldBackups(cfg)
	return offlineZip, nil
}

func BackupAllDatabases(cfg *config.AppConfig) ([]string, error) {
	dbs := ListDatabases(cfg)
	var created []string
	for _, d := range dbs {
		if systemDBSkip[strings.ToLower(d.Name)] {
			continue
		}
		if out, err := BackupDatabase(cfg, d.Name); err == nil {
			created = append(created, out)
		}
	}
	return created, nil
}

func StartAutoBackupScheduler(cfg *config.AppConfig, logFn func(string)) {
	go func() {
		for {
			hours := cfg.AutoBackupIntervalHours
			if hours <= 0 {
				hours = 24
			}
			time.Sleep(time.Duration(hours) * time.Hour)
			if !cfg.AutoBackupEnabled {
				continue
			}
			files, err := BackupAllDatabases(cfg)
			if err == nil && len(files) > 0 && logFn != nil {
				logFn(fmt.Sprintf("Auto-Backup Scheduler: Created %d compressed database backups", len(files)))
			}
		}
	}()
}

func pruneOldBackups(cfg *config.AppConfig) {
	keep := cfg.AutoBackupKeepCount
	if keep <= 0 {
		keep = 10
	}
	list := ListBackups(cfg)
	if len(list) <= keep {
		return
	}
	for _, old := range list[keep:] {
		_ = os.Remove(old.FullPath)
	}
}

func compressSingleFileToZip(srcFile, zipPath string) error {
	zf, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	defer zf.Close()

	zw := zip.NewWriter(zf)
	defer zw.Close()

	in, err := os.Open(srcFile)
	if err != nil {
		return err
	}
	defer in.Close()

	w, err := zw.Create(filepath.Base(srcFile))
	if err != nil {
		return err
	}
	_, err = io.Copy(w, in)
	return err
}

func compressDirectoryToZip(srcDir, zipPath string) error {
	zf, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	defer zf.Close()

	zw := zip.NewWriter(zf)
	defer zw.Close()

	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		w, err := zw.Create(filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		_, err = io.Copy(w, in)
		return err
	})
}

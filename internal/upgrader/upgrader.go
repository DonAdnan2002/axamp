package upgrader

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"axamp/internal/config"
	"axamp/internal/phptransform"
)

type FileReport struct {
	Path       string                `json:"path"`
	RelPath    string                `json:"rel_path"`
	Fixed      int                   `json:"fixed"`
	Notices    int                   `json:"notices"`
	Changed    bool                  `json:"changed"`
	Written    bool                  `json:"written"`
	SyntaxOK   bool                  `json:"syntax_ok"`
	SyntaxMsg  string                `json:"syntax_msg,omitempty"`
	Error      string                `json:"error,omitempty"`
	Changes    []phptransform.Change `json:"changes,omitempty"`
}

type BatchResult struct {
	Root         string       `json:"root"`
	TargetVer    string       `json:"target_version"`
	DryRun       bool         `json:"dry_run"`
	Scanned      int          `json:"scanned"`
	Upgraded     int          `json:"upgraded"`
	TotalFixed   int          `json:"total_fixed"`
	TotalNotices int          `json:"total_notices"`
	Failed       int          `json:"failed"`
	Reports      []FileReport `json:"reports"`
	Error        string       `json:"error,omitempty"`
}

var skipDirs = map[string]bool{
	"vendor": true, "node_modules": true, ".git": true, ".svn": true,
	".hg": true, "storage": true, "cache": true, "tmp": true,
}

func DefaultOptions(target string) phptransform.Options {
	if target == "" {
		target = "8.5"
	}
	return phptransform.Options{
		TargetVersion:          target,
		AddStrictTypes:         false,
		ArrayShortSyntax:       true,
		ListShortSyntax:        true,
		VarToPublic:            true,
		FixCurlyOffsets:        true,
		FixPhp4Constructors:    true,
		ConstructorPromotion:   false,
		FixDeprecatedFunctions: true,
		AllowDynamicProperties: true,
	}
}

func UpgradeSnippet(cfg *config.AppConfig, code string, opts phptransform.Options) map[string]interface{} {
	if opts.TargetVersion == "" {
		opts.TargetVersion = "8.5"
	}
	res := phptransform.Transform(code, opts)
	syntaxOK, syntaxMsg := checkPHPSyntaxString(cfg, res.Code)
	return map[string]interface{}{
		"result":     res,
		"syntax_ok":  syntaxOK,
		"syntax_msg": syntaxMsg,
	}
}

func UpgradeProjectFolder(cfg *config.AppConfig, folder string, opts phptransform.Options, dryRun, backup bool) BatchResult {
	if opts.TargetVersion == "" {
		opts.TargetVersion = "8.5"
	}
	// Resolve relative project names inside htdocs/
	if !filepath.IsAbs(folder) {
		folder = filepath.Join(cfg.RootDir, "htdocs", folder)
	}

	res := BatchResult{
		Root:      folder,
		TargetVer: opts.TargetVersion,
		DryRun:    dryRun,
		Reports:   []FileReport{},
	}

	info, err := os.Stat(folder)
	if err != nil || !info.IsDir() {
		res.Error = fmt.Sprintf("مسار المجلد غير صالح: %s", folder)
		return res
	}

	_ = filepath.WalkDir(folder, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDirs[strings.ToLower(d.Name())] && path != folder {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".php") {
			return nil
		}

		res.Scanned++
		rel, _ := filepath.Rel(folder, path)
		rep := FileReport{
			Path:     path,
			RelPath:  rel,
			SyntaxOK: true,
		}

		content, rerr := os.ReadFile(path)
		if rerr != nil {
			rep.Error = rerr.Error()
			res.Failed++
			res.Reports = append(res.Reports, rep)
			return nil
		}

		out := phptransform.Transform(string(content), opts)
		rep.Fixed = out.FixedCount
		rep.Notices = out.NoticeCount
		rep.Changes = out.Changes
		rep.Changed = out.Code != string(content)
		res.TotalFixed += out.FixedCount
		res.TotalNotices += out.NoticeCount

		if rep.Changed && !dryRun {
			if backup {
				if berr := os.WriteFile(path+".bak", content, 0644); berr != nil {
					rep.Error = "فشل حفظ النسخة الاحتياطية .bak: " + berr.Error()
					res.Failed++
					res.Reports = append(res.Reports, rep)
					return nil
				}
			}
			if werr := os.WriteFile(path, []byte(out.Code), 0644); werr != nil {
				rep.Error = werr.Error()
				res.Failed++
				res.Reports = append(res.Reports, rep)
				return nil
			}
			rep.Written = true
			rep.SyntaxOK, rep.SyntaxMsg = checkPHPSyntaxFile(cfg, path)
		}

		if rep.Changed {
			res.Upgraded++
		}
		if rep.Changed || rep.Notices > 0 || rep.Error != "" {
			res.Reports = append(res.Reports, rep)
		}
		return nil
	})

	sort.SliceStable(res.Reports, func(i, j int) bool {
		if res.Reports[i].Changed != res.Reports[j].Changed {
			return res.Reports[i].Changed
		}
		return res.Reports[i].RelPath < res.Reports[j].RelPath
	})

	return res
}

func LaunchStandaloneStudio() error {
	candidates := []string{
		`E:\php_upgrader\build\bin\php-upgrader.exe`,
		`E:\php_upgrader\php-upgrader.exe`,
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			cmd := exec.Command(c)
			cmd.Dir = filepath.Dir(c)
			return cmd.Start()
		}
	}
	return fmt.Errorf("لم يتم العثور على ملف E:\\php_upgrader\\build\\bin\\php-upgrader.exe")
}

func checkPHPSyntaxFile(cfg *config.AppConfig, filePath string) (bool, string) {
	phpBin := filepath.Join(cfg.RootDir, "bin", "php", "php.exe")
	if _, err := os.Stat(phpBin); os.IsNotExist(err) {
		return true, "php.exe غير متاح للفحص النحوي"
	}
	cmd := exec.Command(phpBin, "-l", filePath)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	msg := strings.TrimSpace(string(out))
	return err == nil, msg
}

func checkPHPSyntaxString(cfg *config.AppConfig, code string) (bool, string) {
	tmpFile := filepath.Join(cfg.RootDir, "tmp", "_upgrader_lint.php")
	_ = os.MkdirAll(filepath.Dir(tmpFile), 0755)
	if err := os.WriteFile(tmpFile, []byte(code), 0644); err != nil {
		return true, ""
	}
	defer os.Remove(tmpFile)
	return checkPHPSyntaxFile(cfg, tmpFile)
}

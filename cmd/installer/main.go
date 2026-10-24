package main

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	port := "14099"
	mux := http.NewServeMux()

	exePath, _ := os.Executable()
	exeDir := filepath.Dir(exePath)
	payloadZip := filepath.Join(exeDir, "AXAMP-v1.0.0-Portable.zip")

	mux.HandleFunc("/logo.png", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath.Join(exeDir, "internal", "server", "ui", "logo.png"))
	})

	mux.HandleFunc("/api/install", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		var req struct {
			TargetDir       string `json:"target_dir"`
			DesktopShortcut bool   `json:"desktop_shortcut"`
			StartMenu       bool   `json:"start_menu"`
			LaunchNow       bool   `json:"launch_now"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.TargetDir == "" {
			req.TargetDir = `C:\AXAMP`
		}

		if err := os.MkdirAll(req.TargetDir, 0755); err != nil {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": err.Error()})
			return
		}

		// Extract from portable zip if available next to installer, otherwise copy core files from exeDir
		if _, err := os.Stat(payloadZip); err == nil {
			if err := extractZip(payloadZip, req.TargetDir); err != nil {
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": err.Error()})
				return
			}
		} else {
			if err := copyLauncherCore(exeDir, req.TargetDir); err != nil {
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": err.Error()})
				return
			}
		}

		installedExe := filepath.Join(req.TargetDir, "axamp-launcher.exe")
		installedIco := filepath.Join(req.TargetDir, "axamp.ico")

		if req.DesktopShortcut {
			desktop := filepath.Join(os.Getenv("USERPROFILE"), "Desktop")
			createShortcut(filepath.Join(desktop, "AXAMP Smart Server.lnk"), installedExe, req.TargetDir, installedIco)
		}
		if req.StartMenu {
			startMenu := filepath.Join(os.Getenv("APPDATA"), `Microsoft\Windows\Start Menu\Programs`)
			createShortcut(filepath.Join(startMenu, "AXAMP Smart Server.lnk"), installedExe, req.TargetDir, installedIco)
		}

		if req.LaunchNow {
			cmd := exec.Command(installedExe)
			cmd.Dir = req.TargetDir
			_ = cmd.Start()
		}

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":         true,
			"target_dir": req.TargetDir,
		})

		go func() {
			time.Sleep(1500 * time.Millisecond)
			os.Exit(0)
		}()
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(installerHTML))
	})

	url := "http://127.0.0.1:" + port
	go func() {
		time.Sleep(300 * time.Millisecond)
		openInstallerWindow(url)
	}()

	fmt.Println("AXAMP Setup Wizard running at", url)
	_ = http.ListenAndServe("127.0.0.1:"+port, mux)
}

func createShortcut(lnkPath, targetExe, workDir, iconPath string) {
	ps := fmt.Sprintf(`$s=(New-Object -COM WScript.Shell).CreateShortcut('%s');$s.TargetPath='%s';$s.WorkingDirectory='%s';$s.IconLocation='%s';$s.Save()`,
		lnkPath, targetExe, workDir, iconPath)
	_ = exec.Command("powershell", "-NoProfile", "-Command", ps).Run()
}

func openInstallerWindow(url string) {
	edgePaths := []string{
		`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
		`C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
	}
	for _, ep := range edgePaths {
		if _, err := os.Stat(ep); err == nil {
			_ = exec.Command(ep, "--app="+url, "--window-size=680,560").Start()
			return
		}
	}
	_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}

func extractZip(zipPath, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		rel := strings.TrimPrefix(strings.ReplaceAll(f.Name, `\`, `/`), "AXAMP/")
		if rel == "" {
			continue
		}
		target := filepath.Join(destDir, filepath.FromSlash(rel))
		if f.FileInfo().IsDir() {
			_ = os.MkdirAll(target, 0755)
			continue
		}
		_ = os.MkdirAll(filepath.Dir(target), 0755)
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			rc.Close()
			return err
		}
		_, _ = io.Copy(out, rc)
		out.Close()
		rc.Close()
	}
	return nil
}

func copyLauncherCore(srcDir, dstDir string) error {
	files := []string{"axamp-launcher.exe", "axamp.ico", "README.md", "LICENSE"}
	for _, name := range files {
		s := filepath.Join(srcDir, name)
		d := filepath.Join(dstDir, name)
		if in, err := os.Open(s); err == nil {
			_ = os.MkdirAll(filepath.Dir(d), 0755)
			if out, err := os.Create(d); err == nil {
				_, _ = io.Copy(out, in)
				out.Close()
			}
			in.Close()
		}
	}
	return nil
}

const installerHTML = `<!DOCTYPE html>
<html lang="ar" dir="rtl">
<head>
<meta charset="UTF-8">
<title>معالج تثبيت AXAMP v1.0.0-stable</title>
<style>
body { margin:0; font-family:'Segoe UI',system-ui,sans-serif; background:#0b1120; color:#f8fafc; display:flex; align-items:center; justify-content:center; height:100vh; }
.box { width:580px; background:#151f32; border:1px solid #26354d; border-radius:16px; padding:28px; box-shadow:0 20px 30px rgba(0,0,0,0.5); }
.hdr { display:flex; align-items:center; gap:16px; border-bottom:1px solid #26354d; padding-bottom:18px; margin-bottom:18px; }
.hdr img { width:64px; height:64px; border-radius:14px; }
input[type="text"] { width:100%; padding:10px 12px; background:#0b1120; border:1px solid #26354d; color:#38bdf8; border-radius:8px; font-size:0.95rem; margin-top:6px; direction:ltr; }
.btn { width:100%; padding:12px; background:linear-gradient(135deg,#0284c7,#2563eb); color:#fff; border:none; border-radius:10px; font-size:1rem; font-weight:bold; cursor:pointer; margin-top:18px; }
.chk { display:block; margin:10px 0; font-size:0.9rem; color:#cbd5e1; }
</style>
</head>
<body>
<div class="box">
    <div class="hdr">
        <img src="/logo.png" onerror="this.style.display='none'">
        <div>
            <h2 style="margin:0;color:#38bdf8;">معالج تثبيت AXAMP v1.0.0-stable</h2>
            <small style="color:#94a3b8;">ابتكار وتطوير: <strong>عدنان سلطان حسن</strong> (donadnan2002@gmail.com)</small>
        </div>
    </div>
    <div>
        <label style="font-size:0.88rem;color:#94a3b8;">مسار التثبيت على الحاسبة (Installation Directory):</label>
        <input type="text" id="target_dir" value="C:\AXAMP">
    </div>
    <div style="margin-top:14px;">
        <label class="chk"><input type="checkbox" id="desktop" checked> إنشاء اختصار بأيقونة AXAMP على سطح المكتب (Desktop Shortcut)</label>
        <label class="chk"><input type="checkbox" id="startmenu" checked> إضافة AXAMP إلى قائمة ابدأ (Start Menu)</label>
        <label class="chk"><input type="checkbox" id="launch" checked> تشغيل AXAMP فوراً بعد انتهاء التثبيت</label>
    </div>
    <button class="btn" id="inst-btn" onclick="doInstall()">🚀 تثبيت AXAMP الآن</button>
    <div id="status" style="margin-top:14px;text-align:center;color:#22c55e;font-weight:bold;"></div>
</div>
<script>
async function doInstall() {
    const btn = document.getElementById('inst-btn');
    const st = document.getElementById('status');
    btn.disabled = true;
    btn.textContent = '⏳ جاري التثبيت وإنشاء الاختصارات...';
    const res = await fetch('/api/install', {
        method: 'POST',
        body: JSON.stringify({
            target_dir: document.getElementById('target_dir').value.trim(),
            desktop_shortcut: document.getElementById('desktop').checked,
            start_menu: document.getElementById('startmenu').checked,
            launch_now: document.getElementById('launch').checked
        })
    }).then(r => r.json());
    if (res.ok) {
        st.textContent = '✅ تم تثبيت AXAMP بنجاح في ' + res.target_dir + '!';
        btn.textContent = 'اكتمل التثبيت بنجاح';
    } else {
        st.style.color = '#ef4444';
        st.textContent = '❌ خطأ: ' + res.error;
        btn.disabled = false;
        btn.textContent = '🚀 تثبيت AXAMP الآن';
    }
}
</script>
</body>
</html>`

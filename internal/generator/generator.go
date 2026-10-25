package generator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"axamp/internal/config"
	"axamp/internal/profiler"
)

func GenerateAll(cfg *config.AppConfig, hw profiler.HardwareProfile) error {
	dirs := []string{
		filepath.Join(cfg.RootDir, "bin", "apache"),
		filepath.Join(cfg.RootDir, "bin", "apache", "logs"),
		filepath.Join(cfg.RootDir, "bin", "php"),
		filepath.Join(cfg.RootDir, "bin", "mysql"),
		filepath.Join(cfg.RootDir, "conf"),
		filepath.Join(cfg.RootDir, "conf", "ssl"),
		filepath.Join(cfg.RootDir, "data"),
		filepath.Join(cfg.RootDir, "htdocs"),
		filepath.Join(cfg.RootDir, "logs"),
		filepath.Join(cfg.RootDir, "tools", "phpmyadmin"),
		filepath.Join(cfg.RootDir, "tools", "adminer"),
		filepath.Join(cfg.RootDir, "tmp"),
	}

	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return err
		}
	}

	ensureDefaultHtdocs(cfg)
	ensureDatabaseTools(cfg)

	if err := GenerateHttpdConf(cfg, hw); err != nil {
		return err
	}
	if err := GeneratePhpIni(cfg, hw); err != nil {
		return err
	}
	if err := GenerateMyIni(cfg, hw); err != nil {
		return err
	}
	return nil
}

func slashPath(p string) string {
	return strings.ReplaceAll(p, `\`, `/`)
}

func resolvePHPDir(cfg *config.AppConfig) string {
	base := filepath.Join(cfg.RootDir, "bin", "php")
	if cfg.ActivePHPVersion != "" && cfg.ActivePHPVersion != "default" {
		verDir := filepath.Join(base, cfg.ActivePHPVersion)
		if info, err := os.Stat(verDir); err == nil && info.IsDir() {
			return verDir
		}
	}
	return base
}

func findApachePHPModule(phpDir string) (string, string) {
	candidates := []struct {
		dll string
		mod string
	}{
		{"php8apache2_4.dll", "php_module"},
		{"php7apache2_4.dll", "php7_module"},
		{"php5apache2_4.dll", "php5_module"},
	}
	for _, c := range candidates {
		full := filepath.Join(phpDir, c.dll)
		if _, err := os.Stat(full); err == nil {
			return slashPath(full), c.mod
		}
	}
	return "", ""
}

func GenerateHttpdConf(cfg *config.AppConfig, hw profiler.HardwareProfile) error {
	root := slashPath(cfg.RootDir)
	phpDll, phpModName := findApachePHPModule(resolvePHPDir(cfg))
	phpIntegrationBlock := "# PHP Integration (Skipped: php.exe / php8apache2_4.dll not installed yet in bin/php)"
	if phpDll != "" {
		phpIntegrationBlock = fmt.Sprintf(`# PHP Integration
LoadModule %s "%s"
PHPIniDir "%s/conf"
AddHandler application/x-httpd-php .php`, phpModName, phpDll, root)
	}
	isDev := cfg.Mode == config.DevMode

	logLevel := "warn"
	dirOptions := "Indexes FollowSymLinks Includes ExecCGI"
	serverTokens := "Prod"
	serverSig := "Off"
	listenAddr := "127.0.0.1:" + cfg.ApachePort

	if isDev {
		logLevel = "info"
		serverTokens = "Full"
		serverSig = "On"
		listenAddr = "0.0.0.0:" + cfg.ApachePort
	} else {
		dirOptions = "-Indexes +FollowSymLinks"
		if cfg.RemoteAccess {
			listenAddr = "0.0.0.0:" + cfg.ApachePort
		}
	}

	var vhostsBlock strings.Builder
	vhostsBlock.WriteString(fmt.Sprintf(`
<VirtualHost *:%s>
    ServerName localhost
    DocumentRoot "%s/htdocs"
</VirtualHost>
`, cfg.ApachePort, root))

	for _, vh := range cfg.VirtualHosts {
		docRoot := slashPath(vh.DocumentRoot)
		if !filepath.IsAbs(vh.DocumentRoot) {
			docRoot = slashPath(filepath.Join(cfg.RootDir, "htdocs", vh.DocumentRoot))
		}
		vhostsBlock.WriteString(fmt.Sprintf(`
<VirtualHost *:%s>
    ServerName %s
    DocumentRoot "%s"
    <Directory "%s">
        Options %s
        AllowOverride All
        Require all granted
    </Directory>
</VirtualHost>
`, cfg.ApachePort, vh.Domain, docRoot, docRoot, dirOptions))
	}

	conf := fmt.Sprintf(`# =====================================================================
# AXAMP Auto-Generated Apache Configuration (Mode: %s)
# Generated dynamically for portability & hardware profile
# =====================================================================
Define SRVROOT "%s/bin/apache"
ServerRoot "${SRVROOT}"

Listen %s
ServerName localhost:%s
ServerTokens %s
ServerSignature %s

LoadModule access_compat_module modules/mod_access_compat.so
LoadModule actions_module modules/mod_actions.so
LoadModule alias_module modules/mod_alias.so
LoadModule allowmethods_module modules/mod_allowmethods.so
LoadModule asis_module modules/mod_asis.so
LoadModule auth_basic_module modules/mod_auth_basic.so
LoadModule authn_core_module modules/mod_authn_core.so
LoadModule authn_file_module modules/mod_authn_file.so
LoadModule authz_core_module modules/mod_authz_core.so
LoadModule authz_groupfile_module modules/mod_authz_groupfile.so
LoadModule authz_host_module modules/mod_authz_host.so
LoadModule authz_user_module modules/mod_authz_user.so
LoadModule autoindex_module modules/mod_autoindex.so
LoadModule cgi_module modules/mod_cgi.so
LoadModule dir_module modules/mod_dir.so
LoadModule env_module modules/mod_env.so
LoadModule headers_module modules/mod_headers.so
LoadModule include_module modules/mod_include.so
LoadModule isapi_module modules/mod_isapi.so
LoadModule log_config_module modules/mod_log_config.so
LoadModule mime_module modules/mod_mime.so
LoadModule negotiation_module modules/mod_negotiation.so
LoadModule rewrite_module modules/mod_rewrite.so
LoadModule setenvif_module modules/mod_setenvif.so
LoadModule socache_shmcb_module modules/mod_socache_shmcb.so
LoadModule ssl_module modules/mod_ssl.so

%s

<IfModule unixd_module>
    User daemon
    Group daemon
</IfModule>

DocumentRoot "%s/htdocs"
<Directory />
    AllowOverride none
    Require all denied
</Directory>

<Directory "%s/htdocs">
    Options %s
    AllowOverride All
    Require all granted
</Directory>

# Built-in Database Management Aliases (phpMyAdmin & Adminer)
Alias /phpmyadmin "%s/tools/phpmyadmin"
<Directory "%s/tools/phpmyadmin">
    Options Indexes FollowSymLinks
    AllowOverride All
    Require all granted
</Directory>

Alias /adminer "%s/tools/adminer"
<Directory "%s/tools/adminer">
    Options Indexes FollowSymLinks
    AllowOverride All
    Require all granted
</Directory>

<IfModule dir_module>
    DirectoryIndex index.php index.html index.htm
</IfModule>

<Files ".ht*">
    Require all denied
</Files>

PidFile "%s/logs/httpd.pid"
ErrorLog "%s/logs/apache_error.log"
LogLevel %s

<IfModule log_config_module>
    LogFormat "%%h %%l %%u %%t \"%%r\" %%>s %%b" common
    CustomLog "%s/logs/apache_access.log" common
</IfModule>

<IfModule mime_module>
    TypesConfig conf/mime.types
    AddType application/x-compress .Z
    AddType application/x-gzip .gz .tgz
    AddType application/x-httpd-php .php
</IfModule>

# Dynamic VirtualHosts
%s
`,
		cfg.Mode,
		root,
		listenAddr,
		cfg.ApachePort,
		serverTokens,
		serverSig,
		phpIntegrationBlock,
		root,
		root,
		dirOptions,
		root,
		root,
		root,
		root,
		root,
		root,
		logLevel,
		root,
		vhostsBlock.String(),
	)

	outPath := filepath.Join(cfg.RootDir, "conf", "httpd.conf")
	return os.WriteFile(outPath, []byte(conf), 0644)
}

func GeneratePhpIni(cfg *config.AppConfig, hw profiler.HardwareProfile) error {
	root := slashPath(cfg.RootDir)
	phpDir := slashPath(resolvePHPDir(cfg))
	isDev := cfg.Mode == config.DevMode

	displayErrors := "Off"
	displayStartup := "Off"
	errorReporting := "E_ALL & ~E_DEPRECATED"
	opcacheEnable := "1"
	opcacheRevalidate := "60"
	exposePhp := "Off"
	maxExecTime := "60"

	if isDev {
		displayErrors = "On"
		displayStartup = "On"
		errorReporting = "E_ALL"
		opcacheEnable = "0"
		opcacheRevalidate = "0"
		exposePhp = "On"
		maxExecTime = "300"
	}

	var extLines strings.Builder
	extDir := filepath.Join(resolvePHPDir(cfg), "ext")
	for ext, enabled := range cfg.PHPExtensions {
		cleanExt := strings.TrimSpace(ext)
		cleanExt = strings.TrimPrefix(cleanExt, "php_")
		cleanExt = strings.TrimSuffix(cleanExt, ".dll")
		dllPath := filepath.Join(extDir, "php_"+cleanExt+".dll")
		if enabled {
			if _, err := os.Stat(dllPath); err == nil {
				extLines.WriteString(fmt.Sprintf("extension=%s\n", cleanExt))
			} else {
				extLines.WriteString(fmt.Sprintf(";extension=%s ; (skipped: php_%s.dll not found in ext/)\n", cleanExt, cleanExt))
			}
		} else {
			extLines.WriteString(fmt.Sprintf(";extension=%s\n", cleanExt))
		}
	}

	opcacheLine := fmt.Sprintf("opcache.enable=%s", opcacheEnable)
	if _, err := os.Stat(filepath.Join(extDir, "php_opcache.dll")); err == nil {
		opcacheLine = "zend_extension=opcache\n" + opcacheLine
	}

	ini := fmt.Sprintf(`; =====================================================================
; AXAMP Auto-Generated PHP Configuration (Mode: %s)
; Hardware Tuned: %d MB System RAM Detected
; =====================================================================
[PHP]
engine = On
short_open_tag = On
precision = 14
output_buffering = 4096
zlib.output_compression = Off
implicit_flush = Off
serialize_precision = -1
expose_php = %s

; Resource Limits (Hardware Profiled)
max_execution_time = %s
max_input_time = 120
memory_limit = %s
post_max_size = %s
upload_max_filesize = %s
max_file_uploads = 50

; Error Handling & Logging
error_reporting = %s
display_errors = %s
display_startup_errors = %s
log_errors = On
error_log = "%s/logs/php_error.log"

; Portable Relative/Dynamic Paths
extension_dir = "%s/ext"
sys_temp_dir = "%s/tmp"
upload_tmp_dir = "%s/tmp"
session.save_path = "%s/tmp"
session.upload_progress.enabled = 0
fatal_error_backtraces = 0

; Dynamic Extensions (Managed by AXAMP GUI & ODBC Manager)
%s

[Date]
date.timezone = UTC

[opcache]
%s
opcache.enable_cli=0
opcache.memory_consumption=128
opcache.interned_strings_buffer=16
opcache.max_accelerated_files=10000
opcache.revalidate_freq=%s

[ODBC]
odbc.allow_persistent = On
odbc.check_persistent = On
odbc.max_persistent = -1
odbc.max_links = -1
odbc.defaultlrl = 4096
odbc.defaultbinmode = 1

[MySQLi]
mysqli.default_port = %s
mysqli.default_host = 127.0.0.1
`,
		cfg.Mode,
		hw.TotalRAMMB,
		exposePhp,
		maxExecTime,
		hw.RecommendedPHPMemory,
		hw.RecommendedPostMax,
		hw.RecommendedPostMax,
		errorReporting,
		displayErrors,
		displayStartup,
		root,
		phpDir,
		root,
		root,
		root,
		extLines.String(),
		opcacheLine,
		opcacheRevalidate,
		cfg.MySQLPort,
	)

	outPath := filepath.Join(cfg.RootDir, "conf", "php.ini")
	return os.WriteFile(outPath, []byte(ini), 0644)
}

func GenerateMyIni(cfg *config.AppConfig, hw profiler.HardwareProfile) error {
	root := slashPath(cfg.RootDir)
	isDev := cfg.Mode == config.DevMode

	bindAddr := "127.0.0.1"
	if cfg.RemoteAccess {
		bindAddr = "0.0.0.0"
	}

	slowQuery := "0"
	if isDev {
		slowQuery = "1"
	}

	ini := fmt.Sprintf(`# =====================================================================
# AXAMP Auto-Generated MySQL / MariaDB Configuration (Mode: %s)
# Hardware Tuned: %d MB RAM -> InnoDB Buffer Pool: %s
# =====================================================================
[client]
port=%s
default-character-set=utf8mb4

[mysqld]
port=%s
bind-address=%s
basedir="%s/bin/mysql"
datadir="%s/data"
tmpdir="%s/tmp"
log-error="%s/logs/mysql_error.log"

character-set-server=utf8mb4
collation-server=utf8mb4_unicode_ci

# Hardware Auto-Tuned Parameters
max_connections=%d
innodb_buffer_pool_size=%s
innodb_log_file_size=64M
innodb_flush_log_at_trx_commit=2
key_buffer_size=32M
max_allowed_packet=128M
thread_cache_size=16

# Mode Specific Diagnostics
slow_query_log=%s
slow_query_log_file="%s/logs/mysql_slow.log"
long_query_time=2
`,
		cfg.Mode,
		hw.TotalRAMMB,
		hw.RecommendedInnoDBSize,
		cfg.MySQLPort,
		cfg.MySQLPort,
		bindAddr,
		root,
		root,
		root,
		root,
		hw.RecommendedMaxConn,
		hw.RecommendedInnoDBSize,
		slowQuery,
		root,
	)

	outPath := filepath.Join(cfg.RootDir, "conf", "my.ini")
	return os.WriteFile(outPath, []byte(ini), 0644)
}

func ensureDefaultHtdocs(cfg *config.AppConfig) {
	indexPath := filepath.Join(cfg.RootDir, "htdocs", "index.php")
	welcomePHP := `<?php
$mode = getenv('AXAMP_MODE') ?: 'Smart Dynamic Mode';
?>
<!DOCTYPE html>
<html lang="ar" dir="rtl">
<head>
    <meta charset="UTF-8">
    <title>AXAMP - Smart Local Server | ابتكار: عدنان سلطان حسن</title>
    <style>
        body { font-family: system-ui, -apple-system, sans-serif; background: #0f172a; color: #f8fafc; margin: 0; padding: 40px; }
        .card { max-width: 900px; margin: 0 auto; background: #1e293b; border-radius: 16px; padding: 32px; border: 1px solid #334155; box-shadow: 0 20px 25px -5px rgba(0,0,0,0.4); }
        h1 { color: #38bdf8; margin-top: 0; }
        .creator-badge { background: rgba(56,189,248,0.12); border: 1px solid #0284c7; padding: 10px 16px; border-radius: 10px; margin-bottom: 18px; display: inline-block; font-size: 0.92rem; color: #e0f2fe; }
        .creator-badge a { color: #38bdf8; text-decoration: none; font-weight: bold; }
        .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 16px; margin: 24px 0; }
        .stat { background: #0f172a; padding: 16px; border-radius: 12px; border: 1px solid #1e293b; }
        .stat span { display: block; color: #94a3b8; font-size: 0.85rem; }
        .stat strong { font-size: 1.2rem; color: #22d3ee; }
        .links a { display: inline-block; margin-left: 12px; padding: 10px 18px; border-radius: 8px; background: #0284c7; color: white; text-decoration: none; font-weight: 600; }
        .links a.secondary { background: #334155; }
    </style>
</head>
<body>
    <div class="card">
        <h1>🚀 أهلاً بك في AXAMP Smart Server</h1>
        <div class="creator-badge">
            👨‍💻 مبتكر النظام: <strong>عدنان سلطان حسن</strong> — <a href="mailto:donadnan2002@gmail.com">donadnan2002@gmail.com</a>
        </div>
        <p>الخادم المحلي الذكي والمحمول يعمل بنجاح من المسار: <code><?php echo __DIR__; ?></code></p>
        <div class="grid">
            <div class="stat"><span>إصدار PHP</span><strong><?php echo PHP_VERSION; ?></strong></div>
            <div class="stat"><span>حد الذاكرة (Memory Limit)</span><strong><?php echo ini_get('memory_limit'); ?></strong></div>
            <div class="stat"><span>إظهار الأخطاء (Display Errors)</span><strong><?php echo ini_get('display_errors') ? 'مفعل (Development)' : 'مغلق (Production)'; ?></strong></div>
            <div class="stat"><span>معمارية النظام</span><strong><?php echo PHP_INT_SIZE === 8 ? '64-Bit (x64)' : '32-Bit (x86)'; ?></strong></div>
        </div>
        <div class="links">
            <a href="/phpmyadmin">فتح phpMyAdmin</a>
            <a href="/adminer">فتح Adminer السريع</a>
            <a class="secondary" href="?phpinfo=1">عرض phpinfo()</a>
        </div>
        <?php if (isset($_GET['phpinfo'])) { echo '<hr style="margin:24px 0;border-color:#334155;">'; phpinfo(); } ?>
    </div>
</body>
</html>`
	_ = os.WriteFile(indexPath, []byte(welcomePHP), 0644)
}

func ensureDatabaseTools(cfg *config.AppConfig) {
	// 1. Auto-configure phpMyAdmin config.inc.php dynamically with current MySQL port
	pmaDir := filepath.Join(cfg.RootDir, "tools", "phpmyadmin")
	pmaConfig := fmt.Sprintf(`<?php
/* AXAMP Auto-Generated phpMyAdmin Configuration */
$cfg['blowfish_secret'] = 'axamp_smart_secret_key_32bytes_!!';
$i = 0;
$i++;
$cfg['Servers'][$i]['auth_type'] = 'config';
$cfg['Servers'][$i]['user'] = 'root';
$cfg['Servers'][$i]['password'] = '';
$cfg['Servers'][$i]['host'] = '127.0.0.1';
$cfg['Servers'][$i]['port'] = '%s';
$cfg['Servers'][$i]['compress'] = false;
$cfg['Servers'][$i]['AllowNoPassword'] = true;
$cfg['UploadDir'] = '';
$cfg['SaveDir'] = '';
$cfg['TempDir'] = '%s/tmp';
$cfg['SendErrorReports'] = 'never';
$cfg['PmaNoRelation_DisableWarning'] = true;
`, cfg.MySQLPort, slashPath(cfg.RootDir))
	_ = os.WriteFile(filepath.Join(pmaDir, "config.inc.php"), []byte(pmaConfig), 0644)

	// Provide a fallback index.php inside tools/phpmyadmin ONLY if full phpMyAdmin is not installed yet
	pmaIndex := filepath.Join(pmaDir, "index.php")
	pmaLibs := filepath.Join(pmaDir, "libraries")
	if _, err := os.Stat(pmaLibs); os.IsNotExist(err) {
		if _, err := os.Stat(pmaIndex); os.IsNotExist(err) {
			placeholder := fmt.Sprintf(`<?php
// AXAMP Smart Database Gateway (phpMyAdmin / Built-in SQL Console)
header("Location: http://127.0.0.1:%s/?tab=database");
exit;
`, cfg.ControlPort)
			_ = os.WriteFile(pmaIndex, []byte(placeholder), 0644)
		}
	}

	// 2. Built-in lightweight PHP SQL studio in /adminer/index.php
	adminerIndex := filepath.Join(cfg.RootDir, "tools", "adminer", "index.php")
	liteStudio := fmt.Sprintf(`<?php
$host = '127.0.0.1';
$port = '%s';
$user = $_POST['user'] ?? 'root';
$pass = $_POST['pass'] ?? '';
$db   = $_GET['db'] ?? '';
$sql  = $_POST['sql'] ?? '';
$msg  = '';
$rows = [];

try {
    $dsn = "mysql:host=$host;port=$port" . ($db ? ";dbname=$db" : "") . ";charset=utf8mb4";
    $pdo = new PDO($dsn, $user, $pass, [PDO::ATTR_ERRMODE => PDO::ERRMODE_EXCEPTION]);
    $dbs = $pdo->query("SHOW DATABASES")->fetchAll(PDO::FETCH_COLUMN);
    if ($sql) {
        $stmt = $pdo->query($sql);
        if ($stmt->columnCount() > 0) {
            $rows = $stmt->fetchAll(PDO::FETCH_ASSOC);
        } else {
            $msg = "تم تنفيذ الاستعلام بنجاح. الصفوف المتأثرة: " . $stmt->rowCount();
        }
    }
} catch (Exception $e) {
    $msg = "خطأ اتصال MySQL: " . $e->getMessage();
    $dbs = [];
}
?>
<!DOCTYPE html>
<html lang="ar" dir="rtl">
<head>
<meta charset="UTF-8">
<title>AXAMP SQL Studio | ابتكار: عدنان سلطان حسن</title>
<style>
body { font-family: system-ui, sans-serif; background: #0f172a; color: #f8fafc; margin: 0; display: flex; height: 100vh; }
.sidebar { width: 250px; background: #1e293b; padding: 20px; border-left: 1px solid #334155; overflow-y: auto; display: flex; flex-direction: column; }
.main { flex: 1; padding: 24px; overflow-y: auto; }
a { color: #38bdf8; text-decoration: none; display: block; padding: 8px; border-radius: 6px; }
a:hover, a.active { background: #334155; }
textarea { width: 100%%; height: 110px; background: #0f172a; color: #38bdf8; border: 1px solid #334155; border-radius: 8px; padding: 12px; font-family: monospace; }
button { background: #0284c7; color: #fff; border: none; padding: 10px 20px; border-radius: 8px; cursor: pointer; font-weight: bold; margin-top: 8px; }
table { width: 100%%; border-collapse: collapse; margin-top: 20px; background: #1e293b; }
th, td { border: 1px solid #334155; padding: 8px 12px; text-align: right; font-size: 0.9rem; }
th { background: #0f172a; color: #38bdf8; }
</style>
</head>
<body>
<div class="sidebar">
    <h3>🗄️ قواعد البيانات</h3>
    <?php foreach ($dbs as $d): ?>
        <a class="<?= $d === $db ? 'active' : '' ?>" href="?db=<?= urlencode($d) ?>"><?= htmlspecialchars($d) ?></a>
    <?php endforeach; ?>
    <div style="margin-top:auto;padding-top:14px;border-top:1px solid #334155;font-size:0.78rem;color:#94a3b8;">
        مبتكر النظام: <strong style="color:#38bdf8;">عدنان سلطان حسن</strong><br>donadnan2002@gmail.com
    </div>
</div>
<div class="main">
    <h2>AXAMP SQL Studio <?= $db ? "— [$db]" : "" ?></h2>
    <?php if ($msg): ?><div style="padding:12px;background:#334155;border-radius:8px;margin-bottom:12px;"><?= htmlspecialchars($msg) ?></div><?php endif; ?>
    <form method="post">
        <textarea name="sql" placeholder="اكتب استعلام SQL هنا... مثال: SHOW TABLES;"><?= htmlspecialchars($sql ?: ($db ? 'SHOW TABLES;' : 'SHOW STATUS;')) ?></textarea>
        <button type="submit">تنفيذ الاستعلام (Run SQL)</button>
    </form>
    <?php if (!empty($rows)): ?>
        <table>
            <thead><tr><?php foreach (array_keys($rows[0]) as $col): ?><th><?= htmlspecialchars($col) ?></th><?php endforeach; ?></tr></thead>
            <tbody>
                <?php foreach ($rows as $r): ?>
                    <tr><?php foreach ($r as $val): ?><td><?= htmlspecialchars((string)$val) ?></td><?php endforeach; ?></tr>
                <?php endforeach; ?>
            </tbody>
        </table>
    <?php endif; ?>
</div>
</body>
</html>`, cfg.MySQLPort)
	_ = os.WriteFile(adminerIndex, []byte(liteStudio), 0644)
}

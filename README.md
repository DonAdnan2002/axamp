# 🚀 AXAMP v2.0.0-stable
> **The Smart, Lightweight, and Standalone XAMPP Alternative Powered by Go**  
> **بديل XAMPP الذكي، المحمول، والمستقل بالكامل بلغة Go — الإصدار 2.0**

👨‍💻 **مبتكر النظام (System Creator):** **عدنان سلطان حسن (Adnan Sultan Hassan)** — [`donadnan2002@gmail.com`](mailto:donadnan2002@gmail.com)

---

**AXAMP v2.0** is an enterprise-grade, offline-first local server stack designed for developers and production environments. It replaces traditional heavy setups with a lightning-fast, self-contained Go binary featuring live real-time telemetry, visual server health charts, an incoming connections inspector, full bilingual support (Arabic & English), smart hardware auto-tuning, hybrid package management, true Windows services integration, XAMPP migration wizard, ODBC manager, and an advanced PHP 8.5 legacy code modernizer.

---

## ✨ Key Features | المزايا الرئيسية

1. **📈 Real-Time Server Telemetry & Visual Charts (New in v2.0):**
   - Live hardware-accelerated SVG charts tracking real-time **CPU Load (%)**, **System Memory (RAM % & Used MB)**, and **Active Concurrent TCP Connections**.
   - Zero-overhead Windows API telemetry sampling (`GetSystemTimes`, `GlobalMemoryStatusEx`) with smooth historical trendlines.

2. **🌐 Real-Time Incoming Connections Inspector (New in v2.0):**
   - Live socket monitoring of incoming server connections across Apache HTTP (port 80/443), MySQL/MariaDB (port 3306), and the AXAMP Control Dashboard.
   - Inspect client remote IP, target port, connection state (`ESTABLISHED`, `TIME_WAIT`, `CLOSE_WAIT`), and per-service concurrent socket counts.

3. **🌍 Full Bilingual Experience — العربية & English (New in v2.0):**
   - Instant one-click language toggle (`[ 🇸🇦 العربية | 🇬🇧 English ]`) with dynamic bidirectional layout (`RTL` / `LTR`).
   - Seamless translation of all navigation, control panels, status badges, advisories, and telemetry metrics with persistent preferences.

4. **🚀 Advanced PHP 8.5 Legacy Code Modernizer (`internal/phptransform` & `internal/upgrader`):**
   - Automated compatibility engine for legacy PHP applications (PHPRunner, legacy CMS, custom codebases):
     - Safe handling of `count()` and `sizeof()` on null/scalars.
     - Stripping conflicting constructor property promotions preventing `Fatal error: Cannot redeclare` issues.
     - Resolving deprecated constants (`MYSQL_ASSOC` $\rightarrow$ `MYSQLI_ASSOC`, `E_STRICT`).
     - Modernizing deprecated functions (`get_magic_quotes_gpc()`, `get_magic_quotes_runtime()`).
     - Safe arithmetic type checking for `postvalue_number()` and array dereferencing.
     - Live **`PHP 8.5.12 -l` syntax verification** and automated `.bak` backups.

5. **🧠 Smart Hardware & Port Profiler (`internal/profiler`):**
   - Automatically detects CPU cores and system RAM to dynamically tune PHP (`memory_limit`, `post_max_size`) and MySQL/MariaDB (`innodb_buffer_pool_size`, `max_connections`).
   - Smart port scanner automatically resolves conflicts on port `80` or `3306` without failing startup.

6. **🔄 Dual Operational Modes (`Development` vs `Production`):**
   - **Development Mode:** Enables `display_errors = On`, `E_ALL`, disables OPcache caching for instant code changes, and enables MySQL `slow_query_log`.
   - **Production Mode:** Enforces security (`display_errors = Off`, `ServerTokens Prod`, `-Indexes`), maximizes OPcache performance, and restricts external exposure.

7. **📦 Hybrid Package Manager — Offline-First (`internal/pkgmanager`):**
   - **Zero-Internet XAMPP Cloner:** Instantly clone Apache, PHP, MySQL, and phpMyAdmin binaries from any existing local `C:\xampp` installation without internet.
   - **Air-Gapped ZIP Installer:** Install portable packages from local `.zip` archives.
   - **Online Mirror Downloader:** Direct download with live progress reporting when connected.

8. **📦 Smart XAMPP Migration Wizard (`internal/migrator`):**
   - Scans legacy XAMPP folders and safely imports `htdocs` projects, `mysql/data` databases, and enabled `php.ini` extensions in one click.

9. **⚙️ Process Supervisor & True Windows Services (`internal/supervisor`):**
   - Self-healing process supervisor automatically restarts Apache or MySQL if they crash.
   - Auto-initializes fresh MySQL/MariaDB `data/` directories (`--initialize-insecure` / `mysql_install_db.exe`).
   - One-click registration of `AXAMP_Apache` and `AXAMP_MySQL` as native Windows background services.

10. **🗄️ Integrated Database Suite & Auto-Backup Scheduler (`internal/dbmanager`):**
    - Auto-configured `phpMyAdmin` (`config.inc.php` dynamically linked to active MySQL port) + built-in lightweight `AXAMP SQL Studio` (`/adminer`).
    - Scheduled and on-demand `.sql.zip` / `raw.zip` compressed backups with automatic pruning.

11. **🔌 Seamless ODBC & Legacy Integration (`internal/odbc`):**
    - Built-in Windows Registry DSN manager (User & System DSNs) and 32-bit vs 64-bit architecture mismatch detector.

12. **🛡️ Automated Local Security Auditor & DevSecOps Advisories (`internal/secguard`):**
    - Real-time security scoring (`0–100`) and grading (`A+` to `D`), scanning `php.ini`, `httpd.conf`, `phpMyAdmin` auth modes, and exposed secrets inside `htdocs/`.
    - One-click **Auto-Hardening** (`.htaccess` secret protection, `disable_functions` hardening, production mode switching).

---

## 🛠️ Quick Start | البدء السريع

```bash
# 1. Run E2E & Integration Tests
go test -v ./...

# 2. Build Standalone Executable
go build -o axamp-launcher.exe .

# 3. Launch AXAMP Control Center
./axamp-launcher.exe
```

---

## 📜 License
Distributed under the MIT License. See [`LICENSE`](LICENSE) for more information.

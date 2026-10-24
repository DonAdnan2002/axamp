# 🚀 AXAMP v1.0.0-stable
> **The Smart, Lightweight, and Standalone XAMPP Alternative Powered by Go**  
> **بديل XAMPP الذكي، المحمول، والمستقل بالكامل بلغة Go**

👨‍💻 **مبتكر النظام (System Creator):** **عدنان سلطان حسن (Adnan Sultan Hassan)** — [`donadnan2002@gmail.com`](mailto:donadnan2002@gmail.com)

---

**AXAMP** is an enterprise-grade, offline-first local server stack designed for developers and isolated environments (Air-gapped networks). It replaces heavy traditional tools with a lightning-fast, self-contained Go binary featuring smart hardware auto-tuning, hybrid package management, true Windows services integration, XAMPP migration wizard, ODBC manager, and automated compressed backups.

---

## ✨ Key Features | المزايا الرئيسية

1. **🧠 Smart Hardware & Port Profiler (`internal/profiler`):**
   - Automatically detects CPU cores and system RAM (`GlobalMemoryStatusEx`) to dynamically tune PHP (`memory_limit`, `post_max_size`) and MySQL/MariaDB (`innodb_buffer_pool_size`, `max_connections`).
   - Smart port scanner automatically resolves conflicts on port `80` or `3306` without failing startup.

2. **🔄 Dual Operational Modes (`Development` vs `Production`):**
   - **Development Mode:** Enables `display_errors = On`, `E_ALL`, disables OPcache caching for instant code changes, and enables MySQL `slow_query_log`.
   - **Production Mode:** Enforces security (`display_errors = Off`, `ServerTokens Prod`, `-Indexes`), maximizes OPcache performance, and restricts external exposure.

3. **📦 Hybrid Package Manager — Offline-First (`internal/pkgmanager`):**
   - **Zero-Internet XAMPP Cloner:** Instantly clone Apache, PHP, MySQL, and phpMyAdmin binaries from any existing local `C:\xampp` installation without internet.
   - **Air-Gapped ZIP Installer:** Install portable packages from local `.zip` archives.
   - **Online Mirror Downloader:** Direct download with live progress reporting when connected.

4. **📦 Smart XAMPP Migration Wizard (`internal/migrator`):**
   - Scans legacy XAMPP folders and safely imports `htdocs` projects, `mysql/data` databases, and enabled `php.ini` extensions in one click.

5. **⚙️ Process Supervisor & True Windows Services (`internal/supervisor`):**
   - Self-healing process supervisor automatically restarts Apache or MySQL if they crash.
   - Auto-initializes fresh MySQL/MariaDB `data/` directories (`--initialize-insecure` / `mysql_install_db.exe`).
   - One-click registration of `AXAMP_Apache` and `AXAMP_MySQL` as native Windows background services.

6. **🗄️ Integrated Database Suite & Auto-Backup Scheduler (`internal/dbmanager`):**
   - Auto-configured `phpMyAdmin` (`config.inc.php` dynamically linked to active MySQL port) + built-in lightweight `AXAMP SQL Studio` (`/adminer`).
   - Scheduled and on-demand `.sql.zip` / `raw.zip` compressed backups with automatic pruning.

7. **🔌 Seamless ODBC & Legacy Integration (`internal/odbc`):**
   - Built-in Windows Registry DSN manager (User & System DSNs) and 32-bit vs 64-bit architecture mismatch detector.

8. **🛡️ Embedded Desktop GUI & PIN-Protected LAN Control (`internal/server`):**
   - Zero-dependency embedded UI launched as a native-like Windows App window (`http://127.0.0.1:4040`) with optional PIN-protected remote LAN management.

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

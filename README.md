<div align="center">

# AXAMP

**Your legacy PHP 5 code, automatically modernized to PHP 8.5 — by a single Go binary.**

*The smart, lightweight, standalone local web stack: Apache + MySQL + PHP, zero dependencies, 100% Go standard library.*

[![Go Version](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](./LICENSE)
[![Platform: Windows](https://img.shields.io/badge/Platform-Windows-0078D6?logo=windows11)](https://github.com/DonAdnan2002/axamp/releases)
[![Zero Dependencies](https://img.shields.io/badge/Dependencies-0-success)](./go.mod)

<!-- TODO(📸 GIF قبل الإطلاق): سجّل GIF بـ 15-20 ثانية هنا — البانوراما: تشغيل الخدمات،
     لوحة القياسات الحية، لصق كود PHP قديم في المُحدّث ومشاهدة تحويله، تبديل اللغة AR/EN.
     ضع الملف في docs/demo.gif ثم استبدل السطر التالي: -->
<!-- ![AXAMP Demo](docs/demo.gif) -->

**[Download](https://github.com/DonAdnan2002/axamp/releases/latest)** · **[The Modernizer](#-the-php-85-modernizer)** · **[Features](#-features)** · **[النسخة العربية](#-النسخة-العربية)**

</div>

---

## Why AXAMP?

Every PHP 8 upgrade estimate on a legacy codebase starts with *"rewrite half of it first"* — `mysql_*` calls, `ereg()`, PHP4 constructors, and two decades of deprecations. AXAMP migrates that code **automatically**, and runs the full local stack around it so you can test the result on PHP 8.5 immediately.

One 8 MB binary. No installer required. No dependencies — the whole thing is Go standard library, and the entire control panel UI is embedded inside the executable.

## 🔬 The PHP 8.5 Modernizer

A hand-written PHP lexer + transpiler in pure Go (~1,300 lines, 50+ tests, fuzzed). Point it at a legacy project folder (or paste a snippet in the UI):

```php
// Before (PHP 5 era)                      // After (PHP 8.5, verified by php -l)
$res = mysql_query($sql);                  $res = mysqli_query($mysqli, $sql);
$row = mysql_fetch_array($res, MYSQL_ASSOC);  $row = mysqli_fetch_array($res, MYSQLI_ASSOC);
if (ereg("^[0-9]+$", $id)) { ... }         if (preg_match('/^[0-9]+$/', $id)) { ... }
$fn = create_function('$a', 'return $a;');  $fn = function($a) { return $a; };
```

The full catalog: `mysql_*` → `mysqli` (annotated mappings) · `ereg`/`split` families → `preg_*` with automatic delimiter selection · `create_function` → closures · PHP4 constructors → `__construct` · `utf8_encode` → `mb_convert_encoding` (≥ 8.2 targets) · `E_STRICT`, `get_magic_quotes_gpc`, implicit nullable params (the 8.4 deprecation), `real` casts, curly string offsets, `count()` null guards, and a fix for the *"Cannot redeclare"* fatal when legacy layouts collide with constructor property promotion.

**Safety model** — because a migration tool that breaks code is worse than no tool:

| Guarantee | How |
|---|---|
| Nothing touched that isn't understood | Token-level lexer; strings, comments and heredocs pass through byte-for-byte |
| Reviewable changes | Every rewrite is comment-annotated |
| Survives mistakes | `.bak` backup per file + dry-run mode |
| Provably valid output | `php -l` (8.5) syntax verification after every file |
| Run it twice, nothing changes | Idempotency is an enforced test invariant |

## ✨ Features

- **Full local stack supervisor** — Apache, MySQL/MariaDB, PHP with auto-heal and a restart circuit breaker (gives up gracefully after 3 failures instead of loop-restarting)
- **Smart port recovery** — detects what's holding port 80/3306 and transparently rebinds, configs included
- **Hardware auto-tuning** — `php.ini` and `my.ini` generated to match your CPU cores and RAM
- **Real-time telemetry** — live CPU/RAM charts and an incoming-connections inspector (IP, port, socket state)
- **Dev / Production modes** — one click switches error display, OPcache, and hardening posture
- **Scheduled compressed backups** — `.sql.zip` via mysqldump with offline raw-folder fallback and auto-pruning
- **Security auditor** — scores your `php.ini`/`httpd.conf` posture and applies one-click hardening
- **XAMPP migration wizard** — imports `htdocs`, databases, and enabled extensions from an existing XAMPP install
- **True Windows services** — optional `AXAMP_Apache` / `AXAMP_MySQL` native service registration
- **Fully bilingual UI** — العربية / English with instant RTL/LTR switching

<!-- TODO(📸 سكرينشوتس قبل الإطلاق): 3 صور بعرض 1200px في مجلد docs/ —
     1. لوحة القياسات مع الرسوم الحية  2. المُحدّث قبل/بعد  3. فاحص الاتصالات + تبديل اللغة
     ثم فعّل بلوك الصور هذا:
### Screenshots

| Dashboard | Modernizer | Connections Inspector |
|:---:|:---:|:---:|
| ![](docs/dashboard.png) | ![](docs/modernizer.png) | ![](docs/connections.png) |
-->

## 🚀 Install

**Download & run** (no installer needed): grab the latest `Portable.zip` from [Releases](https://github.com/DonAdnan2002/axamp/releases/latest), unzip anywhere, run `axamp-launcher.exe`.

Package managers *(submitted, landing soon)*:
```powershell
winget install DonAdnan.AXAMP      # PR pending
scoop bucket add axamp https://github.com/DonAdnan2002/scoop-bucket && scoop install axamp
```

Apache / PHP / MySQL binaries install through the built-in package manager on first run: clone from an existing `C:\xampp`, install from a local ZIP, or download online.

**Build from source** (Go 1.25+):
```bash
go build -ldflags "-s -w" -o axamp-launcher.exe .
./axamp-launcher.exe          # GUI window
./axamp-launcher.exe --headless   # console mode
```

## 🛡️ Control-Plane Security

The embedded dashboard takes its own security seriously:

- Binds **loopback only** (`127.0.0.1`) unless remote access is explicitly enabled
- **DNS-rebinding protection** — local requests must carry a loopback `Host` header
- Remote access (opt-in) requires a PIN transmitted **via header only**, compared in constant time, with per-IP brute-force lockout
- The PIN is never included in API responses; it's readable only from the local machine
- Enforced by [regression tests](./internal/server/server_test.go)

## 🗺️ Roadmap

- [ ] Linux support (supervisor and config generator are already platform-agnostic code)
- [ ] Code-signed release binaries
- [ ] winget / Scoop / Chocolatey availability

## 🤝 Contributing

The modernizer grows **pattern by pattern** — if it misses a legacy pattern in your codebase, [open an issue](https://github.com/DonAdnan2002/axamp/issues) with a (redacted) snippet. That's the most valuable contribution possible.

```bash
go test ./...   # run the suite before submitting
```

## 📜 License

MIT — see [LICENSE](./LICENSE).

<div align="center">

**Created by [Adnan Sultan Hassan](mailto:donadnan2002@gmail.com)**

*مبتكر النظام: عدنان سلطان حسن*

</div>

---

## 🇸🇦 النسخة العربية

**AXAMP** — بديل XAMPP الذكي والمحمول بالكامل: مكدس خوادم محلي (Apache + MySQL/MariaDB + PHP) في ملف Go واحد بدون أي تبعيات خارجية.

الميزة الفريدة: **مُحدّث الكود القديم تلقائيًا إلى PHP 8.5** — يحوّل دوال `mysql_*` و`ereg` ومشغّلات PHP4 القديمة وسلوكيات الإصدارات المندثرة إلى ما يقابلها الحديث، مع تحقق نحوي عبر `php -l` ونسخ احتياطية ووضع تجفّب آمن.

**المزايا:** إشراف ذاتي على العمليات مع قاطع دائرة يمنع التكرار اللانهائي · استرداد ذكي لتعارض المنافذ · ضبط تلقائي للتهيئة حسب العتاد · قياسات حية للمعالج والذاكرة وفاحص للاتصالات الواردة · نسخ احتياطية مجدولة مضغوطة · مدقّق أمني مع تحصين بنقرة · معالج ترحيل من XAMPP · تسجيل كخدمات ويندوز أصلية · واجهة ثنائية اللغة بالكامل (عربي/إنجليزي).

**التثبيت:** حمّل النسخة المحمولة من [الإصدارات](https://github.com/DonAdnan2002/axamp/releases/latest) وشغّل `axamp-launcher.exe` مباشرة — لا يحتاج تثبيتًا.

**المبتكر:** عدنان سلطان حسن — [donadnan2002@gmail.com](mailto:donadnan2002@gmail.com) · رخصة MIT.

package secguard

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"axamp/internal/config"
	"axamp/internal/generator"
	"axamp/internal/profiler"
)

type Severity string

const (
	SeverityHigh   Severity = "HIGH"
	SeverityMedium Severity = "MEDIUM"
	SeverityLow    Severity = "LOW"
	SeverityPass   Severity = "PASS"
)

type SecurityFinding struct {
	ID             string   `json:"id"`
	Level          Severity `json:"level"`
	Category       string   `json:"category"`
	Title          string   `json:"title"`
	Description    string   `json:"description"`
	Recommendation string   `json:"recommendation"`
	AutoFixAction  string   `json:"auto_fix_action,omitempty"`
	TargetPath     string   `json:"target_path,omitempty"`
}

type SecurityReport struct {
	Score        int               `json:"score"`
	Grade        string            `json:"grade"`
	HighCount    int               `json:"high_count"`
	MediumCount  int               `json:"medium_count"`
	PassCount    int               `json:"pass_count"`
	Findings     []SecurityFinding `json:"findings"`
	SmartTips    []string          `json:"smart_tips"`
}

func AuditStack(cfg *config.AppConfig) SecurityReport {
	var findings []SecurityFinding
	score := 100

	// 1. Audit PHP Configuration (conf/php.ini) & Oct 2026 CVE Mitigations
	phpIniPath := filepath.Join(cfg.RootDir, "conf", "php.ini")
	phpContentBytes, _ := os.ReadFile(phpIniPath)
	phpContent := string(phpContentBytes)

	if strings.Contains(phpContent, "session.upload_progress.enabled = 0") &&
		strings.Contains(phpContent, "fatal_error_backtraces = 0") {
		findings = append(findings, SecurityFinding{
			ID:             "php-cve-2026",
			Level:          SeverityPass,
			Category:       "PHP Core Security",
			Title:          "تحصينات ثغرات PHP 2026 الحرجة مفعلة (CVE-2026-109618 / 109619)",
			Description:    "إعدادات session.upload_progress.enabled=0 و fatal_error_backtraces=0 مفعلة لحماية mod_php من هجمات RCE.",
			Recommendation: "استمر على إصدار PHP 8.5.12 أو أحدث.",
		})
	} else {
		score -= 25
		findings = append(findings, SecurityFinding{
			ID:             "php-cve-2026",
			Level:          SeverityHigh,
			Category:       "PHP Core Security",
			Title:          "تحصينات session.upload_progress غير مفعلة في php.ini",
			Description:    "قد يؤدي تفعيل session.upload_progress مع تبديل المسلسلات إلى ثغرة تنفيذ أوامر عن بعد (CVE-2026-109619).",
			Recommendation: "اضغط على زر الإصلاح التلقائي لتعطيلها في php.ini فوراً.",
			AutoFixAction:  "regenerate_configs",
		})
	}

	if strings.Contains(phpContent, "display_errors = On") || strings.Contains(phpContent, "expose_php = On") {
		score -= 10
		findings = append(findings, SecurityFinding{
			ID:             "php-dev-exposure",
			Level:          SeverityMedium,
			Category:       "PHP Configuration",
			Title:          "إظهار الأخطاء وهوية PHP مفعل حالياً (Development Mode)",
			Description:    "تفعيل display_errors=On و expose_php=On ممتاز أثناء البرمجة، لكنه يكشف مسارات النظام الداخلية ورقم إصدار PHP.",
			Recommendation: "عند اختبار الأداء أو مشاركة السيرفر عبر الشبكة، انتقل إلى نمط الإنتاجية (Production Mode).",
			AutoFixAction:  "switch_prod_mode",
		})
	} else {
		findings = append(findings, SecurityFinding{
			ID:             "php-prod-hardened",
			Level:          SeverityPass,
			Category:       "PHP Configuration",
			Title:          "إخفاء أخطاء وهوية PHP مفعل بأمان (Production Mode)",
			Description:    "الأخطاء مسجلة في الملفات الصامتة فقط ولا تظهر للمتصفح.",
			Recommendation: "الإعداد مثالي لبيئة الإنتاج والاختبار الآمن.",
		})
	}

	if !strings.Contains(phpContent, "disable_functions = exec,passthru,shell_exec,system,proc_open,popen") {
		score -= 10
		findings = append(findings, SecurityFinding{
			ID:             "php-dangerous-funcs",
			Level:          SeverityMedium,
			Category:       "PHP Hardening",
			Title:          "دوال تنفيذ أوامر النظام في PHP غير مقيدة (shell_exec / system)",
			Description:    "الدوال مثل exec و shell_exec و system متاحة للسكريبتات؛ إذا وُجدت ثغرة في أحد المشاريع قد تُستغل لتشغيل أوامر ويندوز.",
			Recommendation: "إذا لم تكن مشاريعك تحتاج لتشغيل أوامر الطرفية، يمكنك تقييدها بضغطة زر.",
			AutoFixAction:  "harden_php_functions",
		})
	} else {
		findings = append(findings, SecurityFinding{
			ID:             "php-dangerous-funcs",
			Level:          SeverityPass,
			Category:       "PHP Hardening",
			Title:          "دوال تنفيذ الأوامر الخطرة في PHP مقيدة (disable_functions)",
			Description:    "تم حظر exec, passthru, shell_exec, system, proc_open, popen بنجاح.",
			Recommendation: "حماية عالية ضد ثغرات حقن الأوامر (Command Injection).",
		})
	}

	// 2. Audit Apache Configuration (conf/httpd.conf)
	httpdPath := filepath.Join(cfg.RootDir, "conf", "httpd.conf")
	httpdBytes, _ := os.ReadFile(httpdPath)
	httpdContent := string(httpdBytes)

	if strings.Contains(httpdContent, "Options Indexes") {
		score -= 10
		findings = append(findings, SecurityFinding{
			ID:             "apache-dir-listing",
			Level:          SeverityMedium,
			Category:       "Apache Web Server",
			Title:          "استعراض محتويات المجلدات مفعل (Directory Indexing Enabled)",
			Description:    "يسمح الخادم حالياً بعرض قائمة الملفات في أي مجلد لا يحتوي على ملف index.php أو index.html.",
			Recommendation: "التحويل إلى نمط الإنتاجية يعطل Directory Indexing تلقائياً (-Indexes).",
			AutoFixAction:  "switch_prod_mode",
		})
	} else {
		findings = append(findings, SecurityFinding{
			ID:             "apache-dir-listing",
			Level:          SeverityPass,
			Category:       "Apache Web Server",
			Title:          "استعراض المجلدات العشوائي مغلق (-Indexes)",
			Description:    "خادم Apache يمنع كشف محتويات المجلدات التي لا تحتوي على ملف index.",
			Recommendation: "إعداد آمن.",
		})
	}

	// 3. Audit htdocs/ for Exposed Sensitive Files (.env, .git, .sql backups)
	htdocsDir := filepath.Join(cfg.RootDir, "htdocs")
	htaccessPath := filepath.Join(htdocsDir, ".htaccess")
	htaccessBytes, _ := os.ReadFile(htaccessPath)
	hasHtaccessProtection := strings.Contains(string(htaccessBytes), "AXAMP-SECGUARD")

	var exposedFiles []string
	_ = filepath.Walk(htdocsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		name := strings.ToLower(info.Name())
		if info.IsDir() && name == ".git" {
			rel, _ := filepath.Rel(htdocsDir, path)
			exposedFiles = append(exposedFiles, rel+"/")
			return filepath.SkipDir
		}
		if !info.IsDir() {
			if name == ".env" || strings.HasPrefix(name, ".env.") ||
				strings.HasSuffix(name, ".sql") || strings.HasSuffix(name, ".bak") ||
				name == "composer.lock" {
				rel, _ := filepath.Rel(htdocsDir, path)
				exposedFiles = append(exposedFiles, rel)
			}
		}
		return nil
	})

	if len(exposedFiles) > 0 && !hasHtaccessProtection {
		score -= 25
		findings = append(findings, SecurityFinding{
			ID:             "htdocs-sensitive-leak",
			Level:          SeverityHigh,
			Category:       "Project Secrets & Files",
			Title:          fmt.Sprintf("اكتشاف %d ملفات/مجلدات حساسة مكشوفة داخل htdocs", len(exposedFiles)),
			Description:    "تم العثور على ملفات حساسة قد تحتوي على كلمات مرور أو قواعد بيانات: " + strings.Join(exposedFiles, " , "),
			Recommendation: "اضغط على زر الحماية التلقائية لتوليد قواعد حظر شاملة في .htaccess تمنع تحميل ملفات .env و .git و .sql عبر المتصفح.",
			AutoFixAction:  "protect_htdocs_htaccess",
		})
	} else {
		findings = append(findings, SecurityFinding{
			ID:             "htdocs-sensitive-leak",
			Level:          SeverityPass,
			Category:       "Project Secrets & Files",
			Title:          "حماية الملفات الحساسة في htdocs (.env / .git / .sql)",
			Description:    "لم يتم رصد ملفات حساسة مكشوفة أو أنه تم تأمين مجلد htdocs بملف .htaccess واقٍ.",
			Recommendation: "تأكد دائماً من عدم رفع ملفات .env أو .sql إلى بيئة الاستضافة العامة.",
		})
	}

	// 4. Audit phpMyAdmin & MySQL Root Access
	pmaCfgPath := filepath.Join(cfg.RootDir, "tools", "phpmyadmin", "config.inc.php")
	pmaBytes, _ := os.ReadFile(pmaCfgPath)
	pmaContent := string(pmaBytes)

	if strings.Contains(pmaContent, "['auth_type'] = 'config'") {
		if cfg.RemoteAccess {
			score -= 20
			findings = append(findings, SecurityFinding{
				ID:             "pma-autologin-lan",
				Level:          SeverityHigh,
				Category:       "Database Security",
				Title:          "الدخول التلقائي لـ phpMyAdmin مفعل مع فتح الوصول الشبكي!",
				Description:    "وضع auth_type='config' يدخل مباشرة بحساب root؛ ومع تفعيل الوصول الشبكي قد يتمكن أي جهاز على الشبكة من فتح /phpmyadmin.",
				Recommendation: "حول phpMyAdmin إلى وضع المطالبة بكلمة المرور (cookie auth) بضغطة زر.",
				AutoFixAction:  "secure_phpmyadmin_cookie",
			})
		} else {
			score -= 5
			findings = append(findings, SecurityFinding{
				ID:             "pma-autologin-local",
				Level:          SeverityLow,
				Category:       "Database Security",
				Title:          "الدخول التلقائي السريع مفعل في phpMyAdmin (Localhost Only)",
				Description:    "تم ضبط phpMyAdmin للدخول الفوري بحساب root لتسهيل التطوير المحلي، ومحصور في جهازك فقط.",
				Recommendation: "إذا رغبت في تفعيل شاشة تسجيل الدخول التقليدية (Cookie Login)، اضغط زر التبديل.",
				AutoFixAction:  "secure_phpmyadmin_cookie",
			})
		}
	} else {
		findings = append(findings, SecurityFinding{
			ID:             "pma-cookie-auth",
			Level:          SeverityPass,
			Category:       "Database Security",
			Title:          "بوابة phpMyAdmin محمية بشاشة تسجيل الدخول (Cookie Auth)",
			Description:    "يطالب phpMyAdmin باسم المستخدم وكلمة المرور قبل السماح باستعراض قواعد البيانات.",
			Recommendation: "إعداد آمن.",
		})
	}

	if score < 0 {
		score = 0
	}

	grade := "A+ (ممتاز)"
	switch {
	case score < 55:
		grade = "D (يحتاج تحصين عاجل)"
	case score < 75:
		grade = "C (متوسط - وضع تطوير مفتوح)"
	case score < 90:
		grade = "B+ (جيد جداً)"
	}

	var highC, medC, passC int
	for _, f := range findings {
		switch f.Level {
		case SeverityHigh:
			highC++
		case SeverityMedium, SeverityLow:
			medC++
		case SeverityPass:
			passC++
		}
	}

	tips := []string{
		"💡 نصيحة أمنية (PHP 8.5.12): استخدم دائماً Prepared Statements في PDO أو MySQLi لمنع ثغرات SQL Injection نهائياً.",
		"💡 نصيحة أداء: عند الانتهاء من برمجة مشروعك، جرب التبديل إلى نمط الإنتاجية (Prod Mode) في الشريط الجانبي لاختبار سرعة OPcache الحقيقية.",
		"💡 نصيحة قواعد البيانات: استخدم زر 'نسخ احتياطي مضغوط للكل (.zip)' قبل إجراء أي تعديلات هيكلية كبيرة (ALTER/DROP) على الجداول.",
		"💡 نصيحة العزل المحمول: قام AXAMP بعزل متغير PATH و OPENSSL_CONF تلقائياً لضمان عدم تداخل مكتبات DLL مع أي نسخ XAMPP قديمة في جهازك.",
	}

	return SecurityReport{
		Score:       score,
		Grade:       grade,
		HighCount:   highC,
		MediumCount: medC,
		PassCount:   passC,
		Findings:    findings,
		SmartTips:   tips,
	}
}

func ApplyAutoFix(cfg *config.AppConfig, action string) error {
	hw := profiler.InspectSystem()

	switch action {
	case "regenerate_configs":
		return generator.GenerateAll(cfg, hw)

	case "switch_prod_mode":
		cfg.Mode = config.ProdMode
		_ = cfg.Save()
		return generator.GenerateAll(cfg, hw)

	case "harden_php_functions":
		phpIniPath := filepath.Join(cfg.RootDir, "conf", "php.ini")
		data, err := os.ReadFile(phpIniPath)
		if err != nil {
			return err
		}
		content := string(data)
		if !strings.Contains(content, "disable_functions =") {
			content += "\n; AXAMP Security Guardian Hardening\ndisable_functions = exec,passthru,shell_exec,system,proc_open,popen\n"
			return os.WriteFile(phpIniPath, []byte(content), 0644)
		}
		return nil

	case "protect_htdocs_htaccess":
		htaccessPath := filepath.Join(cfg.RootDir, "htdocs", ".htaccess")
		existing, _ := os.ReadFile(htaccessPath)
		if strings.Contains(string(existing), "AXAMP-SECGUARD") {
			return nil
		}
		rule := `
# BEGIN AXAMP-SECGUARD
<FilesMatch "^(\.env.*|composer\.(json|lock)|.*\.(sql|bak|log|ini|sh))$">
    Require all denied
</FilesMatch>
RedirectMatch 403 /\..*$
# END AXAMP-SECGUARD
`
		return os.WriteFile(htaccessPath, append(existing, []byte(rule)...), 0644)

	case "secure_phpmyadmin_cookie":
		pmaCfgPath := filepath.Join(cfg.RootDir, "tools", "phpmyadmin", "config.inc.php")
		data, err := os.ReadFile(pmaCfgPath)
		if err != nil {
			return err
		}
		updated := strings.ReplaceAll(string(data), "$cfg['Servers'][$i]['auth_type'] = 'config';", "$cfg['Servers'][$i]['auth_type'] = 'cookie';")
		return os.WriteFile(pmaCfgPath, []byte(updated), 0644)

	default:
		return fmt.Errorf("إجراء الإصلاح التلقائي غير معروف: %s", action)
	}
}

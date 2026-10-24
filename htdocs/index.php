<?php
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
</html>
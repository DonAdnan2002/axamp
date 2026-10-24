<?php
$host = '127.0.0.1';
$port = '3306';
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
<title>AXAMP SQL Studio (Adminer Lite)</title>
<style>
body { font-family: system-ui, sans-serif; background: #0f172a; color: #f8fafc; margin: 0; display: flex; height: 100vh; }
.sidebar { width: 250px; background: #1e293b; padding: 20px; border-left: 1px solid #334155; overflow-y: auto; }
.main { flex: 1; padding: 24px; overflow-y: auto; }
a { color: #38bdf8; text-decoration: none; display: block; padding: 8px; border-radius: 6px; }
a:hover, a.active { background: #334155; }
textarea { width: 100%; height: 110px; background: #0f172a; color: #38bdf8; border: 1px solid #334155; border-radius: 8px; padding: 12px; font-family: monospace; }
button { background: #0284c7; color: #fff; border: none; padding: 10px 20px; border-radius: 8px; cursor: pointer; font-weight: bold; margin-top: 8px; }
table { width: 100%; border-collapse: collapse; margin-top: 20px; background: #1e293b; }
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
</html>
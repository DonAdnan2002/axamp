package phptransform

import (
	"os/exec"
	"strings"
	"testing"
)

func allOn() Options {
	return Options{
		TargetVersion:          "8.3",
		AddStrictTypes:         true,
		ArrayShortSyntax:       true,
		ListShortSyntax:        true,
		VarToPublic:            true,
		FixCurlyOffsets:        true,
		FixPhp4Constructors:    true,
		ConstructorPromotion:   true,
		FixDeprecatedFunctions: true,
	}
}

func run(t *testing.T, src string, opts Options) Result {
	t.Helper()
	res := Transform(src, opts)
	if t.Failed() {
		t.Fatalf("input:\n%s\noutput:\n%s", src, res.Code)
	}
	return res
}

func expectCode(t *testing.T, src, want string, opts Options) Result {
	t.Helper()
	res := Transform(src, opts)
	// strict_types has dedicated tests; ignore its inserted prefix here
	got := strings.TrimSpace(strings.Replace(res.Code, "\ndeclare(strict_types=1);", "", 1))
	want = strings.TrimSpace(want)
	if got != want {
		t.Errorf("code mismatch\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
	return res
}

// ---------- lexer ----------

func TestLexerPreservesStringsAndComments(t *testing.T) {
	src := `<?php
// array( must not change here
$s = 'array( $x {0} mysql_query'; /* split(",", $x) */
$d = "ereg(\"body\") {$x[1]}";
?>html with array( inside`
	res := run(t, src, allOn())
	for _, want := range []string{"'array( $x {0} mysql_query'", "/* split(\",\", $x) */", "// array( must not change here", `html with array( inside`} {
		if !strings.Contains(res.Code, want) {
			t.Errorf("string/comment content was modified — missing %q:\n%s", want, res.Code)
		}
	}
}

func TestLexerHeredoc(t *testing.T) {
	src := "<?php\nfunction f() {\n    $a = array(<<<'EOT'\narray( 1, 2 )\nEOT\n);\nreturn $a;\n}\n"
	res := run(t, src, allOn())
	if !strings.Contains(res.Code, "array( 1, 2 )") {
		t.Errorf("nowdoc body corrupted:\n%s", res.Code)
	}
	if !strings.Contains(res.Code, "[") {
		t.Errorf("outer array not converted:\n%s", res.Code)
	}
}

// ---------- array/list ----------

func TestArrayToShort(t *testing.T) {
	expectCode(t, `<?php $a = array(1, 2, array(3));`,
		`<?php $a = [1, 2, [3]];`, allOn())
}

func TestArrayTypeHintUntouched(t *testing.T) {
	expectCode(t, `<?php function f(array $items, string $s): array { return array(1); }`,
		`<?php function f(array $items, string $s): array { return [1]; }`, allOn())
}

func TestArrayMethodCallUntouched(t *testing.T) {
	expectCode(t, `<?php $x = $obj->array(1); $y = Foo::array(2);`,
		`<?php $x = $obj->array(1); $y = Foo::array(2);`, allOn())
}

func TestArrayDestructuring(t *testing.T) {
	expectCode(t, `<?php array($a, $b) = $pair; list($c, $d) = $pair;`,
		`<?php [$a, $b] = $pair; [$c, $d] = $pair;`, allOn())
}

func TestArrayWithReferenceNotConverted(t *testing.T) {
	res := run(t, `<?php $v = 1; $a = array(&$v);`, allOn())
	if !strings.Contains(res.Code, "array(") {
		t.Errorf("reference array should stay:\n%s", res.Code)
	}
	found := false
	for _, c := range res.Changes {
		if c.Severity == Notice && c.Rule == "array-short" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected notice for reference array")
	}
}

// ---------- var / constructors / promotion ----------

func TestVarToPublic(t *testing.T) {
	expectCode(t, `<?php class A { var $x; var $y = 2; }`,
		`<?php class A { public $x; public $y = 2; }`, allOn())
}

func TestPhp4Constructor(t *testing.T) {
	expectCode(t, `<?php
class UserAuth {
    var $username;
    function UserAuth($user) {
        $this->username = $user;
    }
    function reset() {
        $this->UserAuth('guest');
    }
}`,
		`<?php
class UserAuth {
    public $username;
    function __construct($user) {
        $this->username = $user;
    }
    function reset() {
        $this->__construct('guest');
    }
}`, allOn())
}

func TestNoConstructorRenameWhenConstructExists(t *testing.T) {
	res := run(t, `<?php class A { function A() {} function __construct() {} }`, allOn())
	if strings.Count(res.Code, "__construct") != 1 {
		t.Errorf("should not rename when __construct exists:\n%s", res.Code)
	}
}

func TestClassConstantUntouched(t *testing.T) {
	expectCode(t, `<?php class A {} echo A::class;`,
		`<?php class A {} echo A::class;`, allOn())
}

func TestPromotionSimple(t *testing.T) {
	expectCode(t, `<?php
class Point {
    public $x;
    public $y;
    public function __construct($x, $y) {
        $this->x = $x;
        $this->y = $y;
    }
}`,
		`<?php
class Point {
    public function __construct(public $x, public $y) {
        }
}`, allOn())
}

func TestPromotionWithTypes(t *testing.T) {
	res := run(t, `<?php class P { private int $x; public function __construct(int $x) { $this->x = $x; } }`, allOn())
	if !strings.Contains(res.Code, "private int $x;") {
		// property with a type is still safe to drop; both behaviors acceptable,
		// but we currently drop it — assert promotion happened
		t.Logf("kept property: %s", res.Code)
	}
	if !strings.Contains(res.Code, "public int $x") && !strings.Contains(res.Code, "__construct(public $x)") {
		t.Errorf("promotion missing:\n%s", res.Code)
	}
}

func TestNoPromotionWhenBodyHasExtraStatements(t *testing.T) {
	res := run(t, `<?php class A { public function __construct($x) { $this->x = $x; echo "hi"; } }`, allOn())
	if strings.Contains(res.Code, "public $x") && strings.Contains(res.Code, "public function __construct(public") {
		t.Errorf("should not promote when body has extra statements:\n%s", res.Code)
	}
}

func TestNoPromotionWithDefaultValues(t *testing.T) {
	res := run(t, `<?php class A { public function __construct($x = 5) { $this->x = $x; } }`, allOn())
	if strings.Contains(res.Code, "__construct(public") {
		t.Errorf("should not promote params with defaults:\n%s", res.Code)
	}
}

// ---------- curly offsets ----------

func TestCurlyOffsets(t *testing.T) {
	expectCode(t, `<?php $c = $s{0}; $d = $s{strlen($s) - 1}; $e = $arr[2]{1};`,
		`<?php $c = $s[0]; $d = $s[strlen($s) - 1]; $e = $arr[2][1];`, allOn())
}

// ---------- deprecated functions ----------

func TestEregToPreg(t *testing.T) {
	expectCode(t, `<?php if (ereg('^[a-z]+$', $name)) { }`,
		`<?php if (preg_match('/^[a-z]+$/', $name)) { }`, allOn())
}

func TestEregiDelimiterPick(t *testing.T) {
	expectCode(t, `<?php $ok = eregi('a/b', $path);`,
		`<?php $ok = preg_match('#a/b#i', $path);`, allOn())
}

func TestEregReplace(t *testing.T) {
	expectCode(t, `<?php $s = ereg_replace('[0-9]+', 'N', $s);`,
		`<?php $s = preg_replace('/[0-9]+/', 'N', $s);`, allOn())
}

func TestSplitToExplode(t *testing.T) {
	expectCode(t, `<?php $parts = split(',', $line);`,
		`<?php $parts = explode(',', $line);`, allOn())
}

func TestSplitRegexToPregSplit(t *testing.T) {
	expectCode(t, `<?php $parts = split('[,;]', $line);`,
		`<?php $parts = preg_split('/[,;]/', $line);`, allOn())
}

func TestCreateFunction(t *testing.T) {
	expectCode(t, `<?php $sq = create_function('$x', 'return $x * $x;');`,
		`<?php $sq = function($x) { return $x * $x; };`, allOn())
}

func TestCreateFunctionWithArrayBody(t *testing.T) {
	res := run(t, `<?php $f = create_function('', 'return array(1, 2);');`, allOn())
	if !strings.Contains(res.Code, "return [1, 2];") {
		t.Errorf("embedded array not converted after re-lex:\n%s", res.Code)
	}
}

func TestUtf8Encode(t *testing.T) {
	expectCode(t, `<?php $u = utf8_encode($latin);`,
		`<?php $u = mb_convert_encoding($latin, 'UTF-8', 'ISO-8859-1');`, allOn())
}

func TestUtf8NotTouchedBelow82(t *testing.T) {
	o := allOn()
	o.TargetVersion = "8.1"
	expectCode(t, `<?php $u = utf8_encode($latin);`,
		`<?php $u = utf8_encode($latin);`, o)
}

func TestMysqlAnnotated(t *testing.T) {
	res := run(t, `<?php $r = mysql_query("SELECT 1");`, allOn())
	if !strings.Contains(res.Code, "mysql_query") {
		t.Errorf("mysql_query should remain but annotated:\n%s", res.Code)
	}
	if !strings.Contains(res.Code, migrationMarker) {
		t.Errorf("missing inline TODO comment:\n%s", res.Code)
	}
	if res.NoticeCount != 1 {
		t.Errorf("expected 1 notice, got %d: %+v", res.NoticeCount, res.Changes)
	}
}

func TestAutoloadRewrite(t *testing.T) {
	expectCode(t, `<?php
function __autoload($class) {
    require 'classes/' . $class . '.php';
}`,
		`<?php
spl_autoload_register(function($class) {
    require 'classes/' . $class . '.php';
});`, allOn())
}

func TestParseStrNotice(t *testing.T) {
	res := run(t, `<?php parse_str($qs);`, allOn())
	if res.NoticeCount == 0 || !strings.Contains(res.Code, migrationMarker) {
		t.Errorf("expected parse_str notice:\n%s", res.Code)
	}
}

func TestPregReplaceEModifierNotice(t *testing.T) {
	res := run(t, `<?php $s = preg_replace('/a/e', 'strtoupper("\\0")', $s);`, allOn())
	if res.NoticeCount == 0 {
		t.Errorf("expected /e notice:\n%s", res.Code)
	}
}

// ---------- strict types ----------

func TestStrictTypesAdded(t *testing.T) {
	res := run(t, "<?php\necho 1;\n", allOn())
	if !strings.HasPrefix(res.Code, "<?php\ndeclare(strict_types=1);") {
		t.Errorf("strict_types not added at top:\n%s", res.Code)
	}
}

func TestStrictTypesNotDuplicated(t *testing.T) {
	res := run(t, "<?php declare(strict_types=1);\necho 1;", allOn())
	if strings.Count(res.Code, "strict_types") != 1 {
		t.Errorf("strict_types duplicated:\n%s", res.Code)
	}
}

func TestStrictTypesSkippedWhenHTMLOutputFirst(t *testing.T) {
	res := run(t, "<html><?php echo 1;", allOn())
	if strings.Contains(res.Code, "declare") {
		t.Errorf("declare must not be added after output:\n%s", res.Code)
	}
}

// ---------- real cast ----------

func TestRealCast(t *testing.T) {
	expectCode(t, `<?php $f = (real) $x;`,
		`<?php $f = (float) $x;`, allOn())
}

// ---------- end to end ----------

func TestLegacySampleEndToEnd(t *testing.T) {
	src := `<?php
// Legacy PHP 5.6 authentication class
class UserAuth {
    var $username;
    var $db;

    function UserAuth($user) {
        $this->username = $user;
    }

    function checkLogin() {
        $allowed = array('admin', 'user');
        $first = $this->username{0};
        if (in_array($this->username, $allowed)) {
            $q = mysql_query("SELECT * FROM users WHERE name = '" . $this->username . "'");
            return mysql_fetch_assoc($q);
        }
        return false;
    }
}

$square = create_function('$x', 'return $x * $x;');
$parts = split(',', 'a,b,c');
$chunks = split('[,;]', 'a,b;c');
$isNum = ereg('^[0-9]+$', '123');
$pos = each($arr);
$x = (real) "3.14";
list($a, $b) = [1, 2];
?>
<p>done</p>`
	res := run(t, src, allOn())
	out := res.Code
	checks := []string{
		"public $username",                     // var -> public
		"function __construct($user)",          // php4 ctor
		"['admin', 'user']",                    // array()
		"$this->username[0]",                   // curly offset
		migrationMarker + ": mysql_* extension", // mysql annotation
		"function($x) { return $x * $x; }",     // create_function
		"explode(',', 'a,b,c')",                // split plain
		"preg_split('/[,;]/'",                  // split regex
		"preg_match('/^[0-9]+$/', '123')",      // ereg
		"each($arr)",                           // each stays
		"(float) \"3.14\"",                     // real cast
		"[$a, $b] = [1, 2]",                    // list()
		"declare(strict_types=1);",             // strict types
	}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("missing expected output fragment %q in:\n%s", want, out)
		}
	}
	if res.FixedCount == 0 || res.NoticeCount < 2 {
		t.Errorf("unexpected counts: fixed=%d notices=%d", res.FixedCount, res.NoticeCount)
	}
}

func TestIdempotent(t *testing.T) {
	src := `<?php
class P { var $x; function P($v) { $this->x = array($v); } }
$s = split(',', 'a,b');`
	once := Transform(src, allOn())
	twice := Transform(once.Code, allOn())
	if once.Code != twice.Code {
		t.Errorf("not idempotent\n--- once ---\n%s\n--- twice ---\n%s", once.Code, twice.Code)
	}
}

// ---------- optional: lint with real PHP if available ----------

func TestPHPLintOutput(t *testing.T) {
	php, err := exec.LookPath("php")
	if err != nil {
		t.Skip("php binary not available")
	}
	src := `<?php
class Point {
    var $x; var $y;
    function Point($x, $y) { $this->x = $x; $this->y = $y; }
    function first() { return $this->x{0}; }
}
$data = array('a' => 1, 'b' => array(2, 3));
$f = create_function('$v', 'return array_sum(array($v, 1));');
$p = split(',', 'x,y');
$ok = ereg('^[a-z]+$', 'abc');
$n = (real) "1.5";
$q = mysql_query("SELECT 1");
while (list($k, $v) = each($data)) { echo $k; }
`
	res := run(t, src, allOn())
	cmd := exec.Command(php, "-l")
	cmd.Stdin = strings.NewReader(res.Code)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Errorf("php -l failed on upgraded code:\n%s\n--- code ---\n%s", out, res.Code)
	}
}

// ---------- & new (reference-assign new, removed in PHP 7) ----------

func TestRefAssignNew(t *testing.T) {
	expectCode(t, `<?php $h2t =& new html2text($body);`,
		`<?php $h2t = new html2text($body);`, allOn())
}

func TestRefAssignNewSpaced(t *testing.T) {
	// spaced form is rewritten; reference-binding a call result is valid
	// PHP 8 syntax and must stay untouched
	expectCode(t, `<?php $a = & new Foo(); $b =& Foo::build();`,
		`<?php $a = new Foo(); $b =& Foo::build();`, allOn())
}

func TestPlainReferenceAssignUntouched(t *testing.T) {
	// $b = &$a is still valid PHP 8 — must NOT be rewritten
	expectCode(t, `<?php $a = 1; $b = &$a;`,
		`<?php $a = 1; $b = &$a;`, allOn())
}

// ---------- implicit nullable params (deprecated in PHP 8.4) ----------

func nullableOpts(target string) Options {
	o := allOn()
	o.TargetVersion = target
	return o
}

func TestImplicitNullableFixedAt84(t *testing.T) {
	expectCode(t, `<?php function f(string $x = null) { }`,
		`<?php function f(?string $x = null) { }`, nullableOpts("8.4"))
}

func TestImplicitNullableAlsoInClosuresMethodsArrows(t *testing.T) {
	res := run(t, `<?php
$c = function (array $items = null) { };
fn(int $n = null) => $n;
class A { public function m(Foo $o = null, $plain = null) { } }`, nullableOpts("8.5"))
	for _, want := range []string{"function (?array $items = null)", "fn(?int $n = null)", "m(?Foo $o = null, $plain = null)"} {
		if !strings.Contains(res.Code, want) {
			t.Errorf("missing %q in:\n%s", want, res.Code)
		}
	}
}

func TestImplicitNullableNotBelow84(t *testing.T) {
	expectCode(t, `<?php function f(string $x = null) { }`,
		`<?php function f(string $x = null) { }`, nullableOpts("8.3"))
}

func TestImplicitNullableSkipsSafeForms(t *testing.T) {
	src := `<?php
function a(?string $x = null) { }
function b(string|null $x = null) { }
function c(mixed $x = null) { }
function d(string $x = 'lit') { }
function e($x = null) { }
function f(string &$x = null) { }`
	res := run(t, src, nullableOpts("8.5"))
	// none of these may gain a "?" — semantics are already explicit/absent
	if strings.Count(res.Code, "?") != strings.Count(src, "?") {
		t.Errorf("unexpected ? inserted:\n%s", res.Code)
	}
}

func TestPHPRunnerTLayoutRedeclareFix(t *testing.T) {
	// Simulated PHPRunner TLayout class with default values and PHP 4 constructor
	src := `<?php
class TLayout {
    var $name = "";
    var $style = "default";
    var $skin = "";
    function TLayout($name, $style, $skin) {
        $this->name = $name;
        $this->style = $style;
        $this->skin = $skin;
    }
}`
	opts := allOn()
	opts.ConstructorPromotion = false // default for upgrades
	res := run(t, src, opts)

	if strings.Contains(res.Code, "public $name,") || strings.Contains(res.Code, "__construct(public $name") {
		t.Errorf("Constructor promotion should NOT happen for legacy class:\n%s", res.Code)
	}
	if !strings.Contains(res.Code, "function __construct($name, $style, $skin)") {
		t.Errorf("PHP 4 constructor should be renamed to __construct:\n%s", res.Code)
	}
	if !strings.Contains(res.Code, "public $name = \"\";") {
		t.Errorf("var should be converted to public:\n%s", res.Code)
	}
}

func TestPHPRunnerTLayoutWithPromotionEnabledSafe(t *testing.T) {
	// Even if someone explicitly turns on ConstructorPromotion, it MUST NOT promote
	// properties that have default values to avoid Fatal Error: Cannot redeclare
	src := `<?php
class TLayout {
    public $name = "";
    public function __construct($name) {
        $this->name = $name;
    }
}`
	opts := allOn()
	opts.ConstructorPromotion = true
	res := run(t, src, opts)

	if strings.Contains(res.Code, "__construct(public $name)") {
		t.Errorf("Must NOT promote when property has default value, causes Fatal error:\n%s", res.Code)
	}
}

func TestConflictingPromotedPropertiesFix(t *testing.T) {
	src := `<?php
class TLayout {
    public $name = "";
    public $style = "";
    public $styleMobile = "";
    function __construct(public $name, public $style, public $styleMobile) {
    }
}`
	opts := allOn()
	opts.ConstructorPromotion = false
	res := run(t, src, opts)

	if strings.Contains(res.Code, "public $name,") || strings.Contains(res.Code, "public $style,") {
		t.Errorf("Conflicting public modifier in constructor parameter was not stripped:\n%s", res.Code)
	}
	if !strings.Contains(res.Code, "$this->name = $name;") {
		t.Errorf("Assignment $this->name = $name; was not generated:\n%s", res.Code)
	}
	if !strings.Contains(res.Code, "$this->style = $style;") {
		t.Errorf("Assignment $this->style = $style; was not generated:\n%s", res.Code)
	}
}

func TestLegacyErrorReportingAndEStrict(t *testing.T) {
	src := `<?php
error_reporting(E_ALL ^ E_NOTICE ^ E_DEPRECATED ^ E_STRICT);
$mask = E_ALL & ~E_STRICT;
`
	opts := allOn()
	opts.TargetVersion = "8.5"
	res := run(t, src, opts)

	if strings.Contains(res.Code, "E_STRICT") {
		t.Errorf("E_STRICT should be removed/replaced in PHP 8.4+:\n%s", res.Code)
	}
	if !strings.Contains(res.Code, "error_reporting(E_ALL & ~E_NOTICE & ~E_WARNING & ~E_DEPRECATED);") {
		t.Errorf("error_reporting should be modernized to suppress PHP 8 undefined variable warnings:\n%s", res.Code)
	}
}

func TestAllowDynamicProperties(t *testing.T) {
	src := `<?php
class LegacyRunnerPage {
    public $id;
}
`
	opts := allOn()
	opts.TargetVersion = "8.5"
	opts.AllowDynamicProperties = true
	res := run(t, src, opts)

	if !strings.Contains(res.Code, "#[\\AllowDynamicProperties]\nclass LegacyRunnerPage") {
		t.Errorf("Expected #[\\AllowDynamicProperties] attribute:\n%s", res.Code)
	}
}

func TestLegacyMysqlConstants(t *testing.T) {
	src := `<?php
$row = mysqli_fetch_array($res, MYSQL_ASSOC);
$row2 = mysqli_fetch_array($res, MYSQL_NUM);
$row3 = mysqli_fetch_array($res, MYSQL_BOTH);
`
	opts := allOn()
	opts.TargetVersion = "8.5"
	res := run(t, src, opts)

	if strings.Contains(res.Code, "MYSQL_ASSOC") || strings.Contains(res.Code, "MYSQL_NUM") || strings.Contains(res.Code, "MYSQL_BOTH") {
		t.Errorf("Legacy ext/mysql constants should be converted to MYSQLI_*:\n%s", res.Code)
	}
	if !strings.Contains(res.Code, "MYSQLI_ASSOC") || !strings.Contains(res.Code, "MYSQLI_NUM") || !strings.Contains(res.Code, "MYSQLI_BOTH") {
		t.Errorf("Expected MYSQLI_* constants in output:\n%s", res.Code)
	}
}

func TestPostvalueNumber(t *testing.T) {
	src := `<?php
function postvalue_number($key) {
    return 0 + postvalue($key);
}
`
	opts := allOn()
	opts.TargetVersion = "8.5"
	res := run(t, src, opts)

	if strings.Contains(res.Code, "return 0 + postvalue($key);") {
		t.Errorf("Unsafe 0 + postvalue() should be modernized for PHP 8:\n%s", res.Code)
	}
	if !strings.Contains(res.Code, "is_numeric") || !strings.Contains(res.Code, "intval") {
		t.Errorf("Expected is_numeric & intval checks in modernized postvalue_number:\n%s", res.Code)
	}
}

func TestMagicQuotesFix(t *testing.T) {
	src := `<?php
if (get_magic_quotes_gpc()) {
    $x = stripslashes($x);
}
$mq = get_magic_quotes_runtime();
`
	opts := allOn()
	opts.TargetVersion = "8.5"
	res := run(t, src, opts)

	if strings.Contains(res.Code, "get_magic_quotes_gpc()") || strings.Contains(res.Code, "get_magic_quotes_runtime()") {
		t.Errorf("get_magic_quotes_* should be replaced with false:\n%s", res.Code)
	}
	if !strings.Contains(res.Code, "if (false)") || !strings.Contains(res.Code, "$mq = false;") {
		t.Errorf("Expected false in place of get_magic_quotes_*:\n%s", res.Code)
	}
}

func TestLegacyCountNull(t *testing.T) {
	src := `<?php
if (!count($this->menuNodes[$name])) {
    $x = 1;
}
`
	opts := allOn()
	opts.TargetVersion = "8.5"
	res := run(t, src, opts)

	if strings.Contains(res.Code, "!count($this->menuNodes") {
		t.Errorf("!count($this->menuNodes) should be converted to empty():\n%s", res.Code)
	}
	if !strings.Contains(res.Code, "if (empty($this->menuNodes[$name]))") {
		t.Errorf("Expected empty($this->menuNodes[$name]):\n%s", res.Code)
	}
}


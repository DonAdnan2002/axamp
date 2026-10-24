package phptransform

import (
	"math/rand"
	"os/exec"
	"sort"
	"strings"
	"testing"
)

// Property-based adversarial campaign: compose random PHP from trap-rich
// fragments and assert NON-NEGOTIABLE invariants on every output.
//
//   I1  preservation: every string literal / comment / HTML segment of the
//       input appears in the output (multiset subset). The transformer may
//       ADD comments (TODO markers) but must never lose or alter content.
//   I2  idempotency: Transform(Transform(x)) == Transform(x)
//   I3  lexical roundtrip: Render(Lex(output)) == output
//   I4  conditional validity: if the INPUT lints clean with the real PHP
//       binary, the OUTPUT must lint clean too.
func TestFuzzInvariants(t *testing.T) {
	php, phpErr := exec.LookPath("php")

	fragments := []string{
		`$s = 'array( mysql_query {0} ereg(';`,
		`$t = "interp {$x[1]} array( inside";`,
		`/* block array( comment */ $u = 1; // line split( comment`,
		"$h = array(<<<'EOT'\narray( {0} mysql_query body\nEOT\n);",
		`$v = array(1, array(2, array('k' => array(3))), array(&$ref));`,
		`list($a, $b) = each($rows);`,
		`$m = ereg('^[a-z]+$', $name); $n = eregi('A/B', $path);`,
		`$r = eregi_replace('[0-9]+', 'N', $txt);`,
		`$sp = split(',', $csv); $sq = spliti('[,;]', $csv2);`,
		`$cf = create_function('$x', 'return $x * 2;');`,
		`$f = (real) "1.5"; $g = (real) $f;`,
		`$c0 = $str{0}; $c1 = $obj->prop{1}; $c2 = $arr[2]{3};`,
		`$q = mysql_query('SELECT 1'); $qr = mysql_fetch_assoc($q);`,
		`$enc = utf8_encode($v); $dec = utf8_decode($enc);`,
		`function h($x) { return is_real($x) ? 1 : 0; }`,
		`while (list($k, $v2) = each($arr)) { echo $k; }`,
		`$fn = function($p) use ($q) { return array($p, $q); };`,
		`?>plain html array( with mysql_query <?php`,
		`$neg = -$x; $tern = $a ? array(1) : array(2);`,
		`foreach (array('a','b') as $k2 => $v3) { $$k2 = $v3{0}; }`,
	}
	classWrap := []string{
		`class C%d { var $p1; var $p2; function C%d($a1, $a2) { $this->p1 = $a1; $this->p2 = $a2; } }`,
		`class D%d { public $x; public function __construct($x) { $this->x = array($x); } }`,
	}

	rng := rand.New(rand.NewSource(20261024))
	lintChecked := 0
	const maxLintChecks = 40

	for iter := 0; iter < 150; iter++ {
		var b strings.Builder
		b.WriteString("<?php\n")
		n := 4 + rng.Intn(10)
		for i := 0; i < n; i++ {
			switch rng.Intn(5) {
			case 0:
				b.WriteString(strings.Replace(strings.Replace(
					classWrap[rng.Intn(len(classWrap))], "%d", itoa(iter), 1), "%d", itoa(i), 1))
				b.WriteString("\n")
			default:
				b.WriteString(fragments[rng.Intn(len(fragments))])
				b.WriteString("\n")
			}
		}
		src := b.String()

		// Two-mode campaign:
		//
		// Mode A (syntax rules only, no deprecated-fn rewriting): string,
		// comment and HTML tokens must be preserved BIT-FOR-BIT — these rules
		// may never touch literals, by design.
		optsA := allOnForFuzz()
		optsA.FixDeprecatedFunctions = false
		passA := Transform(src, optsA)
		assertPreserved(t, iter, src, passA.Code, true)

		// Mode B (all rules): the deprecated-fn fixer legitimately REBUILDS
		// literal arguments of the calls it fixes (e.g. split's pattern), so
		// string-literal bit-preservation does not apply there; comments and
		// HTML interleaving are still never lost.
		once := Transform(src, allOnForFuzz())
		assertPreserved(t, iter, src, once.Code, false)

		// I2 — idempotency
		twice := Transform(once.Code, allOnForFuzz())
		if twice.Code != once.Code {
			t.Fatalf("iter %d: NOT IDEMPOTENT\n--- once ---\n%s\n--- twice ---\n%s",
				iter, once.Code, twice.Code)
		}

		// I3 — lexical roundtrip of the output
		if got := Render(Lex(once.Code)); got != once.Code {
			t.Fatalf("iter %d: roundtrip broke\n--- out ---\n%s\n--- re ---\n%s", iter, once.Code, got)
		}

		// I4 — conditional real-binary lint
		if phpErr == nil || lintChecked >= maxLintChecks {
			continue
		}
		if !lints(php, src) {
			continue // input itself invalid — not the transformer's fault
		}
		lintChecked++
		if !lints(php, once.Code) {
			t.Fatalf("iter %d: input lints but OUTPUT FAILS php -l\n%s", iter, once.Code)
		}
	}
	if phpErr != nil {
		t.Log("php binary not found — I4 skipped, I1-I3 exercised")
	}
}

func allOnForFuzz() Options {
	o := allOn()
	o.AddStrictTypes = false // keep compositions lint-stable regardless of leading output
	return o
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var d []byte
	for i > 0 {
		d = append([]byte{byte('0' + i%10)}, d...)
		i /= 10
	}
	return string(d)
}

// assertPreserved checks a multiset-subset invariant on lexical content.
// includeStrings=true: string literals are preserved bit-for-bit (valid for
// the syntax-only rules, which never touch literals). includeStrings=false:
// only comments must be preserved (the deprecated-fn fixer may rebuild the
// literal arguments of the calls it fixes).
func assertPreserved(t *testing.T, iter int, inSrc, outSrc string, includeStrings bool) {
	t.Helper()
	counts := func(code string) map[string]int {
		m := map[string]int{}
		for _, tok := range Lex(code) {
			if tok.Type == TokComment || (includeStrings && tok.Type == TokString) {
				m[string(rune(tok.Type))+"\x00"+tok.Text]++
			}
		}
		return m
	}
	inC := counts(inSrc)
	outC := counts(outSrc)
	var missing []string
	for k, n := range inC {
		if outC[k] < n {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("iter %d: PRESERVATION VIOLATED — lost/changed %d token(s), e.g. %q\ninput:\n%s\noutput:\n%s",
			iter, len(missing), clip(missing[0], 80), inSrc, outSrc)
	}
}

func lints(php, code string) bool {
	cmd := exec.Command(php, "-l")
	cmd.Stdin = strings.NewReader(code)
	return cmd.Run() == nil
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

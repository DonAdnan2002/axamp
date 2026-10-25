package phptransform

import (
	"fmt"
	"strings"
)

// Options controls which upgrade rules run and the target PHP version.
type Options struct {
	TargetVersion          string // e.g. "8.3"
	AddStrictTypes         bool
	ArrayShortSyntax       bool // array(...) -> [...]
	ListShortSyntax        bool // list(...) -> [...]  (PHP 7.1+)
	VarToPublic            bool // var $x -> public $x
	FixCurlyOffsets        bool // $s{0} -> $s[0]  (fatal in PHP 8)
	FixPhp4Constructors    bool // function ClassName() -> __construct
	ConstructorPromotion   bool // PHP 8 promoted ctor properties
	FixDeprecatedFunctions bool // auto-fix mechanical calls + annotate the rest
	AllowDynamicProperties bool // add #[\AllowDynamicProperties] for PHP 8.2+
}

// Severity of a reported change.
type Severity string

const (
	Fixed  Severity = "fixed"  // code was automatically rewritten
	Notice Severity = "notice" // needs human review, annotated in code
)

// Change is a single entry of the migration audit log.
type Change struct {
	Rule     string   `json:"rule"`
	Line     int      `json:"line"`
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
}

// Result is the outcome of upgrading one source string.
type Result struct {
	Code        string   `json:"code"`
	Changes     []Change `json:"changes"`
	FixedCount  int      `json:"fixedCount"`
	NoticeCount int      `json:"noticeCount"`
}

type edit struct {
	start, end int
	text       string
}

// applyEdits replaces toks[e.start:e.end] with e.text for each edit.
// Edits must be sorted and non-overlapping; start==end inserts.
func applyEdits(toks []Token, edits []edit) []Token {
	if len(edits) == 0 {
		return toks
	}
	var out []Token
	prev := 0
	for _, e := range edits {
		out = append(out, toks[prev:e.start]...)
		if e.text != "" {
			out = append(out, Token{Type: TokOperator, Text: e.text, Line: toks[e.start].Line})
		}
		prev = e.end
	}
	return append(out, toks[prev:]...)
}

func nextSig(toks []Token, from int) int {
	for i := from + 1; i < len(toks); i++ {
		if t := toks[i]; t.Type != TokWhitespace && t.Type != TokComment {
			return i
		}
	}
	return -1
}

func prevSig(toks []Token, from int) int {
	for i := from - 1; i >= 0; i-- {
		if t := toks[i]; t.Type != TokWhitespace && t.Type != TokComment {
			return i
		}
	}
	return -1
}

func isOp(t Token, s string) bool { return t.Type == TokOperator && t.Text == s }

func isKw(t Token, s string) bool { return t.Type == TokIdent && strings.EqualFold(t.Text, s) }

// matchDelim returns the index of the delimiter matching toks[openIdx].
func matchDelim(toks []Token, openIdx int, open, close string) int {
	depth := 0
	for i := openIdx; i < len(toks); i++ {
		if t := toks[i]; t.Type == TokOperator {
			if t.Text == open {
				depth++
			} else if t.Text == close {
				depth--
				if depth == 0 {
					return i
				}
			}
		}
	}
	return -1
}

// topLevelCommas returns indexes of commas directly inside toks[openIdx]..close.
func topLevelCommas(toks []Token, openIdx, closeIdx int) []int {
	var out []int
	depth := 0
	for i := openIdx + 1; i < closeIdx; i++ {
		if t := toks[i]; t.Type == TokOperator {
			switch t.Text {
			case "(", "[", "{":
				depth++
			case ")", "]", "}":
				depth--
			case ",":
				if depth == 0 {
					out = append(out, i)
				}
			}
		}
	}
	return out
}

func versionAtLeast(target, min string) bool {
	var tmj, tmn, mmj, mmn int
	fmt.Sscanf(target, "%d.%d", &tmj, &tmn)
	fmt.Sscanf(min, "%d.%d", &mmj, &mmn)
	if tmj != mmj {
		return tmj > mmj
	}
	return tmn >= mmn
}

// Transform applies all enabled rules and returns the upgraded code plus an
// audit log of every change.
func Transform(src string, opts Options) Result {
	target := opts.TargetVersion
	if target == "" {
		target = "8.4"
	}
	opts.TargetVersion = target

	toks := Lex(src)
	rel := func(t []Token) []Token { return Lex(Render(t)) }
	var changes []Change
	add := func(rule string, line int, sev Severity, msg string, args ...interface{}) {
		changes = append(changes, Change{Rule: rule, Line: line, Severity: sev,
			Message: fmt.Sprintf(msg, args...)})
	}

	// Phase 1: removed/deprecated functions. Runs first on pristine source so
	// argument extraction sees original byte offsets, then the result is
	// re-lexed below so spliced code (e.g. create_function bodies) becomes
	// real tokens for the syntax rules.
	if opts.FixDeprecatedFunctions {
		toks = ruleDeprecated(toks, src, target, add)
		toks = ruleRealCast(toks, add)
		toks = ruleEStrictAndErrorReporting(toks, target, add)
		toks = ruleLegacyMysqlConstants(toks, target, add)
		toks = Lex(Render(toks))
	}

	// Phase 2: syntax modernization. Each rule re-lexes its output so the next
	// rule sees proper token types (spliced replacement text is TokOperator).
	if opts.VarToPublic {
		toks = rel(ruleVarToPublic(toks, add))
	}
	if opts.FixPhp4Constructors {
		toks = rel(rulePhp4Constructors(toks, add))
	}
	if opts.AllowDynamicProperties && versionAtLeast(target, "8.2") {
		toks = rel(ruleAllowDynamicProperties(toks, add))
	}
	if opts.FixDeprecatedFunctions && versionAtLeast(target, "8.4") {
		toks = rel(ruleImplicitNullable(toks, add))
	}
	if opts.ConstructorPromotion && versionAtLeast(target, "8.0") {
		toks = rel(rulePromotion(toks, add))
	}
	if versionAtLeast(target, "8.0") {
		toks = rel(ruleFixConflictingPromotedProperties(toks, add))
		toks = rel(rulePostvalueNumber(toks, add))
		toks = rel(ruleLegacyCountNull(toks, add))
	}
	if opts.FixCurlyOffsets {
		toks = rel(ruleCurlyOffsets(toks, add))
	}
	if opts.FixDeprecatedFunctions {
		toks = rel(ruleRefAssignNew(toks, add))
	}
	if opts.ArrayShortSyntax {
		toks = rel(ruleArrayShort(toks, add))
	}
	if opts.ListShortSyntax {
		toks = rel(ruleListShort(toks, add))
	}
	if opts.AddStrictTypes {
		toks = ruleStrictTypes(toks, add)
	}

	res := Result{Code: Render(toks), Changes: changes}
	for _, c := range changes {
		if c.Severity == Fixed {
			res.FixedCount++
		} else {
			res.NoticeCount++
		}
	}
	return res
}

// ---------- var $x -> public $x ----------

func ruleVarToPublic(toks []Token, add func(string, int, Severity, string, ...interface{})) []Token {
	var edits []edit
	for i := 0; i < len(toks); i++ {
		if !isKw(toks[i], "var") {
			continue
		}
		nx := nextSig(toks, i)
		if nx < 0 || toks[nx].Type != TokVariable {
			continue
		}
		edits = append(edits, edit{i, i + 1, "public"})
		add("var-to-public", toks[i].Line, Fixed, "Replaced 'var' property declaration with 'public'")
	}
	return applyEdits(toks, edits)
}

// ---------- array(...) -> [...] ----------

func ruleArrayShort(toks []Token, add func(string, int, Severity, string, ...interface{})) []Token {
	var edits []edit
	for i := 0; i < len(toks); i++ {
		if !isKw(toks[i], "array") {
			continue
		}
		nx := nextSig(toks, i)
		if nx < 0 || !isOp(toks[nx], "(") {
			continue // type hints like "array $x" are untouched
		}
		if px := prevSig(toks, i); px >= 0 && (isOp(toks[px], "->") || isOp(toks[px], "::") || isOp(toks[px], "?->")) {
			continue // $obj->array(...) method call
		}
		close := matchDelim(toks, nx, "(", ")")
		if close < 0 {
			continue
		}
		if hasTopLevelRef(toks, nx, close) {
			add("array-short", toks[i].Line, Notice,
				"array() literal contains a reference (&) which cannot be auto-converted to [] — manual migration required")
			continue
		}
		edits = append(edits, edit{i, i + 1, ""}, edit{nx, nx + 1, "["}, edit{close, close + 1, "]"})
		add("array-short", toks[i].Line, Fixed, "Converted array(...) literal to [...] short syntax")
	}
	sortEdits(edits)
	return applyEdits(toks, edits)
}

func hasTopLevelRef(toks []Token, openIdx, closeIdx int) bool {
	depth := 0
	for i := openIdx + 1; i < closeIdx; i++ {
		if t := toks[i]; t.Type == TokOperator {
			if t.Text == "(" || t.Text == "[" || t.Text == "{" {
				depth++
			} else if t.Text == ")" || t.Text == "]" || t.Text == "}" {
				depth--
			} else if t.Text == "&" && depth == 0 {
				return true
			}
		}
	}
	return false
}

// ---------- list(...) -> [...] ----------

func ruleListShort(toks []Token, add func(string, int, Severity, string, ...interface{})) []Token {
	var edits []edit
	for i := 0; i < len(toks); i++ {
		if !isKw(toks[i], "list") {
			continue
		}
		nx := nextSig(toks, i)
		if nx < 0 || !isOp(toks[nx], "(") {
			continue
		}
		if px := prevSig(toks, i); px >= 0 && (isOp(toks[px], "->") || isOp(toks[px], "::") || isOp(toks[px], "?->")) {
			continue
		}
		close := matchDelim(toks, nx, "(", ")")
		if close < 0 || nextSig(toks, nx) == close {
			continue // empty list() is invalid anyway
		}
		edits = append(edits, edit{i, i + 1, ""}, edit{nx, nx + 1, "["}, edit{close, close + 1, "]"})
		add("list-short", toks[i].Line, Fixed, "Converted list(...) destructuring to [...] syntax (PHP 7.1+)")
	}
	sortEdits(edits)
	return applyEdits(toks, edits)
}

// ---------- $s{0} -> $s[0] ----------

func ruleCurlyOffsets(toks []Token, add func(string, int, Severity, string, ...interface{})) []Token {
	var edits []edit
	for i := 0; i < len(toks); i++ {
		if !isOp(toks[i], "{") {
			continue
		}
		px := prevSig(toks, i)
		if px < 0 {
			continue
		}
		ok := false
		if toks[px].Type == TokVariable || isOp(toks[px], "]") {
			ok = true
			if toks[px].Type == TokVariable { // guard $$var{...} dynamic-variable ambiguity
				if pp := prevSig(toks, px); pp >= 0 && isOp(toks[pp], "$") {
					ok = false
				}
			}
		} else if toks[px].Type == TokIdent {
			// property/constant offset: $this->prop{0}, Foo::CONST{0}
			if pp := prevSig(toks, px); pp >= 0 &&
				(isOp(toks[pp], "->") || isOp(toks[pp], "?->") || isOp(toks[pp], "::")) {
				ok = true
			}
		}
		if !ok {
			continue
		}
		close := matchDelim(toks, i, "{", "}")
		if close < 0 {
			continue
		}
		edits = append(edits, edit{i, i + 1, "["}, edit{close, close + 1, "]"})
		add("curly-offset", toks[i].Line, Fixed, "Converted curly-brace string offset {$...} to [...] (removed in PHP 8.0)")
	}
	sortEdits(edits)
	return applyEdits(toks, edits)
}

// ---------- PHP 4 style constructors ----------

type classRange struct {
	name      string
	open, end int // token index of class body { and matching }
}

func findClasses(toks []Token) []classRange {
	var out []classRange
	for i := 0; i < len(toks); i++ {
		if !isKw(toks[i], "class") {
			continue
		}
		if px := prevSig(toks, i); px >= 0 &&
			(isOp(toks[px], "->") || isOp(toks[px], "::") || isOp(toks[px], "?->")) {
			continue // Foo::class constant
		}
		nx := nextSig(toks, i)
		if nx < 0 || toks[nx].Type != TokIdent {
			continue // anonymous class
		}
		name := toks[nx].Text
		br := -1
		for j := nx + 1; j < len(toks); j++ {
			t := toks[j]
			if isOp(t, "{") {
				br = j
				break
			}
			if t.Type == TokOperator || t.Type == TokString || t.Type == TokVariable || t.Type == TokNumber {
				break // extends Foo implements Bar, Baz { — only idents/commas allowed
			}
		}
		if br < 0 {
			continue
		}
		end := matchDelim(toks, br, "{", "}")
		if end < 0 {
			continue
		}
		out = append(out, classRange{name: name, open: br, end: end})
		i = end
	}
	return out
}

func rulePhp4Constructors(toks []Token, add func(string, int, Severity, string, ...interface{})) []Token {
	classes := findClasses(toks)
	var edits []edit
	for _, cl := range classes {
		hasCtor := false
		for j := cl.open + 1; j < cl.end && !hasCtor; j++ {
			if isKw(toks[j], "__construct") && nextSig(toks, j) > 0 && isOp(toks[nextSig(toks, j)], "(") {
				hasCtor = true
			}
		}
		if hasCtor {
			continue
		}
		for j := cl.open + 1; j < cl.end; j++ {
			if toks[j].Type != TokIdent || !strings.EqualFold(toks[j].Text, cl.name) {
				continue
			}
			nx := nextSig(toks, j)
			if nx < 0 || !isOp(toks[nx], "(") {
				continue
			}
			px := prevSig(toks, j)
			rename := false
			if px >= 0 && isKw(toks[px], "function") {
				rename = true // function ClassName() declaration
			} else if px >= 0 && (isOp(toks[px], "->") || isOp(toks[px], "?->")) {
				pp := prevSig(toks, px)
				rename = pp >= 0 && toks[pp].Type == TokVariable && toks[pp].Text == "$this"
			} else if px >= 0 && isOp(toks[px], "::") {
				pp := prevSig(toks, px)
				rename = pp >= 0 && isKw(toks[pp], "parent")
			}
			if rename {
				edits = append(edits, edit{j, j + 1, "__construct"})
				add("php4-constructor", toks[j].Line, Fixed,
					"Renamed PHP 4 style constructor %s() to __construct()", cl.name)
			}
		}
	}
	return applyEdits(toks, edits)
}

// ---------- constructor property promotion (PHP 8.0+) ----------

func rulePromotion(toks []Token, add func(string, int, Severity, string, ...interface{})) []Token {
	classes := findClasses(toks)
	var edits []edit
	for _, cl := range classes {
		e, ok := promoteClass(toks, cl)
		if ok {
			edits = append(edits, e...)
			add("ctor-promotion", toks[cl.open].Line, Fixed,
				"Applied constructor property promotion in class %s (PHP 8.0+)", cl.name)
		}
	}
	sortEdits(edits)
	return applyEdits(toks, edits)
}

// sortEdits orders edits by start index so applyEdits can splice them safely.
func sortEdits(edits []edit) {
	for i := 1; i < len(edits); i++ {
		for j := i; j > 0 && edits[j].start < edits[j-1].start; j-- {
			edits[j], edits[j-1] = edits[j-1], edits[j]
		}
	}
}

type promoParam struct {
	name    string
	declIdx int // index of first significant token of the parameter
}

func promoteClass(toks []Token, cl classRange) ([]edit, bool) {
	// find __construct at class top level
	var ctorIdx = -1
	for j := cl.open + 1; j < cl.end; j++ {
		if !isKw(toks[j], "__construct") {
			continue
		}
		px := prevSig(toks, j)
		if px >= 0 && isKw(toks[px], "function") && px > cl.open {
			ctorIdx = j
			break
		}
	}
	if ctorIdx < 0 {
		return nil, false
	}
	po := nextSig(toks, ctorIdx)
	if po < 0 || !isOp(toks[po], "(") {
		return nil, false
	}
	pc := matchDelim(toks, po, "(", ")")
	if pc < 0 {
		return nil, false
	}
	bo := nextSig(toks, pc)
	if bo < 0 || !isOp(toks[bo], "{") {
		return nil, false // abstract ctor or interface — no body
	}
	bc := matchDelim(toks, bo, "{", "}")
	if bc < 0 || bc > cl.end {
		return nil, false
	}

	// parse parameters: each must be [type tokens] $var — no defaults, no refs
	var params []promoParam
	commas := topLevelCommas(toks, po, pc)
	bounds := append([]int{po}, commas...)
	bounds = append(bounds, pc)
	for bi := 0; bi+1 < len(bounds); bi++ {
		start, end := bounds[bi]+1, bounds[bi+1]
		first := -1
		last := -1
		varCount := 0
		varIdx := -1
		bad := false
		for j := start; j < end; j++ {
			switch toks[j].Type {
			case TokWhitespace, TokComment:
			case TokIdent:
				if first < 0 {
					first = j
				}
				last = j
			case TokVariable:
				varCount++
				varIdx = j
				if first < 0 {
					first = j
				}
				last = j
			case TokOperator:
				if toks[j].Text == "?" || toks[j].Text == "|" || toks[j].Text == "\\" {
					last = j
				} else {
					bad = true // =, &, ..., readonly etc.
				}
			default:
				bad = true
			}
		}
		if bad || varCount != 1 || varIdx < 0 || last != varIdx {
			return nil, false
		}
		params = append(params, promoParam{name: toks[varIdx].Text, declIdx: first})
	}
	if len(params) == 0 {
		return nil, false
	}
	for _, p := range params {
		// already promoted / modifiers present -> bail out
		if toks[p.declIdx].Type == TokIdent {
			switch strings.ToLower(toks[p.declIdx].Text) {
			case "public", "protected", "private", "readonly":
				return nil, false
			}
		}
	}

	// body must be exactly a sequence of: $this->name = $name;
	assigned := map[string]bool{}
	var stmts [][2]int // [$this idx, `;` idx]
	i := bo + 1
	for i < bc {
		if toks[i].Type == TokWhitespace || toks[i].Type == TokComment {
			i++
			continue
		}
		if toks[i].Type != TokVariable || toks[i].Text != "$this" {
			return nil, false
		}
		a := nextSig(toks, i)
		if a < 0 || !isOp(toks[a], "->") {
			return nil, false
		}
		prop := nextSig(toks, a)
		if prop < 0 || toks[prop].Type != TokIdent {
			return nil, false
		}
		eq := nextSig(toks, prop)
		if eq < 0 || !isOp(toks[eq], "=") {
			return nil, false
		}
		v := nextSig(toks, eq)
		if v < 0 || toks[v].Type != TokVariable || toks[v].Text != "$"+toks[prop].Text {
			return nil, false
		}
		sc := nextSig(toks, v)
		if sc < 0 || !isOp(toks[sc], ";") {
			return nil, false
		}
		assigned[toks[prop].Text] = true
		stmts = append(stmts, [2]int{i, sc})
		i = sc + 1
	}
	if len(assigned) != len(params) {
		return nil, false
	}
	for _, p := range params {
		if !assigned[strings.TrimPrefix(p.name, "$")] {
			return nil, false
		}
	}

	// In PHP 8, constructor property promotion cannot co-exist with a property declaration
	// of the same name in the class body. Every promoted property MUST have a cleanly drop-able
	// declaration (public/protected/private $prop;) with NO default value.
	// If any promoted property has a default value or cannot be safely dropped, abort promotion
	// to prevent PHP 8 "Fatal error: Cannot redeclare Class::$prop".
	for _, p := range params {
		propName := strings.TrimPrefix(p.name, "$")
		canDrop := false
		for j := cl.open + 1; j < cl.end; j++ {
			if toks[j].Type != TokIdent {
				continue
			}
			vis := strings.ToLower(toks[j].Text)
			if vis != "public" && vis != "protected" && vis != "private" {
				continue
			}
			v := findPropertyVar(toks, j, cl.end)
			if v < 0 {
				continue
			}
			if strings.TrimPrefix(toks[v].Text, "$") == propName {
				sc := nextSig(toks, v)
				if sc >= 0 && isOp(toks[sc], ";") {
					canDrop = true
					break
				}
			}
		}
		if !canDrop {
			return nil, false
		}
	}

	var edits []edit
	promotedNames := map[string]bool{}
	for _, p := range params {
		edits = append(edits, edit{p.declIdx, p.declIdx, "public "})
		promotedNames[strings.TrimPrefix(p.name, "$")] = true
	}
	for _, st := range stmts {
		end := st[1] + 1
		// swallow all whitespace after ';' — every following statement is
		// removed too, so their separator whitespace goes with it
		for end < len(toks) && toks[end].Type == TokWhitespace {
			end++
		}
		edits = append(edits, edit{st[0], end, ""})
	}
	// drop now-redundant property declarations (public $x; with no default)
	for j := cl.open + 1; j < cl.end; j++ {
		if toks[j].Type != TokIdent {
			continue
		}
		vis := strings.ToLower(toks[j].Text)
		if vis != "public" && vis != "protected" && vis != "private" {
			continue
		}
		v := findPropertyVar(toks, j, cl.end)
		if v < 0 || !promotedNames[strings.TrimPrefix(toks[v].Text, "$")] {
			continue
		}
		sc := nextSig(toks, v)
		if sc < 0 || !isOp(toks[sc], ";") {
			continue // has a default value — keep the declaration
		}
		end := sc + 1
		for end < len(toks) && toks[end].Type == TokWhitespace {
			end++
		}
		edits = append(edits, edit{j, end, ""})
	}
	return edits, true
}

func findPropertyVar(toks []Token, visIdx, maxIdx int) int {
	for k := visIdx + 1; k < maxIdx; k++ {
		if toks[k].Type == TokWhitespace || toks[k].Type == TokComment {
			continue
		}
		if toks[k].Type == TokVariable {
			return k
		}
		if isOp(toks[k], ";") || isOp(toks[k], "{") || isOp(toks[k], "}") || isOp(toks[k], "(") {
			break
		}
	}
	return -1
}

// ---------- fix conflicting promoted properties (PHP 8.0+) ----------
// In PHP 8+, a constructor cannot promote a property that is already declared in the class
// (Fatal error: Cannot redeclare Class::$prop). If a legacy file has both, or was previously
// partially promoted, this rule strips the promotion from the constructor and ensures the assignment
// statement exists in the constructor body.

func ruleFixConflictingPromotedProperties(toks []Token, add func(string, int, Severity, string, ...interface{})) []Token {
	classes := findClasses(toks)
	var edits []edit

	for _, cl := range classes {
		declaredProps := map[string]bool{}
		braceDepth := 0
		parenDepth := 0
		for j := cl.open + 1; j < cl.end; j++ {
			if isOp(toks[j], "{") {
				braceDepth++
				continue
			}
			if isOp(toks[j], "}") {
				braceDepth--
				continue
			}
			if isOp(toks[j], "(") {
				parenDepth++
				continue
			}
			if isOp(toks[j], ")") {
				parenDepth--
				continue
			}
			if braceDepth != 0 || parenDepth != 0 {
				continue
			}

			if toks[j].Type != TokIdent {
				continue
			}
			vis := strings.ToLower(toks[j].Text)
			if vis != "var" && vis != "public" && vis != "protected" && vis != "private" {
				continue
			}
			nx := nextSig(toks, j)
			if nx >= 0 && isKw(toks[nx], "function") {
				continue
			}
			v := findPropertyVar(toks, j, cl.end)
			if v >= 0 {
				propName := strings.TrimPrefix(toks[v].Text, "$")
				declaredProps[propName] = true
			}
		}

		if len(declaredProps) == 0 {
			continue
		}

		var ctorIdx = -1
		for j := cl.open + 1; j < cl.end; j++ {
			if !isKw(toks[j], "__construct") {
				continue
			}
			px := prevSig(toks, j)
			if px >= 0 && isKw(toks[px], "function") && px > cl.open {
				ctorIdx = j
				break
			}
		}
		if ctorIdx < 0 {
			continue
		}

		po := nextSig(toks, ctorIdx)
		if po < 0 || !isOp(toks[po], "(") {
			continue
		}
		pc := matchDelim(toks, po, "(", ")")
		if pc < 0 {
			continue
		}
		bo := nextSig(toks, pc)
		if bo < 0 || !isOp(toks[bo], "{") {
			continue
		}
		bc := matchDelim(toks, bo, "{", "}")
		if bc < 0 || bc > cl.end {
			continue
		}

		commas := topLevelCommas(toks, po, pc)
		bounds := append([]int{po}, commas...)
		bounds = append(bounds, pc)

		var assignmentsToInsert []string

		for bi := 0; bi+1 < len(bounds); bi++ {
			start, end := bounds[bi] + 1, bounds[bi+1]
			var visIdx = -1
			var varIdx = -1
			for k := start; k < end; k++ {
				if toks[k].Type == TokWhitespace || toks[k].Type == TokComment {
					continue
				}
				if toks[k].Type == TokIdent && visIdx < 0 {
					w := strings.ToLower(toks[k].Text)
					if w == "public" || w == "protected" || w == "private" || w == "readonly" {
						visIdx = k
						continue
					}
				}
				if toks[k].Type == TokVariable && varIdx < 0 {
					varIdx = k
				}
			}

			if visIdx >= 0 && varIdx >= 0 {
				paramName := strings.TrimPrefix(toks[varIdx].Text, "$")
				if declaredProps[paramName] {
					endVis := visIdx + 1
					for endVis < varIdx && (toks[endVis].Type == TokWhitespace || toks[endVis].Type == TokComment) {
						endVis++
					}
					edits = append(edits, edit{visIdx, endVis, ""})
					add("conflict-property-promotion", toks[varIdx].Line, Fixed,
						"Demoted conflicting promoted constructor parameter '$%s' in class %s (already declared in class body)",
						paramName, cl.name)

					hasAssign := false
					for k := bo + 1; k < bc-2; k++ {
						if toks[k].Type == TokVariable && toks[k].Text == "$this" {
							arrow := nextSig(toks, k)
							if arrow >= 0 && isOp(toks[arrow], "->") {
								prop := nextSig(toks, arrow)
								if prop >= 0 && toks[prop].Type == TokIdent && toks[prop].Text == paramName {
									hasAssign = true
									break
								}
							}
						}
					}
					if !hasAssign {
						assignmentsToInsert = append(assignmentsToInsert, fmt.Sprintf("$this->%s = $%s;", paramName, paramName))
					}
				}
			}
		}

		if len(assignmentsToInsert) > 0 {
			var sb strings.Builder
			sb.WriteString("\r\n\t\t")
			sb.WriteString(strings.Join(assignmentsToInsert, "\r\n\t\t"))
			edits = append(edits, edit{bo + 1, bo + 1, sb.String()})
		}
	}

	sortEdits(edits)
	return applyEdits(toks, edits)
}

// ---------- $x =& new C(...) -> $x = new C(...) ----------
//
// Reference-assigning `new` was removed in PHP 7.0. Since PHP 5 objects are
// handles, `=& new` was always semantically identical to `= new` — dropping
// the & is a safe mechanical migration. Plain reference assignment
// ($b = &$a) is still valid PHP 8 and must NOT be touched: the rule only
// fires when & is immediately followed by `new`.
func ruleRefAssignNew(toks []Token, add func(string, int, Severity, string, ...interface{})) []Token {
	var edits []edit
	for i := 0; i < len(toks); i++ {
		if !isKw(toks[i], "new") {
			continue
		}
		amp := prevSig(toks, i)
		if amp < 0 || !isOp(toks[amp], "&") {
			continue
		}
		eq := prevSig(toks, amp)
		if eq < 0 || !isOp(toks[eq], "=") {
			continue
		}
		// whitespace-aware deletion keeps exactly one space between = and new
		// (or none when everything was glued: $x =&new ...)
		end := amp + 1
		wsBefore := amp > 0 && toks[amp-1].Type == TokWhitespace
		wsAfter := amp+1 < len(toks) && toks[amp+1].Type == TokWhitespace
		if wsBefore && wsAfter {
			end = amp + 2
		}
		edits = append(edits, edit{amp, end, ""})
		add("ref-assign-new", toks[i].Line, Fixed,
			"Dropped & from '=& new' (removed in PHP 7.0, semantically identical)")
	}
	sortEdits(edits)
	return applyEdits(toks, edits)
}

// ---------- implicit nullable parameters (deprecated in PHP 8.4) ----------
//
// `function f(string $x = null)` implicitly makes the type nullable; PHP 8.4
// deprecates it in favor of the explicit `?string $x = null` (valid since
// 7.1, semantically identical). Skipped when already explicit (?), when the
// union mentions null, for mixed (already includes null), for untyped
// params, for non-null defaults, and for by-ref params (kept conservative).
func ruleImplicitNullable(toks []Token, add func(string, int, Severity, string, ...interface{})) []Token {
	var edits []edit
	for i := 0; i < len(toks); i++ {
		if !(isKw(toks[i], "function") || isKw(toks[i], "fn")) {
			continue
		}
		if px := prevSig(toks, i); px >= 0 &&
			(isOp(toks[px], "->") || isOp(toks[px], "?->") || isOp(toks[px], "::")) {
			continue // $obj->fn(...) — a method call, not an arrow function
		}
		j := nextSig(toks, i)
		if j < 0 {
			continue
		}
		if isKw(toks[i], "function") {
			if isOp(toks[j], "&") { // function &name()
				j = nextSig(toks, j)
			}
			if j >= 0 && toks[j].Type == TokIdent {
				j = nextSig(toks, j) // skip the declared function name
			}
		}
		if j < 0 || !isOp(toks[j], "(") {
			continue
		}
		close := matchDelim(toks, j, "(", ")")
		if close < 0 {
			continue
		}
		commas := topLevelCommas(toks, j, close)
		bounds := append([]int{j}, commas...)
		bounds = append(bounds, close)
		for bi := 0; bi+1 < len(bounds); bi++ {
			var sig []int
			for k := bounds[bi] + 1; k < bounds[bi+1]; k++ {
				if toks[k].Type != TokWhitespace && toks[k].Type != TokComment {
					sig = append(sig, k)
				}
			}
			// required tail: [type...] $var = null
			if len(sig) < 4 {
				continue
			}
			v, eq, null := sig[len(sig)-3], sig[len(sig)-2], sig[len(sig)-1]
			if toks[v].Type != TokVariable || !isOp(toks[eq], "=") || !isKw(toks[null], "null") {
				continue
			}
			// by-ref params (&$x): kept conservative, not rewritten
			if len(sig) >= 5 && isOp(toks[sig[len(sig)-4]], "&") {
				continue
			}
			first := sig[0]
			if toks[first].Type != TokIdent { // `?`-prefixed or `&$var` — already explicit / skipped
				continue
			}
			if strings.EqualFold(toks[first].Text, "mixed") {
				continue // mixed already includes null
			}
			explicitNull := false
			for _, k := range sig[1 : len(sig)-3] {
				if isKw(toks[k], "null") {
					explicitNull = true
				}
			}
			if explicitNull {
				continue // string|null already says it
			}
			edits = append(edits, edit{first, first, "?"})
			add("implicit-nullable", toks[i].Line, Fixed,
				"Made nullable parameter explicit (?%s $… = null) — implicit nullable deprecated in PHP 8.4",
				toks[first].Text)
		}
	}
	sortEdits(edits)
	return applyEdits(toks, edits)
}

// ---------- strict_types ----------

func ruleStrictTypes(toks []Token, add func(string, int, Severity, string, ...interface{})) []Token {
	for i := 0; i < len(toks); i++ {
		if isKw(toks[i], "strict_types") {
			if px := prevSig(toks, i); px >= 0 && isOp(toks[px], "(") {
				return toks // already declared
			}
		}
	}
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if t.Type != TokTagOpen || t.Text != "<?php" {
			continue
		}
		// declare() must be the very first statement: only leading
		// whitespace (or a UTF-8 BOM) may precede the open tag
		ok := true
		for k := 0; k < i; k++ {
			p := toks[k]
			if p.Type == TokWhitespace ||
				(p.Type == TokHTML && strings.TrimSpace(stripBOM(p.Text)) == "") {
				continue
			}
			ok = false
			break
		}
		if !ok {
			continue
		}
		newToks := append([]Token{}, toks[:i+1]...)
		newToks = append(newToks, Token{Type: TokOperator, Text: "\ndeclare(strict_types=1);", Line: t.Line + 1})
		newToks = append(newToks, toks[i+1:]...)
		add("strict-types", t.Line+1, Fixed, "Added declare(strict_types=1) as first statement")
		return newToks
	}
	return toks
}

func stripBOM(s string) string {
	if len(s) >= 3 && s[0] == 0xEF && s[1] == 0xBB && s[2] == 0xBF {
		return s[3:]
	}
	return s
}

// ---------- (real) cast -> (float) ----------

func ruleRealCast(toks []Token, add func(string, int, Severity, string, ...interface{})) []Token {
	var edits []edit
	for i := 0; i < len(toks); i++ {
		if !isKw(toks[i], "real") {
			continue
		}
		px := prevSig(toks, i)
		if px < 0 || !isOp(toks[px], "(") {
			continue
		}
		nx := nextSig(toks, i)
		if nx < 0 || !isOp(toks[nx], ")") {
			continue
		}
		after := nextSig(toks, nx)
		if after < 0 {
			continue
		}
		at := toks[after]
		if !(at.Type == TokVariable || at.Type == TokNumber || at.Type == TokString ||
			isOp(at, "(") || isOp(at, "$")) {
			continue
		}
		edits = append(edits, edit{px, nx + 1, "(float)"})
		add("real-cast", toks[i].Line, Fixed, "Replaced deprecated (real) cast with (float) (removed in PHP 8.0)")
	}
	return applyEdits(toks, edits)
}

// ---------- Modernize legacy error_reporting() & remove deprecated E_STRICT ----------

func ruleEStrictAndErrorReporting(toks []Token, target string, add func(string, int, Severity, string, ...interface{})) []Token {
	var edits []edit

	for i := 0; i < len(toks); i++ {
		// 1. error_reporting(...) calls
		if isKw(toks[i], "error_reporting") {
			nx := nextSig(toks, i)
			if nx >= 0 && isOp(toks[nx], "(") {
				close := matchDelim(toks, nx, "(", ")")
				if close > nx {
					hasNotice := false
					hasStrict := false
					hasSuppression := false
					for j := nx + 1; j < close; j++ {
						if toks[j].Type == TokIdent {
							if strings.EqualFold(toks[j].Text, "E_NOTICE") {
								hasNotice = true
							}
							if strings.EqualFold(toks[j].Text, "E_STRICT") {
								hasStrict = true
							}
						}
						if isOp(toks[j], "~") || isOp(toks[j], "^") {
							hasSuppression = true
						}
					}
					if (hasNotice && hasSuppression) || hasStrict {
						edits = append(edits, edit{nx + 1, close, "E_ALL & ~E_NOTICE & ~E_WARNING & ~E_DEPRECATED"})
						add("error-reporting-compat", toks[i].Line, Fixed,
							"Modernized legacy error_reporting() to suppress PHP 8 undefined variable warnings & deprecated E_STRICT")
						i = close
						continue
					}
				}
			}
		}

		// 2. Standalone E_STRICT references (outside error_reporting)
		if toks[i].Type == TokIdent && strings.EqualFold(toks[i].Text, "E_STRICT") {
			px := prevSig(toks, i)
			if px >= 0 && isOp(toks[px], "~") {
				pp := prevSig(toks, px)
				if pp >= 0 && (isOp(toks[pp], "&") || isOp(toks[pp], "^") || isOp(toks[pp], "|")) {
					edits = append(edits, edit{pp, i + 1, ""})
				} else {
					edits = append(edits, edit{px, i + 1, "0"})
				}
			} else if px >= 0 && (isOp(toks[px], "^") || isOp(toks[px], "|") || isOp(toks[px], "&")) {
				edits = append(edits, edit{px, i + 1, ""})
			} else {
				nx := nextSig(toks, i)
				if nx >= 0 && (isOp(toks[nx], "^") || isOp(toks[nx], "|") || isOp(toks[nx], "&")) {
					edits = append(edits, edit{i, nx + 1, ""})
				} else {
					edits = append(edits, edit{i, i + 1, "0"})
				}
			}
			add("estrict-removed", toks[i].Line, Fixed,
				"Removed deprecated E_STRICT constant reference (removed in PHP 8.4)")
		}
	}

	sortEdits(edits)
	return applyEdits(toks, edits)
}

// ---------- #[\\AllowDynamicProperties] attribute for classes (PHP 8.2+) ----------

func ruleAllowDynamicProperties(toks []Token, add func(string, int, Severity, string, ...interface{})) []Token {
	classes := findClasses(toks)
	var edits []edit
	for _, cl := range classes {
		classKwIdx := -1
		for j := cl.open - 1; j >= 0; j-- {
			if isKw(toks[j], "class") {
				classKwIdx = j
				break
			}
		}
		if classKwIdx < 0 {
			continue
		}
		hasAttr := false
		for j := classKwIdx - 1; j >= 0 && j >= classKwIdx-15; j-- {
			if strings.Contains(toks[j].Text, "AllowDynamicProperties") {
				hasAttr = true
				break
			}
			if isOp(toks[j], ";") || isOp(toks[j], "}") {
				break
			}
		}
		// Place attribute before any class modifiers like abstract, final, readonly
		insertIdx := classKwIdx
		for j := classKwIdx - 1; j >= 0; j-- {
			if toks[j].Type == TokWhitespace || toks[j].Type == TokComment {
				continue
			}
			if isKw(toks[j], "abstract") || isKw(toks[j], "final") || isKw(toks[j], "readonly") {
				insertIdx = j
				continue
			}
			break
		}
		if !hasAttr {
			edits = append(edits, edit{insertIdx, insertIdx, "#[\\AllowDynamicProperties]\n"})
			add("allow-dynamic-props", toks[insertIdx].Line, Fixed,
				"Added #[\\AllowDynamicProperties] attribute to class %s (avoids PHP 8.2+ dynamic property deprecations)", cl.name)
		}
	}
	sortEdits(edits)
	return applyEdits(toks, edits)
}

// ---------- Legacy ext/mysql constants (removed in PHP 7.0) -> mysqli equivalents ----------
// MYSQL_ASSOC -> MYSQLI_ASSOC, MYSQL_NUM -> MYSQLI_NUM, MYSQL_BOTH -> MYSQLI_BOTH

func ruleLegacyMysqlConstants(toks []Token, target string, add func(string, int, Severity, string, ...interface{})) []Token {
	if !versionAtLeast(target, "7.0") {
		return toks
	}
	var edits []edit
	for i := 0; i < len(toks); i++ {
		if toks[i].Type != TokIdent {
			continue
		}
		px := prevSig(toks, i)
		if px >= 0 && (isOp(toks[px], "->") || isOp(toks[px], "::") || isOp(toks[px], "?->")) {
			continue
		}
		var repl string
		switch toks[i].Text {
		case "MYSQL_ASSOC":
			repl = "MYSQLI_ASSOC"
		case "MYSQL_NUM":
			repl = "MYSQLI_NUM"
		case "MYSQL_BOTH":
			repl = "MYSQLI_BOTH"
		}
		if repl != "" {
			edits = append(edits, edit{i, i + 1, repl})
			add("legacy-mysql-const", toks[i].Line, Fixed,
				"Replaced removed ext/mysql constant %s with %s (PHP 7.0+)", toks[i].Text, repl)
		}
	}
	sortEdits(edits)
	return applyEdits(toks, edits)
}

// ---------- rulePostvalueNumber (PHP 8.0+) ----------
// In PHP 8+, `0 + ""` throws TypeError (Unsupported operand types: int + string).
// Modernizes `postvalue_number($key) { return 0 + postvalue($key); }` to be safe against non-numeric/empty strings.

func rulePostvalueNumber(toks []Token, add func(string, int, Severity, string, ...interface{})) []Token {
	var edits []edit
	for i := 0; i < len(toks); i++ {
		if !isKw(toks[i], "postvalue_number") {
			continue
		}
		px := prevSig(toks, i)
		if px < 0 || !isKw(toks[px], "function") {
			continue
		}
		nx := nextSig(toks, i)
		if nx < 0 || !isOp(toks[nx], "(") {
			continue
		}
		pc := matchDelim(toks, nx, "(", ")")
		if pc < 0 {
			continue
		}
		bo := nextSig(toks, pc)
		if bo < 0 || !isOp(toks[bo], "{") {
			continue
		}
		bc := matchDelim(toks, bo, "{", "}")
		if bc < 0 {
			continue
		}
		hasZeroAdd := false
		for k := bo + 1; k < bc-2; k++ {
			if toks[k].Type == TokNumber && toks[k].Text == "0" {
				plus := nextSig(toks, k)
				if plus >= 0 && isOp(toks[plus], "+") {
					pv := nextSig(toks, plus)
					if pv >= 0 && isKw(toks[pv], "postvalue") {
						hasZeroAdd = true
						break
					}
				}
			}
		}
		if hasZeroAdd {
			bodyReplacement := "{\r\n\t$val = postvalue($key);\r\n\tif (is_numeric($val))\r\n\t\treturn 0 + $val;\r\n\treturn intval($val);\r\n}"
			edits = append(edits, edit{bo, bc + 1, bodyReplacement})
			add("postvalue-number-php8", toks[i].Line, Fixed,
				"Modernized postvalue_number() body to prevent PHP 8 'Unsupported operand types: int + string' TypeError")
		}
	}
	sortEdits(edits)
	return applyEdits(toks, edits)
}

// ---------- ruleLegacyCountNull (PHP 8.0+) ----------
// In PHP 8+, `count(null)` throws TypeError (Countable|array, null given).
// In PHPRunner: `!count($this->menuNodes[$name])` is modernized to `empty($this->menuNodes[$name])`

func ruleLegacyCountNull(toks []Token, add func(string, int, Severity, string, ...interface{})) []Token {
	var edits []edit
	for i := 0; i < len(toks); i++ {
		if !isKw(toks[i], "count") {
			continue
		}
		px := prevSig(toks, i)
		if px < 0 || !isOp(toks[px], "!") {
			continue
		}
		nx := nextSig(toks, i)
		if nx < 0 || !isOp(toks[nx], "(") {
			continue
		}
		close := matchDelim(toks, nx, "(", ")")
		if close < 0 {
			continue
		}
		isMenuNodes := false
		for k := nx + 1; k < close; k++ {
			if toks[k].Type == TokIdent && toks[k].Text == "menuNodes" {
				isMenuNodes = true
				break
			}
		}
		if isMenuNodes {
			edits = append(edits, edit{px, nx + 1, "empty("})
			add("count-null-php8", toks[i].Line, Fixed,
				"Modernized !count($this->menuNodes[...]) to empty(...) to prevent PHP 8 count(null) TypeError")
		}
	}
	sortEdits(edits)
	return applyEdits(toks, edits)
}

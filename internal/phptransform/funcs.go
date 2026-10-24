package phptransform

import "strings"

// ---------- helpers over raw source ----------

// stringLiteralValue returns the PHP runtime value of a simple string token.
// Single-quoted strings are unescaped per PHP rules; double-quoted strings are
// accepted only when they contain no "$" or backslash. ok=false otherwise.
func stringLiteralValue(tok Token) (string, bool) {
	if tok.Type != TokString || len(tok.Text) < 2 {
		return "", false
	}
	switch tok.Text[0] {
	case '\'':
		if tok.Text[len(tok.Text)-1] != '\'' {
			return "", false
		}
		body := tok.Text[1 : len(tok.Text)-1]
		body = strings.ReplaceAll(body, `\'`, "'")
		body = strings.ReplaceAll(body, `\\`, `\`)
		return body, true
	case '"':
		if tok.Text[len(tok.Text)-1] != '"' {
			return "", false
		}
		body := tok.Text[1 : len(tok.Text)-1]
		if strings.ContainsAny(body, "$\\") {
			return "", false
		}
		return body, true
	}
	return "", false
}

// argSegment returns the raw original source text of the argument occupying
// token indexes [start, end), trimmed of surrounding whitespace/comments.
func argSegment(src string, toks []Token, start, end int) string {
	for start < end {
		if t := toks[start]; t.Type == TokWhitespace || t.Type == TokComment {
			start++
			continue
		}
		break
	}
	for end > start {
		if t := toks[end-1]; t.Type == TokWhitespace || t.Type == TokComment {
			end--
			continue
		}
		break
	}
	if start >= end {
		return ""
	}
	return src[toks[start].Pos : toks[end-1].Pos+len(toks[end-1].Text)]
}

// singleStringArg returns (tokenIndex, value, ok) when segment [start, end)
// consists of exactly one simple string literal.
func singleStringArg(toks []Token, start, end int) (int, string, bool) {
	idx := -1
	for j := start; j < end; j++ {
		switch toks[j].Type {
		case TokWhitespace, TokComment:
		case TokString:
			if idx >= 0 {
				return -1, "", false
			}
			idx = j
		default:
			return -1, "", false
		}
	}
	if idx < 0 {
		return -1, "", false
	}
	v, ok := stringLiteralValue(toks[idx])
	return idx, v, ok
}

const migrationMarker = "TODO(PHP-MIGRATION)"

// ---------- PCRE helpers ----------

var regexMetaChars = `.+*?[^]$(){}=!<>|:-#/\`

func hasRegexMeta(s string) bool { return strings.ContainsAny(s, regexMetaChars) }

// pregQuote mimics preg_quote().
func pregQuote(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if strings.IndexByte(regexMetaChars, s[i]) >= 0 {
			b.WriteByte('\\')
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func pickDelimiter(pat string) string {
	for _, d := range []string{"/", "#", "~", "%", "=", ","} {
		if !strings.Contains(pat, d) {
			return d
		}
	}
	return ""
}

// ---------- auto-fix builders ----------

// makeEregFix converts ereg/eregi/ereg_replace/eregi_replace with a literal
// pattern argument into the PCRE equivalent.
func makeEregFix(isReplace, ci bool) fnFix {
	return func(src string, toks []Token, open, close int) (string, bool) {
		commas := topLevelCommas(toks, open, close)
		need := 1
		if isReplace {
			need = 2
		}
		if len(commas) < need {
			return "", false
		}
		_, pat, ok := singleStringArg(toks, open+1, commas[0])
		if !ok || pat == "" {
			return "", false
		}
		d := pickDelimiter(pat)
		if d == "" {
			return "", false
		}
		mod := ""
		if ci {
			mod = "i"
		}
		target := "preg_match"
		if isReplace {
			target = "preg_replace"
		}
		rest := argSegment(src, toks, commas[0]+1, close)
		if rest == "" {
			return "", false
		}
		return target + "('" + d + pat + d + mod + "', " + rest + ")", true
	}
}

// makeSplitFix converts split/spliti with a literal delimiter.
func makeSplitFix(ci bool) fnFix {
	return func(src string, toks []Token, open, close int) (string, bool) {
		commas := topLevelCommas(toks, open, close)
		if len(commas) < 1 {
			return "", false
		}
		idx, delim, ok := singleStringArg(toks, open+1, commas[0])
		if !ok || delim == "" {
			return "", false
		}
		rest := argSegment(src, toks, commas[0]+1, close)
		if rest == "" {
			return "", false
		}
		if !ci && !hasRegexMeta(delim) {
			// plain delimiter — explode() is the faithful equivalent
			return "explode(" + toks[idx].Text + ", " + rest + ")", true
		}
		// The delimiter is a POSIX ERE pattern (or spliti needs /i): ERE and
		// PCRE share syntax here, so use the pattern as-is and never re-quote.
		pat := delim
		if !hasRegexMeta(delim) {
			pat = pregQuote(delim) // literal delimiter, only spliti reaches this
		}
		d := pickDelimiter(pat)
		if d == "" {
			return "", false
		}
		mod := ""
		if ci {
			mod = "i"
		}
		return "preg_split('" + d + pat + d + mod + "', " + rest + ")", true
	}
}

// fixCreateFunction converts create_function('$a', 'return ...;') literals
// into a real anonymous function.
func fixCreateFunction(src string, toks []Token, open, close int) (string, bool) {
	commas := topLevelCommas(toks, open, close)
	if len(commas) != 1 {
		return "", false
	}
	_, params, ok1 := singleStringArg(toks, open+1, commas[0])
	_, body, ok2 := singleStringArg(toks, commas[0]+1, close)
	if !ok1 || !ok2 {
		return "", false
	}
	params = strings.TrimSpace(params)
	body = strings.TrimSpace(body)
	if body == "" {
		return "", false
	}
	if !strings.HasSuffix(body, ";") && !strings.HasSuffix(body, "}") {
		body += ";"
	}
	return "function(" + params + ") { " + body + " }", true
}

// ---------- deprecated function table ----------

type fnFix func(src string, toks []Token, open, close int) (string, bool)

type deprecatedFn struct {
	names    []string
	prefixes []string
	since    string // warn/fix when target >= since
	note     string
	fix      fnFix
}

var deprecatedTable = []deprecatedFn{
	{names: []string{"utf8_encode"}, since: "8.2",
		note: "utf8_encode() is deprecated (PHP 8.2) — converted to mb_convert_encoding()",
		fix: func(src string, toks []Token, open, close int) (string, bool) {
			arg := argSegment(src, toks, open+1, close)
			if arg == "" {
				return "", false
			}
			return "mb_convert_encoding(" + arg + ", 'UTF-8', 'ISO-8859-1')", true
		}},
	{names: []string{"utf8_decode"}, since: "8.2",
		note: "utf8_decode() is deprecated (PHP 8.2) — converted to mb_convert_encoding()",
		fix: func(src string, toks []Token, open, close int) (string, bool) {
			arg := argSegment(src, toks, open+1, close)
			if arg == "" {
				return "", false
			}
			return "mb_convert_encoding(" + arg + ", 'ISO-8859-1', 'UTF-8')", true
		}},
	{names: []string{"ereg"}, since: "5.4",
		note: "ereg() removed in PHP 5.4 — converted to preg_match()",
		fix:  makeEregFix(false, false)},
	{names: []string{"eregi"}, since: "5.4",
		note: "eregi() removed in PHP 5.4 — converted to preg_match() with /i",
		fix:  makeEregFix(false, true)},
	{names: []string{"ereg_replace"}, since: "5.4",
		note: "ereg_replace() removed in PHP 5.4 — converted to preg_replace()",
		fix:  makeEregFix(true, false)},
	{names: []string{"eregi_replace"}, since: "5.4",
		note: "eregi_replace() removed in PHP 5.4 — converted to preg_replace() with /i",
		fix:  makeEregFix(true, true)},
	{names: []string{"split"}, since: "5.4",
		note: "split() removed in PHP 5.4 — converted to explode()/preg_split()",
		fix:  makeSplitFix(false)},
	{names: []string{"spliti"}, since: "5.4",
		note: "spliti() removed in PHP 5.4 — converted to preg_split() with /i",
		fix:  makeSplitFix(true)},
	{names: []string{"sql_regcase"}, since: "5.4",
		note: "sql_regcase() removed in PHP 5.4 — write the case-insensitive regex manually"},
	{names: []string{"session_register", "session_unregister", "session_is_registered"},
		since: "5.4", note: "session_* registration functions removed — use $_SESSION directly"},
	{names: []string{"call_user_method"}, since: "7.0",
		note: "call_user_method() removed in PHP 7.0 — use call_user_func([$obj, 'method'], ...)"},
	{names: []string{"call_user_method_array"}, since: "7.0",
		note: "call_user_method_array() removed in PHP 7.0 — use call_user_func_array([$obj, 'method'], ...)"},
	{prefixes: []string{"mysql_"}, since: "7.0",
		note: "mysql_* extension removed in PHP 7.0 — migrate to PDO or MySQLi"},
	{prefixes: []string{"mssql_"}, since: "7.0",
		note: "mssql_* extension removed in PHP 7.0 — use sqlsrv/pdo_sqlsrv"},
	{names: []string{"each"}, since: "7.2",
		note: "each() removed in PHP 8.0 — rewrite the loop using foreach()"},
	{names: []string{"create_function"}, since: "7.2",
		note: "create_function() removed in PHP 8.0 — converted to an anonymous function",
		fix:  fixCreateFunction},
	{names: []string{"get_magic_quotes_gpc"}, since: "7.4",
		note: "get_magic_quotes_gpc() removed in PHP 8.0 — always returned false since 5.4, replace with false"},
	{names: []string{"get_magic_quotes_runtime"}, since: "7.4",
		note: "get_magic_quotes_runtime() removed in PHP 8.0 — replace with false"},
	{names: []string{"is_real"}, since: "7.4",
		note: "is_real() removed in PHP 8.0 — converted to is_float()",
		fix: func(src string, toks []Token, open, close int) (string, bool) {
			arg := argSegment(src, toks, open+1, close)
			if arg == "" {
				return "", false
			}
			return "is_float(" + arg + ")", true
		}},
	{names: []string{"import_request_variables", "define_syslog_variables"}, since: "7.3",
		note: "function removed — delete the call or use the documented alternative"},
	{prefixes: []string{"mcrypt_"}, since: "7.2",
		note: "mcrypt_* removed in PHP 7.2 — use OpenSSL or Sodium"},
}

func lookupDeprecated(name string) *deprecatedFn {
	for k := range deprecatedTable {
		t := &deprecatedTable[k]
		for _, n := range t.names {
			if strings.EqualFold(n, name) {
				return t
			}
		}
		for _, p := range t.prefixes {
			if len(name) > len(p) && strings.EqualFold(name[:len(p)], p) {
				return t
			}
		}
	}
	return nil
}

// ruleDeprecated rewrites mechanically-convertible removed calls and injects
// inline TODO comments for the rest. It runs in passes with a re-lex between
// passes so calls nested inside a fixed call are handled too.
func ruleDeprecated(toks []Token, src, target string, add func(string, int, Severity, string, ...interface{})) []Token {
	const maxPasses = 6
	for pass := 0; pass < maxPasses; pass++ {
		var edits []edit
		fixedRanges := [][2]int{}
		inFixed := func(i int) bool {
			for _, r := range fixedRanges {
				if i > r[0] && i < r[1] {
					return true
				}
			}
			return false
		}
		for i := 0; i < len(toks); i++ {
			if toks[i].Type != TokIdent {
				continue
			}
			if inFixed(i) {
				continue
			}
			nx := nextSig(toks, i)
			if nx < 0 || !isOp(toks[nx], "(") {
				continue
			}
			if px := prevSig(toks, i); px >= 0 &&
				(isOp(toks[px], "->") || isOp(toks[px], "::") || isOp(toks[px], "?->")) {
				continue
			}
			// already annotated by a previous pass: check the raw preceding
			// token (prevSig skips comments, which would hide the marker)
			annotated := false
			for k := i - 1; k >= 0; k-- {
				if toks[k].Type == TokWhitespace {
					continue
				}
				if toks[k].Type == TokComment && strings.Contains(toks[k].Text, migrationMarker) {
					annotated = true
				}
				break
			}
			if annotated {
				continue
			}
			name := toks[i].Text
			close := matchDelim(toks, nx, "(", ")")
			if close < 0 {
				continue
			}

			// __autoload declaration -> spl_autoload_register
			if isKw(toks[i], "__autoload") && versionAtLeast(target, "8.0") {
				px := prevSig(toks, i)
				if px >= 0 && isKw(toks[px], "function") {
					bo := nextSig(toks, close)
					if bo >= 0 && isOp(toks[bo], "{") {
						if bc := matchDelim(toks, bo, "{", "}"); bc > 0 {
							params := src[toks[nx].Pos : toks[close].Pos+1]
							body := src[toks[bo].Pos : toks[bc].Pos+1]
							edits = append(edits, edit{px, bc + 1,
								"spl_autoload_register(function" + params + " " + body + ");"})
							fixedRanges = append(fixedRanges, [2]int{px, bc + 1})
							add("deprecated-fn", toks[i].Line, Fixed,
								"__autoload() converted to spl_autoload_register() (PHP 8.0)")
							continue
						}
					}
				}
				edits = append(edits, edit{i, i,
					"/* " + migrationMarker + ": __autoload removed in PHP 8.0 — use spl_autoload_register() */ "})
				add("deprecated-fn", toks[i].Line, Notice,
					"__autoload removed in PHP 8.0 — use spl_autoload_register()")
				continue
			}

			// parse_str() with a single argument
			if isKw(toks[i], "parse_str") && versionAtLeast(target, "8.0") {
				if len(topLevelCommas(toks, nx, close)) == 0 {
					edits = append(edits, edit{i, i,
						"/* " + migrationMarker + ": parse_str() requires the result array as 2nd argument since PHP 8.0 */ "})
					add("deprecated-fn", toks[i].Line, Notice,
						"parse_str() without 2nd argument removed in PHP 8.0 — pass a result array")
				}
				continue
			}

			// preg_replace() with /e modifier
			if isKw(toks[i], "preg_replace") && versionAtLeast(target, "7.0") {
				if commas := topLevelCommas(toks, nx, close); len(commas) >= 1 {
					if _, v, ok := singleStringArg(toks, nx+1, commas[0]); ok && len(v) >= 3 &&
						v[len(v)-1] == 'e' && v[0] == v[len(v)-2] {
						edits = append(edits, edit{i, i,
							"/* " + migrationMarker + ": /e modifier removed in PHP 7.0 — use preg_replace_callback() */ "})
						add("deprecated-fn", toks[i].Line, Notice,
							"preg_replace() /e modifier removed in PHP 7.0 — use preg_replace_callback()")
					}
				}
				continue
			}

			info := lookupDeprecated(name)
			if info == nil || !versionAtLeast(target, info.since) {
				continue
			}
			if info.fix != nil {
				if text, ok := info.fix(src, toks, nx, close); ok {
					edits = append(edits, edit{i, close + 1, text})
					fixedRanges = append(fixedRanges, [2]int{i, close + 1})
					add("deprecated-fn", toks[i].Line, Fixed, "%s", info.note)
					continue
				}
			}
			edits = append(edits, edit{i, i, "/* " + migrationMarker + ": " + info.note + " */ "})
			add("deprecated-fn", toks[i].Line, Notice, "%s", info.note)
		}

		if len(edits) == 0 {
			return toks
		}
		toks = applyEdits(toks, edits)
		src = Render(toks)
		toks = Lex(src)
	}
	return toks
}

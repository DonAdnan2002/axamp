// Package phptransform provides a tokenizer-based PHP code upgrader.
// It analyzes PHP source as real tokens (never blind string replacement),
// so strings, comments and heredocs are never accidentally modified.
package phptransform

import "strings"

// TokType classifies a lexical token.
type TokType int

const (
	TokHTML       TokType = iota // content outside <?php ?> tags
	TokTagOpen                   // <?php / <?= / <?
	TokTagClose                  // ?>
	TokWhitespace
	TokComment // // ... / # ... / /* ... */
	TokString  // '...' / "..." / `...` / heredoc / nowdoc
	TokVariable // $name
	TokNumber
	TokIdent
	TokOperator
)

// Token is a single lexical unit with its byte offset and line number.
type Token struct {
	Type TokType
	Text string
	Pos  int // byte offset in original source
	Line int
}

func isIdentStart(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || b >= 0x80
}

func isIdentChar(b byte) bool {
	return isIdentStart(b) || (b >= '0' && b <= '9')
}

func isHexChar(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
}

// longest-match operators; "#[" is the PHP 8 attribute opener, not a comment
var multiOps = []string{
	"<<=", ">>=", "**=", "...", "===", "!==", "<=>", "??=", "?->",
	"<<", ">>", "**", "==", "!=", "<>", "<=", ">=", "&&", "||",
	"++", "--", "+=", "-=", "*=", "/=", "%=", ".=", "&=", "|=", "^=",
	"=>", "->", "::", "??", "#[",
}

// Lex splits PHP source (possibly mixed with HTML) into tokens.
func Lex(src string) []Token {
	var toks []Token
	line := 1
	i := 0
	n := len(src)
	inPHP := false

	add := func(t TokType, start int, text string) {
		toks = append(toks, Token{Type: t, Text: text, Pos: start, Line: line})
		line += strings.Count(text, "\n")
	}

	for i < n {
		if !inPHP {
			j := strings.Index(src[i:], "<?")
			if j < 0 {
				add(TokHTML, i, src[i:])
				break
			}
			if j > 0 {
				add(TokHTML, i, src[i:i+j])
			}
			i += j
			rest := src[i:]
			switch {
			case strings.HasPrefix(rest, "<?php"):
				add(TokTagOpen, i, "<?php")
				i += 5
			case strings.HasPrefix(rest, "<?="):
				add(TokTagOpen, i, "<?=")
				i += 3
			default:
				add(TokTagOpen, i, "<?")
				i += 2
			}
			inPHP = true
			continue
		}

		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '\v' || c == '\f':
			j := i + 1
			for j < n && strings.IndexByte(" \t\r\n\v\f", src[j]) >= 0 {
				j++
			}
			add(TokWhitespace, i, src[i:j])
			i = j

		case c == '?' && i+1 < n && src[i+1] == '>':
			add(TokTagClose, i, "?>")
			i += 2
			inPHP = false

		case (c == '/' && i+1 < n && src[i+1] == '/') || (c == '#' && !(i+1 < n && src[i+1] == '[')):
			j := i
			for j < n && src[j] != '\n' && !(src[j] == '?' && j+1 < n && src[j+1] == '>') {
				j++
			}
			add(TokComment, i, src[i:j])
			i = j

		case c == '/' && i+1 < n && src[i+1] == '*':
			end := n
			if k := strings.Index(src[i+2:], "*/"); k >= 0 {
				end = i + 2 + k + 2
			}
			add(TokComment, i, src[i:end])
			i = end

		case c == '\'' || c == '"' || c == '`':
			j := i + 1
			for j < n {
				if src[j] == '\\' {
					j += 2
					continue
				}
				if src[j] == c {
					j++
					break
				}
				j++
			}
			if j > n {
				j = n
			}
			add(TokString, i, src[i:j])
			i = j

		case c == '<' && strings.HasPrefix(src[i:], "<<<"):
			j := i + 3
			for j < n && (src[j] == ' ' || src[j] == '\t') {
				j++
			}
			quote := byte(0)
			if j < n && (src[j] == '\'' || src[j] == '"') {
				quote = src[j]
				j++
			}
			ms := j
			for j < n && isIdentChar(src[j]) {
				j++
			}
			marker := src[ms:j]
			if quote != 0 && j < n && src[j] == quote {
				j++
			}
			for j < n && src[j] != '\n' { // rest of header line
				j++
			}
			for j < n && src[j] == '\n' {
				lineStart := j + 1
				k := lineStart
				for k < n && (src[k] == ' ' || src[k] == '\t') {
					k++
				}
				if strings.HasPrefix(src[k:], marker) {
					after := k + len(marker)
					if after >= n || !isIdentChar(src[after]) {
						j = after
						break
					}
				}
				nl := strings.IndexByte(src[lineStart:], '\n')
				if nl < 0 {
					j = n
				} else {
					j = lineStart + nl
				}
			}
			if j > n {
				j = n
			}
			add(TokString, i, src[i:j])
			i = j

		case c == '$' && i+1 < n && isIdentStart(src[i+1]):
			j := i + 1
			for j < n && isIdentChar(src[j]) {
				j++
			}
			add(TokVariable, i, src[i:j])
			i = j

		case isIdentStart(c):
			j := i
			for j < n && isIdentChar(src[j]) {
				j++
			}
			add(TokIdent, i, src[i:j])
			i = j

		case (c >= '0' && c <= '9') || (c == '.' && i+1 < n && src[i+1] >= '0' && src[i+1] <= '9'):
			j := i
			if c == '0' && i+1 < n && (src[i+1] == 'x' || src[i+1] == 'X') {
				j = i + 2
				for j < n && (isHexChar(src[j]) || src[j] == '_') {
					j++
				}
			} else {
				for j < n {
					ch := src[j]
					if (ch >= '0' && ch <= '9') || ch == '_' || ch == '.' {
						j++
						continue
					}
					if (ch == 'e' || ch == 'E') && j+1 < n &&
						(src[j+1] == '+' || src[j+1] == '-' || (src[j+1] >= '0' && src[j+1] <= '9')) {
						j += 2
						continue
					}
					break
				}
			}
			add(TokNumber, i, src[i:j])
			i = j

		default:
			matched := ""
			for _, op := range multiOps {
				if strings.HasPrefix(src[i:], op) {
					matched = op
					break
				}
			}
			if matched == "" {
				matched = src[i : i+1]
			}
			add(TokOperator, i, matched)
			i += len(matched)
		}
	}
	return toks
}

// Render concatenates token texts back into source code.
func Render(toks []Token) string {
	var b strings.Builder
	for _, t := range toks {
		b.WriteString(t.Text)
	}
	return b.String()
}

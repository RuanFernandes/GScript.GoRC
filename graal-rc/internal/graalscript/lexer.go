package graalscript

import (
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

type tokenKind uint8

const (
	tokenEOF tokenKind = iota
	tokenIdentifier
	tokenNumber
	tokenString
	tokenOperator
	tokenPunctuation
	tokenComment
)

type token struct {
	kind     tokenKind
	text     string
	start    int
	end      int
	startPos Position
	endPos   Position
}

type lexer struct {
	text      string
	offset    int
	line      int
	character int
}

func lex(text string) []token {
	l := lexer{text: text}
	tokens := make([]token, 0, len(text)/4)
	for l.offset < len(l.text) {
		if l.skipWhitespace() {
			continue
		}
		start := l.offset
		startPos := Position{Line: l.line, Character: l.character}
		r, size := l.peek()
		if r == '/' && l.peekNext('/') {
			l.consumeLineComment()
			tokens = append(tokens, l.makeToken(tokenComment, start, startPos))
			continue
		}
		if r == '/' && l.peekNext('*') {
			l.consumeBlockComment()
			tokens = append(tokens, l.makeToken(tokenComment, start, startPos))
			continue
		}
		if r == '"' {
			l.consumeString()
			tokens = append(tokens, l.makeToken(tokenString, start, startPos))
			continue
		}
		if isIdentifierStart(r) {
			l.consumeIdentifier()
			tokens = append(tokens, l.makeToken(tokenIdentifier, start, startPos))
			continue
		}
		if unicode.IsDigit(r) {
			l.consumeNumber()
			tokens = append(tokens, l.makeToken(tokenNumber, start, startPos))
			continue
		}
		if operator, ok := l.consumeOperator(); ok {
			tokens = append(tokens, token{
				kind: tokenOperator, text: operator, start: start, end: l.offset,
				startPos: startPos, endPos: Position{Line: l.line, Character: l.character},
			})
			continue
		}
		if strings.ContainsRune("(){}[];,.:?", r) {
			l.advance()
			tokens = append(tokens, l.makeToken(tokenPunctuation, start, startPos))
			continue
		}
		// Unknown characters are retained as one-character operators. This is
		// important for an editor parser: an unfinished expression must not make
		// all following tokens disappear.
		l.advanceBySize(size)
		tokens = append(tokens, l.makeToken(tokenOperator, start, startPos))
	}
	tokens = append(tokens, token{
		kind: tokenEOF, start: len(text), end: len(text),
		startPos: Position{Line: l.line, Character: l.character},
		endPos:   Position{Line: l.line, Character: l.character},
	})
	return tokens
}

func (l *lexer) skipWhitespace() bool {
	r, size := l.peek()
	if r == 0 || !unicode.IsSpace(r) {
		return false
	}
	l.advanceBySize(size)
	return true
}

func (l *lexer) consumeLineComment() {
	for l.offset < len(l.text) {
		r, size := l.peek()
		if r == '\n' {
			return
		}
		l.advanceBySize(size)
	}
}

func (l *lexer) consumeBlockComment() {
	l.advanceBySize(2)
	for l.offset < len(l.text) {
		r, _ := l.peek()
		if l.peekNext('/') && r == '*' {
			l.advanceBySize(2)
			return
		}
		_, size := l.peek()
		l.advanceBySize(size)
	}
}

func (l *lexer) consumeString() {
	l.advance()
	for l.offset < len(l.text) {
		r, size := l.peek()
		if r == '\\' {
			l.advanceBySize(size)
			if l.offset < len(l.text) {
				_, escapedSize := l.peek()
				l.advanceBySize(escapedSize)
			}
			continue
		}
		l.advanceBySize(size)
		if r == '"' {
			return
		}
	}
}

func (l *lexer) consumeIdentifier() {
	for l.offset < len(l.text) {
		r, size := l.peek()
		if !isIdentifierPart(r) {
			return
		}
		l.advanceBySize(size)
	}
}

func (l *lexer) consumeNumber() {
	dot := false
	for l.offset < len(l.text) {
		r, size := l.peek()
		if unicode.IsDigit(r) {
			l.advanceBySize(size)
			continue
		}
		if r == '.' && !dot {
			dot = true
			l.advanceBySize(size)
			continue
		}
		if (r == 'x' || r == 'X') && l.offset > 0 && l.text[l.offset-1] == '0' {
			l.advanceBySize(size)
			continue
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			l.advanceBySize(size)
			continue
		}
		return
	}
}

func (l *lexer) consumeOperator() (string, bool) {
	if l.offset >= len(l.text) {
		return "", false
	}
	remaining := l.text[l.offset:]
	for _, op := range []string{"===", "!==", "...", "==", "!=", "<=", ">=", "&&", "||", "++", "--", "+=", "-=", "*=", "/=", "%=", "::", "=>"} {
		if strings.HasPrefix(remaining, op) {
			for range op {
				l.advance()
			}
			return op, true
		}
	}
	if strings.ContainsRune("@+-*/%|=*!?&<>~^", rune(remaining[0])) {
		l.advance()
		return remaining[:1], true
	}
	return "", false
}

func (l *lexer) makeToken(kind tokenKind, start int, startPos Position) token {
	return token{
		kind: kind, text: l.text[start:l.offset], start: start, end: l.offset,
		startPos: startPos, endPos: Position{Line: l.line, Character: l.character},
	}
}

func (l *lexer) peek() (rune, int) {
	if l.offset >= len(l.text) {
		return 0, 0
	}
	r, size := utf8.DecodeRuneInString(l.text[l.offset:])
	return r, size
}

func (l *lexer) peekNext(expected rune) bool {
	_, size := l.peek()
	if size == 0 || l.offset+size >= len(l.text) {
		return false
	}
	r, _ := utf8.DecodeRuneInString(l.text[l.offset+size:])
	return r == expected
}

func (l *lexer) advance() {
	_, size := l.peek()
	if size > 0 {
		l.advanceBySize(size)
	}
}

func (l *lexer) advanceBySize(size int) {
	if size <= 0 || l.offset >= len(l.text) {
		return
	}
	end := l.offset + size
	if end > len(l.text) {
		end = len(l.text)
	}
	r, _ := utf8.DecodeRuneInString(l.text[l.offset:end])
	l.offset = end
	if r == '\n' {
		l.line++
		l.character = 0
		return
	}
	l.character += len(utf16.Encode([]rune{r}))
}

func isIdentifierStart(r rune) bool {
	return r == '_' || r == '$' || unicode.IsLetter(r)
}

func isIdentifierPart(r rune) bool {
	return isIdentifierStart(r) || unicode.IsDigit(r)
}

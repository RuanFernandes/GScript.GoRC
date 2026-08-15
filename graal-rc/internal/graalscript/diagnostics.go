package graalscript

import (
	"fmt"
	"strings"
)

type delimiterFrame struct {
	token token
	close string
}

var openingDelimiters = map[string]string{
	"(": ")",
	"[": "]",
	"{": "}",
}

var closingDelimiters = map[string]bool{
	")": true,
	"]": true,
	"}": true,
}

func (d *Document) diagnostics() []Diagnostic {
	diagnostics := make([]Diagnostic, 0)
	stack := make([]delimiterFrame, 0)

	for _, current := range d.Tokens {
		if current.kind == tokenComment || current.kind == tokenString || current.kind == tokenEOF {
			continue
		}
		if close, ok := openingDelimiters[current.text]; ok {
			stack = append(stack, delimiterFrame{token: current, close: close})
			continue
		}
		if !closingDelimiters[current.text] {
			continue
		}

		if len(stack) == 0 {
			diagnostics = append(diagnostics, unexpectedDelimiterDiagnostic(current))
			continue
		}
		top := stack[len(stack)-1]
		if top.close == current.text {
			stack = stack[:len(stack)-1]
			continue
		}

		// Recover from a mismatched closer by discarding nested openers until
		// the matching opener is found. Each discarded opener gets its own
		// diagnostic so the user can fix the actual nesting problem.
		match := -1
		for i := len(stack) - 2; i >= 0; i-- {
			if stack[i].close == current.text {
				match = i
				break
			}
		}
		if match < 0 {
			diagnostics = append(diagnostics, unexpectedDelimiterDiagnostic(current))
			continue
		}
		for i := match + 1; i < len(stack); i++ {
			diagnostics = append(diagnostics, unclosedDelimiterDiagnostic(stack[i]))
		}
		stack = stack[:match]
	}

	for _, frame := range stack {
		diagnostics = append(diagnostics, unclosedDelimiterDiagnostic(frame))
	}
	diagnostics = append(diagnostics, d.semicolonDiagnostics()...)
	return diagnostics
}

type semicolonParser struct {
	tokens    []token
	pairs     map[int]int
	blockOpen map[int]bool
	missing   []Diagnostic
}

func (d *Document) semicolonDiagnostics() []Diagnostic {
	parser := newSemicolonParser(d.Tokens)
	parser.parseSequence(0, len(parser.tokens))
	return parser.missing
}

func newSemicolonParser(tokens []token) *semicolonParser {
	filtered := make([]token, 0, len(tokens))
	for _, current := range tokens {
		if current.kind != tokenComment && current.kind != tokenEOF {
			filtered = append(filtered, current)
		}
	}

	parser := &semicolonParser{
		tokens:    filtered,
		pairs:     make(map[int]int),
		blockOpen: make(map[int]bool),
	}
	stack := make([]int, 0)
	for i, current := range parser.tokens {
		switch current.text {
		case "(", "[", "{":
			stack = append(stack, i)
		case ")", "]", "}":
			if len(stack) == 0 || !matchingDelimiters(parser.tokens[stack[len(stack)-1]].text, current.text) {
				continue
			}
			open := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			parser.pairs[open] = i
			parser.pairs[i] = open
		}
	}
	for i, current := range parser.tokens {
		if current.text == "{" && parser.looksLikeBlockOpen(i) {
			parser.blockOpen[i] = true
		}
	}
	return parser
}

func matchingDelimiters(open, close string) bool {
	switch open {
	case "(":
		return close == ")"
	case "[":
		return close == "]"
	case "{":
		return close == "}"
	default:
		return false
	}
}

func (p *semicolonParser) looksLikeBlockOpen(index int) bool {
	if index <= 0 {
		return true
	}
	previous := p.tokens[index-1]
	if previous.text == ")" || previous.text == ";" || previous.text == "}" {
		return true
	}
	if previous.text == ":" && p.followsCaseLabel(index) {
		return true
	}
	if previous.kind == tokenIdentifier {
		switch strings.ToLower(previous.text) {
		case "else", "try", "finally", "do":
			return true
		}
	}
	return false
}

func (p *semicolonParser) followsCaseLabel(index int) bool {
	for i := index - 1; i >= 0; i-- {
		if p.isCaseLabel(i) {
			return true
		}
		switch p.tokens[i].text {
		case "{", "}", ";":
			return false
		}
	}
	return false
}

func (p *semicolonParser) parseSequence(start, end int) int {
	for start < end {
		if p.tokens[start].text == "}" {
			return start + 1
		}
		start = p.parseStatement(start, end)
	}
	return start
}

func (p *semicolonParser) parseStatement(start, end int) int {
	if start >= end {
		return end
	}
	if p.tokens[start].text == ";" {
		return start + 1
	}
	if p.isCaseLabel(start) {
		return p.skipCaseLabel(start, end)
	}
	if p.isFunctionDeclarationStart(start) {
		return p.parseFunctionDeclaration(start, end)
	}
	if p.isEnumDeclarationStart(start) {
		return p.parseEnumDeclaration(start, end)
	}
	if p.isGUIDeclarationStart(start) {
		return p.parseGUIDeclaration(start, end)
	}
	if p.isControlKeyword(start) {
		return p.parseControl(start, end)
	}
	if p.tokens[start].text == "{" && p.blockOpen[start] {
		return p.parseBlock(start, end)
	}

	next, last, terminated := p.parseExpressionStatement(start, end)
	if !terminated && last >= start {
		p.addMissingSemicolon(p.tokens[last])
	}
	if next <= start {
		return start + 1
	}
	return next
}

func (p *semicolonParser) parseExpressionStatement(start, end int) (int, int, bool) {
	last := -1
	for index := start; index < end; index++ {
		current := p.tokens[index]
		if current.text == ";" {
			return index + 1, last, true
		}
		if current.text == "}" || (current.text == "{" && p.blockOpen[index]) {
			if current.text == "{" && p.blockOpen[index] && p.isGUIBlockOpen(index) {
				return p.parseBlock(index, end), last, true
			}
			return index, last, false
		}
		if index > start && p.hasLineBreak(last, index) && p.canEndExpression(last) && p.startsNewStatement(index) {
			return index, last, false
		}

		last = index
		if close, ok := p.pairs[index]; ok && close > index {
			if current.text == "{" && p.blockOpen[index] {
				return index, last, false
			}
			last = close
			index = close
		}
	}
	return end, last, false
}

func (p *semicolonParser) parseFunctionDeclaration(start, end int) int {
	functionIndex := start
	if !isIdentifierText(p.tokens[functionIndex], "function") {
		functionIndex++
	}
	open := functionIndex + 1
	for open < end && p.tokens[open].text != "(" {
		open++
	}
	if open >= end {
		return end
	}
	close, ok := p.pairs[open]
	if !ok || close <= open {
		return end
	}
	bodyOpen := close + 1
	if bodyOpen < end && p.tokens[bodyOpen].text == "{" && p.blockOpen[bodyOpen] {
		return p.parseBlock(bodyOpen, end)
	}
	return bodyOpen
}

func (p *semicolonParser) isEnumDeclarationStart(index int) bool {
	if index < 0 || index+2 >= len(p.tokens) || !isIdentifierText(p.tokens[index], "enum") {
		return false
	}
	return p.tokens[index+1].kind == tokenIdentifier && p.tokens[index+2].text == "{"
}

func (p *semicolonParser) parseEnumDeclaration(start, end int) int {
	open := start + 2
	close, ok := p.pairs[open]
	if !ok || close <= open {
		return end
	}

	next := close + 1
	if next < end && p.tokens[next].text == ";" {
		return next + 1
	}
	return next
}

func (p *semicolonParser) isGUIDeclarationStart(index int) bool {
	if index < 0 || index >= len(p.tokens) || !isIdentifierText(p.tokens[index], "new") {
		return false
	}
	control := index + 1
	if control >= len(p.tokens) || p.tokens[control].kind != tokenIdentifier || !strings.HasPrefix(strings.ToLower(p.tokens[control].text), "gui") {
		return false
	}
	open := control + 1
	if open >= len(p.tokens) || p.tokens[open].text != "(" {
		return false
	}
	close, ok := p.pairs[open]
	if !ok || close <= open {
		return false
	}
	bodyOpen := close + 1
	return bodyOpen < len(p.tokens) && p.tokens[bodyOpen].text == "{" && p.blockOpen[bodyOpen]
}

func (p *semicolonParser) parseGUIDeclaration(start, end int) int {
	control := start + 1
	open := control + 1
	close, ok := p.pairs[open]
	if !ok || close <= open {
		return end
	}
	bodyOpen := close + 1
	if bodyOpen < end && p.tokens[bodyOpen].text == "{" && p.blockOpen[bodyOpen] {
		return p.parseBlock(bodyOpen, end)
	}
	return bodyOpen
}

func (p *semicolonParser) isGUIBlockOpen(index int) bool {
	if index <= 0 || index >= len(p.tokens) || p.tokens[index].text != "{" || !p.blockOpen[index] {
		return false
	}
	close := index - 1
	if p.tokens[close].text != ")" {
		return false
	}
	open, ok := p.pairs[close]
	if !ok || open <= 1 {
		return false
	}
	control := open - 1
	newIndex := control - 1
	return p.tokens[control].kind == tokenIdentifier &&
		strings.HasPrefix(strings.ToLower(p.tokens[control].text), "gui") &&
		isIdentifierText(p.tokens[newIndex], "new")
}

func (p *semicolonParser) parseControl(start, end int) int {
	keyword := strings.ToLower(p.tokens[start].text)
	next := start + 1

	if keyword == "else" && next < end && isIdentifierText(p.tokens[next], "if") {
		return p.parseControl(next, end)
	}
	if keyword == "do" {
		bodyEnd := p.parseBody(next, end)
		if bodyEnd < end && isIdentifierText(p.tokens[bodyEnd], "while") {
			next = p.afterCondition(bodyEnd, end)
			if next < end && p.tokens[next].text == ";" {
				return next + 1
			}
			return next
		}
		return bodyEnd
	}

	next = p.afterCondition(next, end)
	return p.parseBody(next, end)
}

func (p *semicolonParser) afterCondition(start, end int) int {
	if start >= end || p.tokens[start].text != "(" {
		return start
	}
	if close, ok := p.pairs[start]; ok && close > start {
		return close + 1
	}
	return end
}

func (p *semicolonParser) parseBody(start, end int) int {
	if start >= end {
		return end
	}
	if p.tokens[start].text == "{" && p.blockOpen[start] {
		return p.parseBlock(start, end)
	}
	return p.parseStatement(start, end)
}

func (p *semicolonParser) parseBlock(open, end int) int {
	close, ok := p.pairs[open]
	if !ok || close <= open || close > end {
		p.parseSequence(open+1, end)
		return end
	}
	p.parseSequence(open+1, close)
	return close + 1
}

func (p *semicolonParser) isFunctionDeclarationStart(index int) bool {
	if isIdentifierText(p.tokens[index], "function") {
		return true
	}
	if p.tokens[index].kind != tokenIdentifier {
		return false
	}
	switch strings.ToLower(p.tokens[index].text) {
	case "public", "private", "protected", "static", "final":
		next := index + 1
		return next < len(p.tokens) && isIdentifierText(p.tokens[next], "function")
	default:
		return false
	}
}

func (p *semicolonParser) isControlKeyword(index int) bool {
	if p.tokens[index].kind != tokenIdentifier {
		return false
	}
	switch strings.ToLower(p.tokens[index].text) {
	case "if", "for", "while", "switch", "with", "else", "try", "catch", "finally", "do":
		return true
	default:
		return false
	}
}

func (p *semicolonParser) isCaseLabel(index int) bool {
	return isIdentifierText(p.tokens[index], "case") || isIdentifierText(p.tokens[index], "default")
}

func (p *semicolonParser) skipCaseLabel(start, end int) int {
	for index := start + 1; index < end; index++ {
		if p.tokens[index].text == ":" {
			return index + 1
		}
		if close, ok := p.pairs[index]; ok && close > index {
			index = close
		}
	}
	return end
}

func (p *semicolonParser) hasLineBreak(previous, current int) bool {
	return previous >= 0 && current < len(p.tokens) && p.tokens[previous].endPos.Line < p.tokens[current].startPos.Line
}

func (p *semicolonParser) canEndExpression(index int) bool {
	if index < 0 || index >= len(p.tokens) {
		return false
	}
	current := p.tokens[index]
	switch current.text {
	case ".", "::", "=", "==", "===", "!=", "!==", "<", "<=", ">", ">=", "&&", "||", "|", "&", "+", "-", "*", "/", "%", "@", "?", ":", ",", "(", "[", "{", "!", "~", "^":
		return false
	}
	if current.kind == tokenOperator && current.text != "++" && current.text != "--" {
		return false
	}
	return true
}

func (p *semicolonParser) startsNewStatement(index int) bool {
	if index < 0 || index >= len(p.tokens) {
		return false
	}
	current := p.tokens[index]
	switch current.text {
	case ".", "::", "(", "[", ")", "]", ",", ";":
		return false
	}
	if current.kind == tokenOperator || current.text == "?" || current.text == ":" {
		return false
	}
	return true
}

func (p *semicolonParser) addMissingSemicolon(current token) {
	p.missing = append(p.missing, Diagnostic{
		Range:    Range{Start: current.startPos, End: current.endPos},
		Severity: 1,
		Source:   "graalscript",
		Message:  "Expected ';' after statement.",
	})
}

func isIdentifierText(current token, value string) bool {
	return current.kind == tokenIdentifier && strings.EqualFold(current.text, value)
}

func unclosedDelimiterDiagnostic(frame delimiterFrame) Diagnostic {
	return Diagnostic{
		Range:    Range{Start: frame.token.startPos, End: frame.token.endPos},
		Severity: 1,
		Source:   "graalscript",
		Message:  fmt.Sprintf("Unclosed delimiter %q; expected %q.", frame.token.text, frame.close),
	}
}

func unexpectedDelimiterDiagnostic(current token) Diagnostic {
	return Diagnostic{
		Range:    Range{Start: current.startPos, End: current.endPos},
		Severity: 1,
		Source:   "graalscript",
		Message:  fmt.Sprintf("Unexpected closing delimiter %q.", current.text),
	}
}

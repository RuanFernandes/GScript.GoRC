package graalscript

import (
	"path/filepath"
	"strconv"
	"strings"
)

type nodeKind string

const (
	nodeDocument   nodeKind = "document"
	nodeFunction   nodeKind = "function"
	nodeBlock      nodeKind = "block"
	nodeCall       nodeKind = "call"
	nodeAssignment nodeKind = "assignment"
	nodeWith       nodeKind = "with"
	nodeJoin       nodeKind = "join"
)

const (
	scriptSideServer = "server"
	scriptSideClient = "client"
)

// ASTNode is intentionally small in v1. The parser keeps enough structure for
// semantic queries while accepting incomplete code typed in an editor.
type ASTNode struct {
	Kind     nodeKind
	Name     string
	Range    Range
	Children []ASTNode
}

type FunctionSymbol struct {
	Name           string
	Params         []string
	Range          Range
	SelectionRange Range
	BodyRange      Range
	ReturnType     string
	Documentation  string
	Visibility     string
	Public         bool
	Private        bool
	Side           string
}

type VariableSymbol struct {
	Name               string
	Scope              string
	Side               string
	Type               string
	Value              string
	Values             []string
	DynamicExpressions []string
	Range              Range
	SelectionRange     Range
	Detail             string
	FunctionRange      Range
}

type WithBlock struct {
	Range    Range
	Receiver string
}

type Document struct {
	URI          string
	Text         string
	Version      int
	Tokens       []token
	AST          ASTNode
	Functions    []FunctionSymbol
	Variables    []VariableSymbol
	Members      []VariableSymbol
	Joins        []string
	Imports      []string
	With         []WithBlock
	LineStarts   []int
	ClientOffset int
}

func parseDocument(uri, text string, version int) *Document {
	doc := &Document{
		URI: uri, Text: text, Version: version, Tokens: lex(text),
		AST:          ASTNode{Kind: nodeDocument, Range: Range{Start: Position{}, End: endPosition(text)}},
		LineStarts:   lineStarts(text),
		ClientOffset: clientSideOffset(text),
	}
	doc.parseFunctions()
	doc.parseJoinsAndVariables()
	doc.parseAssignments()
	doc.parseForEachVariables()
	doc.parseDynamicAccesses()
	doc.parseDynamicAssignments()
	doc.parseWithBlocks()
	doc.parseCalls()
	return doc
}

func (d *Document) parseFunctions() {
	for i := 0; i < len(d.Tokens); i++ {
		if !isIdentifier(d.Tokens[i], "function") {
			continue
		}
		nameIndex := nextSignificant(d.Tokens, i+1)
		if nameIndex < 0 || d.Tokens[nameIndex].kind != tokenIdentifier {
			continue
		}
		visibility := "implicit"
		if modifier := previousSignificant(d.Tokens, i-1); modifier >= 0 && d.Tokens[modifier].kind == tokenIdentifier {
			switch strings.ToLower(d.Tokens[modifier].text) {
			case "public":
				visibility = "public"
			case "private":
				visibility = "private"
			}
		}
		openIndex := nextSignificant(d.Tokens, nameIndex+1)
		if openIndex < 0 || d.Tokens[openIndex].text != "(" {
			continue
		}
		closeIndex := matchingToken(d.Tokens, openIndex, "(", ")")
		if closeIndex < 0 {
			closeIndex = len(d.Tokens) - 1
		}
		params := parseParameterNames(d.Tokens, openIndex+1, closeIndex)
		bodyOpen := nextSignificant(d.Tokens, closeIndex+1)
		bodyClose := bodyOpen
		if bodyOpen >= 0 && d.Tokens[bodyOpen].text == "{" {
			bodyClose = matchingToken(d.Tokens, bodyOpen, "{", "}")
			if bodyClose < 0 {
				bodyClose = len(d.Tokens) - 1
			}
		}
		nameToken := d.Tokens[nameIndex]
		bodyRange := Range{Start: nameToken.endPos, End: nameToken.endPos}
		if bodyOpen >= 0 && bodyOpen < len(d.Tokens) {
			endToken := d.Tokens[bodyClose]
			bodyRange = Range{Start: d.Tokens[bodyOpen].startPos, End: endToken.endPos}
		}
		fn := FunctionSymbol{
			Name: nameToken.text, Params: params,
			Range:          Range{Start: d.Tokens[i].startPos, End: bodyRange.End},
			SelectionRange: Range{Start: nameToken.startPos, End: nameToken.endPos},
			BodyRange:      bodyRange,
			Visibility:     visibility,
			Public:         visibility == "public",
			Private:        visibility == "private",
			Side:           d.sideAtOffset(nameToken.start),
		}
		d.Functions = append(d.Functions, fn)
		node := ASTNode{Kind: nodeFunction, Name: fn.Name, Range: fn.Range}
		if bodyOpen >= 0 && bodyOpen < len(d.Tokens) {
			node.Children = append(node.Children, ASTNode{Kind: nodeBlock, Range: bodyRange})
		}
		d.AST.Children = append(d.AST.Children, node)
	}
}

func (d *Document) parseJoinsAndVariables() {
	for i := 0; i < len(d.Tokens); i++ {
		tok := d.Tokens[i]
		if tok.kind == tokenComment || tok.kind == tokenEOF {
			continue
		}
		if isIdentifier(tok, "join") {
			open := nextSignificant(d.Tokens, i+1)
			if open >= 0 && d.Tokens[open].text == "(" {
				value := nextSignificant(d.Tokens, open+1)
				if value >= 0 && (d.Tokens[value].kind == tokenString || d.Tokens[value].kind == tokenIdentifier) {
					className := tokenStringValue(d.Tokens[value])
					if className != "" {
						d.Joins = appendUnique(d.Joins, className)
						d.AST.Children = append(d.AST.Children, ASTNode{Kind: nodeJoin, Name: className, Range: Range{Start: tok.startPos, End: d.Tokens[value].endPos}})
					}
				}
			}
		}
		if isIdentifier(tok, "import") {
			value := nextSignificant(d.Tokens, i+1)
			if value >= 0 && (d.Tokens[value].kind == tokenString || d.Tokens[value].kind == tokenIdentifier) {
				className := tokenStringValue(d.Tokens[value])
				if className != "" {
					d.Imports = appendUnique(d.Imports, className)
					d.AST.Children = append(d.AST.Children, ASTNode{Kind: nodeJoin, Name: className, Range: Range{Start: tok.startPos, End: d.Tokens[value].endPos}})
				}
			}
		}
		if tok.kind != tokenIdentifier {
			continue
		}
		next := nextSignificant(d.Tokens, i+1)
		if next < 0 || d.Tokens[next].text != "." {
			continue
		}
		scope := strings.ToLower(tok.text)
		if !isDynamicVariableScope(scope) {
			continue
		}
		member := nextSignificant(d.Tokens, next+1)
		switch {
		case member >= 0 && d.Tokens[member].kind == tokenIdentifier:
			detail := scope + " member"
			if scope != "temp" && scope != "this" && scope != "thiso" {
				detail = scope + " property"
			}
			d.addVariableSymbol(
				scope,
				d.Tokens[member].text,
				Range{Start: tok.startPos, End: d.Tokens[member].endPos},
				Range{Start: d.Tokens[member].startPos, End: d.Tokens[member].endPos},
				detail,
			)
		case member >= 0 && d.Tokens[member].text == "(":
			close := matchingToken(d.Tokens, member, "(", ")")
			if close < 0 {
				continue
			}
			name, ok := d.dynamicPropertyName(member+1, close, tok.startPos)
			if !ok {
				continue
			}
			d.addDynamicVariableSymbol(
				scope,
				name,
				tokenTextBetween(d.Text, d.Tokens, member+1, close),
				Range{Start: tok.startPos, End: d.Tokens[close].endPos},
				Range{Start: d.Tokens[member].startPos, End: d.Tokens[close].endPos},
				scope+" dynamic variable",
			)
		}
	}
}

func (d *Document) addVariableSymbol(scope, name string, symbolRange, selectionRange Range, detail string) {
	scope = strings.ToLower(strings.TrimSpace(scope))
	name = strings.TrimSpace(name)
	if name == "" || !isDynamicVariableScope(scope) {
		return
	}
	side := d.sideAtOffset(offsetAt(d.Text, symbolRange.Start))
	if scope == "temp" {
		functionRange := Range{}
		if fn := functionAt(d, symbolRange.Start); fn != nil {
			functionRange = fn.BodyRange
		}
		for _, variable := range d.Variables {
			if strings.EqualFold(variable.Scope, scope) &&
				strings.EqualFold(variable.Name, name) &&
				variable.FunctionRange == functionRange {
				return
			}
		}
		d.Variables = append(d.Variables, VariableSymbol{
			Name: name, Scope: scope, Side: side,
			Range: symbolRange, SelectionRange: selectionRange,
			Detail: detail, FunctionRange: functionRange,
		})
		return
	}
	for _, member := range d.Members {
		if strings.EqualFold(member.Scope, scope) &&
			strings.EqualFold(member.Name, name) &&
			memberAvailableInSide(member, side) {
			return
		}
	}
	d.Members = append(d.Members, VariableSymbol{
		Name: name, Scope: scope, Side: side,
		Range: symbolRange, SelectionRange: selectionRange,
		Detail: detail,
	})
}

func (d *Document) addDynamicVariableSymbol(scope, name, expression string, symbolRange, selectionRange Range, detail string) {
	d.addVariableSymbol(scope, name, symbolRange, selectionRange, detail)
	if strings.TrimSpace(expression) == "" {
		return
	}
	symbol := d.symbolFor(scope, name, symbolRange.Start)
	if symbol == nil {
		return
	}
	symbol.DynamicExpressions = appendUnique(symbol.DynamicExpressions, strings.TrimSpace(expression))
}

func isDynamicVariableScope(scope string) bool {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "temp", "this", "thiso", "player", "client", "clientr", "server", "serverr":
		return true
	default:
		return false
	}
}

type inferredValue struct {
	Type   string
	Value  string
	Values []string
}

func (d *Document) parseAssignments() {
	for i := 0; i < len(d.Tokens); i++ {
		if d.Tokens[i].kind != tokenIdentifier {
			continue
		}
		dot := nextSignificant(d.Tokens, i+1)
		if dot < 0 || d.Tokens[dot].text != "." {
			continue
		}
		member := nextSignificant(d.Tokens, dot+1)
		if member < 0 || d.Tokens[member].kind != tokenIdentifier {
			continue
		}
		scope := strings.ToLower(d.Tokens[i].text)
		if !isDynamicVariableScope(scope) {
			continue
		}
		equal := nextSignificant(d.Tokens, member+1)
		if equal < 0 || d.Tokens[equal].text != "=" {
			continue
		}
		valueStart := nextSignificant(d.Tokens, equal+1)
		if valueStart < 0 {
			continue
		}

		target := d.symbolFor(scope, d.Tokens[member].text, d.Tokens[i].startPos)
		if target == nil {
			continue
		}
		inferred := d.inferExpression(valueStart, d.Tokens[i].startPos)
		if inferred.Type == "" {
			continue
		}
		applyInferredValue(target, inferred)
	}
}

func (d *Document) parseDynamicAssignments() {
	for i := 0; i < len(d.Tokens); i++ {
		if d.Tokens[i].kind != tokenIdentifier {
			continue
		}
		scope := strings.ToLower(d.Tokens[i].text)
		if !isDynamicVariableScope(scope) {
			continue
		}
		dot := nextSignificant(d.Tokens, i+1)
		if dot < 0 || d.Tokens[dot].text != "." {
			continue
		}
		open := nextSignificant(d.Tokens, dot+1)
		if open < 0 || d.Tokens[open].text != "(" {
			continue
		}
		close := matchingToken(d.Tokens, open, "(", ")")
		if close < 0 {
			continue
		}
		names, ok := d.dynamicPropertyNames(open+1, close, d.Tokens[i].startPos)
		if !ok {
			continue
		}
		equal := nextSignificant(d.Tokens, close+1)
		if equal < 0 || d.Tokens[equal].text != "=" {
			continue
		}
		valueStart := nextSignificant(d.Tokens, equal+1)
		if valueStart < 0 {
			continue
		}
		inferred := d.inferExpression(valueStart, d.Tokens[i].startPos)
		if inferred.Type == "" {
			continue
		}
		for _, name := range names {
			d.addDynamicVariableSymbol(
				scope,
				name,
				tokenTextBetween(d.Text, d.Tokens, open+1, close),
				Range{Start: d.Tokens[i].startPos, End: d.Tokens[close].endPos},
				Range{Start: d.Tokens[open].startPos, End: d.Tokens[close].endPos},
				scope+" dynamic variable",
			)
			target := d.symbolFor(scope, name, d.Tokens[i].startPos)
			if target != nil {
				applyInferredValue(target, inferred)
			}
		}
	}
}

func applyInferredValue(target *VariableSymbol, inferred inferredValue) {
	target.Type = inferred.Type
	target.Value = inferred.Value
	target.Values = append([]string(nil), inferred.Values...)
}

func (d *Document) parseForEachVariables() {
	for i := 0; i < len(d.Tokens); i++ {
		if !isIdentifier(d.Tokens[i], "for") {
			continue
		}
		open := nextSignificant(d.Tokens, i+1)
		if open < 0 || d.Tokens[open].text != "(" {
			continue
		}
		close := matchingToken(d.Tokens, open, "(", ")")
		if close < 0 {
			continue
		}
		colon := -1
		for j := open + 1; j < close; j++ {
			if d.Tokens[j].kind != tokenComment && d.Tokens[j].text == ":" {
				colon = j
				break
			}
		}
		if colon < 0 {
			continue
		}
		targetStart := nextSignificantBefore(d.Tokens, open+1, colon)
		if targetStart < 0 || d.Tokens[targetStart].kind != tokenIdentifier {
			continue
		}
		targetDot := nextSignificantBefore(d.Tokens, targetStart+1, colon)
		targetMember := nextSignificantBefore(d.Tokens, targetDot+1, colon)
		if targetDot < 0 || d.Tokens[targetDot].text != "." ||
			targetMember < 0 || d.Tokens[targetMember].kind != tokenIdentifier {
			continue
		}
		scope := strings.ToLower(d.Tokens[targetStart].text)
		if !isDynamicVariableScope(scope) {
			continue
		}
		sourceStart := nextSignificantBefore(d.Tokens, colon+1, close)
		if sourceStart < 0 {
			continue
		}
		source := d.symbolReference(sourceStart, close, d.Tokens[i].startPos)
		if source == nil || len(source.Values) == 0 && source.Value == "" {
			continue
		}
		d.addVariableSymbol(
			scope,
			d.Tokens[targetMember].text,
			Range{Start: d.Tokens[targetStart].startPos, End: d.Tokens[targetMember].endPos},
			Range{Start: d.Tokens[targetMember].startPos, End: d.Tokens[targetMember].endPos},
			scope+" foreach variable",
		)
		target := d.symbolFor(scope, d.Tokens[targetMember].text, d.Tokens[targetStart].startPos)
		if target == nil {
			continue
		}
		target.Type = "string"
		target.Values = append([]string(nil), source.Values...)
		if len(target.Values) == 0 && source.Value != "" {
			target.Values = []string{source.Value}
		}
		if len(target.Values) == 1 {
			target.Value = target.Values[0]
		} else {
			target.Value = ""
		}
	}
}

func (d *Document) symbolReference(start, end int, position Position) *VariableSymbol {
	if start < 0 || start >= end || start >= len(d.Tokens) {
		return nil
	}
	if d.Tokens[start].kind != tokenIdentifier {
		return nil
	}
	dot := nextSignificantBefore(d.Tokens, start+1, end)
	if dot < 0 || d.Tokens[dot].text != "." {
		return nil
	}
	member := nextSignificantBefore(d.Tokens, dot+1, end)
	if member < 0 {
		return nil
	}
	scope := strings.ToLower(d.Tokens[start].text)
	name := ""
	if d.Tokens[member].kind == tokenIdentifier {
		name = d.Tokens[member].text
	} else if d.Tokens[member].text == "(" {
		close := matchingToken(d.Tokens, member, "(", ")")
		if close < 0 || close >= end {
			return nil
		}
		resolved, ok := d.dynamicPropertyName(member+1, close, position)
		if !ok {
			return nil
		}
		name = resolved
	} else {
		return nil
	}
	return d.symbolFor(scope, name, position)
}

func (d *Document) parseDynamicAccesses() {
	for i := 0; i < len(d.Tokens); i++ {
		if d.Tokens[i].kind != tokenIdentifier || !isDynamicVariableScope(d.Tokens[i].text) {
			continue
		}
		dot := nextSignificant(d.Tokens, i+1)
		if dot < 0 || d.Tokens[dot].text != "." {
			continue
		}
		open := nextSignificant(d.Tokens, dot+1)
		if open < 0 || d.Tokens[open].text != "(" {
			continue
		}
		close := matchingToken(d.Tokens, open, "(", ")")
		if close < 0 {
			continue
		}
		names, ok := d.dynamicPropertyNames(open+1, close, d.Tokens[i].startPos)
		if !ok {
			continue
		}
		for _, name := range names {
			d.addDynamicVariableSymbol(
				strings.ToLower(d.Tokens[i].text),
				name,
				tokenTextBetween(d.Text, d.Tokens, open+1, close),
				Range{Start: d.Tokens[i].startPos, End: d.Tokens[close].endPos},
				Range{Start: d.Tokens[open].startPos, End: d.Tokens[close].endPos},
				strings.ToLower(d.Tokens[i].text)+" dynamic variable",
			)
		}
	}
}

func (d *Document) inferExpression(start int, position Position) inferredValue {
	if start < 0 || start >= len(d.Tokens) {
		return inferredValue{}
	}
	tok := d.Tokens[start]
	if tok.kind == tokenString {
		return inferredValue{Type: "string", Value: tokenStringValue(tok)}
	}
	if tok.kind == tokenNumber {
		return inferredValue{Type: "number", Value: tok.text}
	}
	if tok.text == "{" || tok.text == "[" {
		return inferredValue{Type: "array", Values: d.literalValues(start, position)}
	}
	if tok.kind != tokenIdentifier {
		return inferredValue{}
	}
	switch strings.ToLower(tok.text) {
	case "true", "false":
		return inferredValue{Type: "bool", Value: strings.ToLower(tok.text)}
	case "nil", "null":
		return inferredValue{Type: "nil", Value: strings.ToLower(tok.text)}
	}
	if isNPCFinder(tok.text) {
		open := nextSignificant(d.Tokens, start+1)
		if open < 0 || d.Tokens[open].text != "(" {
			return inferredValue{}
		}
		close := matchingToken(d.Tokens, open, "(", ")")
		if close < 0 {
			close = len(d.Tokens)
		}
		argument := nextSignificant(d.Tokens, open+1)
		if argument < 0 || argument >= close {
			return inferredValue{Type: "npc"}
		}
		return inferredValue{Type: "npc", Value: d.stringValue(argument, close, position)}
	}
	switch strings.ToLower(tok.text) {
	case "findplayer", "findplayer2", "findplayerbyid":
		return inferredValue{Type: "player"}
	case "findweapon":
		return inferredValue{Type: "weapon", Value: d.stringValueFromCall(start, position)}
	case "findlevel":
		return inferredValue{Type: "level", Value: d.stringValueFromCall(start, position)}
	}

	dot := nextSignificant(d.Tokens, start+1)
	if dot < 0 || d.Tokens[dot].text != "." {
		return inferredValue{}
	}
	member := nextSignificant(d.Tokens, dot+1)
	if member < 0 {
		return inferredValue{}
	}
	scope := strings.ToLower(tok.text)
	if !isDynamicVariableScope(scope) {
		return inferredValue{}
	}
	name := d.Tokens[member].text
	if d.Tokens[member].text == "(" {
		close := matchingToken(d.Tokens, member, "(", ")")
		if close < 0 {
			return inferredValue{}
		}
		resolved, ok := d.dynamicPropertyName(member+1, close, position)
		if !ok {
			return inferredValue{}
		}
		name = resolved
	}
	source := d.symbolFor(scope, name, position)
	if source == nil {
		return inferredValue{}
	}
	return inferredValue{Type: source.Type, Value: source.Value, Values: append([]string(nil), source.Values...)}
}

func (d *Document) literalValues(start int, position Position) []string {
	if start < 0 || start >= len(d.Tokens) {
		return nil
	}
	open, close := d.Tokens[start].text, ""
	switch open {
	case "{":
		close = "}"
	case "[":
		close = "]"
	default:
		return nil
	}
	end := matchingToken(d.Tokens, start, open, close)
	if end < 0 {
		return nil
	}
	values := []string{}
	elementStart := start + 1
	parenDepth, bracketDepth, braceDepth := 0, 0, 0
	for i := start + 1; i <= end; i++ {
		if i == end || (d.Tokens[i].text == "," && parenDepth == 0 && bracketDepth == 0 && braceDepth == 0) {
			if elementValues, ok := d.literalElementValues(elementStart, i, position); ok {
				values = appendUniqueStrings(values, elementValues...)
			}
			elementStart = i + 1
			continue
		}
		switch d.Tokens[i].text {
		case "(":
			parenDepth++
		case ")":
			parenDepth--
		case "[":
			bracketDepth++
		case "]":
			bracketDepth--
		case "{":
			braceDepth++
		case "}":
			braceDepth--
		}
	}
	return values
}

func (d *Document) literalElementValues(start, end int, position Position) ([]string, bool) {
	first := nextSignificantBefore(d.Tokens, start, end)
	if first < 0 {
		return nil, false
	}
	last := nextSignificantBefore(d.Tokens, first+1, end)
	if last < 0 {
		switch {
		case d.Tokens[first].kind == tokenString:
			return []string{tokenStringValue(d.Tokens[first])}, true
		case d.Tokens[first].kind == tokenNumber:
			return []string{d.Tokens[first].text}, true
		case d.Tokens[first].kind == tokenIdentifier &&
			(strings.EqualFold(d.Tokens[first].text, "true") || strings.EqualFold(d.Tokens[first].text, "false")):
			return []string{strings.ToLower(d.Tokens[first].text)}, true
		}
	}
	if values, ok := d.dynamicPropertyNamesInTokens(d.Tokens, start, end, position); ok {
		return values, true
	}
	return nil, false
}

func appendUniqueStrings(items []string, values ...string) []string {
	for _, value := range values {
		if value == "" {
			continue
		}
		found := false
		for _, item := range items {
			if item == value {
				found = true
				break
			}
		}
		if !found {
			items = append(items, value)
		}
	}
	return items
}

func (d *Document) stringValue(start, end int, position Position) string {
	if start < 0 || start >= len(d.Tokens) || start >= end {
		return ""
	}
	if d.Tokens[start].kind == tokenString {
		return tokenStringValue(d.Tokens[start])
	}
	if d.Tokens[start].kind != tokenIdentifier {
		return ""
	}
	dot := nextSignificant(d.Tokens, start+1)
	if dot < 0 || dot >= end || d.Tokens[dot].text != "." {
		return ""
	}
	member := nextSignificant(d.Tokens, dot+1)
	if member < 0 || member >= end {
		return ""
	}
	scope := strings.ToLower(d.Tokens[start].text)
	if !isDynamicVariableScope(scope) {
		return ""
	}
	name := ""
	if d.Tokens[member].kind == tokenIdentifier {
		name = d.Tokens[member].text
	} else if d.Tokens[member].text == "(" {
		close := matchingToken(d.Tokens, member, "(", ")")
		if close < 0 || close >= end {
			return ""
		}
		resolved, ok := d.dynamicPropertyName(member+1, close, position)
		if !ok {
			return ""
		}
		name = resolved
	} else {
		return ""
	}
	source := d.symbolFor(scope, name, position)
	if source == nil || source.Type != "string" {
		return ""
	}
	return source.Value
}

func (d *Document) stringValueFromCall(start int, position Position) string {
	open := nextSignificant(d.Tokens, start+1)
	if open < 0 || d.Tokens[open].text != "(" {
		return ""
	}
	close := matchingToken(d.Tokens, open, "(", ")")
	if close < 0 {
		close = len(d.Tokens)
	}
	argument := nextSignificant(d.Tokens, open+1)
	if argument < 0 || argument >= close {
		return ""
	}
	return d.stringValue(argument, close, position)
}

const maxDynamicCandidates = 128

func (d *Document) dynamicPropertyName(start, end int, position Position) (string, bool) {
	return d.dynamicPropertyNameInTokens(d.Tokens, start, end, position)
}

func (d *Document) dynamicPropertyNameInTokens(tokens []token, start, end int, position Position) (string, bool) {
	names, ok := d.dynamicPropertyNamesInTokens(tokens, start, end, position)
	if !ok || len(names) != 1 {
		return "", false
	}
	return names[0], true
}

func (d *Document) dynamicPropertyNames(start, end int, position Position) ([]string, bool) {
	return d.dynamicPropertyNamesInTokens(d.Tokens, start, end, position)
}

func (d *Document) dynamicPropertyNamesInTokens(tokens []token, start, end int, position Position) ([]string, bool) {
	if start < 0 || start >= end || end > len(tokens) {
		return nil, false
	}
	return d.dynamicExpressionValuesInTokens(tokens, start, end, position)
}

func (d *Document) dynamicExpressionValue(start, end int, position Position) (string, bool) {
	return d.dynamicExpressionValueInTokens(d.Tokens, start, end, position)
}

func (d *Document) dynamicExpressionValueInTokens(tokens []token, start, end int, position Position) (string, bool) {
	values, ok := d.dynamicExpressionValuesInTokens(tokens, start, end, position)
	if !ok || len(values) != 1 {
		return "", false
	}
	return values[0], true
}

func (d *Document) dynamicExpressionValuesInTokens(tokens []token, start, end int, position Position) ([]string, bool) {
	terms := [][]string{}
	for index := nextSignificantBefore(tokens, start, end); index >= 0; {
		tok := tokens[index]
		if tok.text == "@" {
			index = nextSignificantBefore(tokens, index+1, end)
			continue
		}
		var termValues []string
		switch {
		case tok.kind == tokenString:
			termValues = []string{tokenStringValue(tok)}
			index++
		case tok.kind == tokenIdentifier:
			dot := nextSignificantBefore(tokens, index+1, end)
			if dot < 0 || tokens[dot].text != "." {
				return nil, false
			}
			member := nextSignificantBefore(tokens, dot+1, end)
			if member < 0 || tokens[member].kind != tokenIdentifier {
				return nil, false
			}
			scope := strings.ToLower(tok.text)
			if !isDynamicVariableScope(scope) {
				return nil, false
			}
			symbol := d.symbolFor(scope, tokens[member].text, position)
			if symbol == nil {
				return nil, false
			}
			termValues = append([]string(nil), symbol.Values...)
			if len(termValues) == 0 && symbol.Value != "" {
				termValues = []string{symbol.Value}
			}
			if len(termValues) == 0 {
				return nil, false
			}
			index = member + 1
		default:
			return nil, false
		}
		terms = append(terms, termValues)
		next := nextSignificantBefore(tokens, index, end)
		if next < 0 {
			break
		}
		if tokens[next].text != "@" {
			return nil, false
		}
		index = next + 1
	}
	if len(terms) == 0 {
		return nil, false
	}
	values := []string{""}
	for _, termValues := range terms {
		nextValues := make([]string, 0, len(values)*len(termValues))
		for _, prefix := range values {
			for _, term := range termValues {
				nextValues = appendUniqueStrings(nextValues, prefix+term)
				if len(nextValues) >= maxDynamicCandidates {
					break
				}
			}
			if len(nextValues) >= maxDynamicCandidates {
				break
			}
		}
		values = nextValues
	}
	return values, len(values) > 0
}

func nextSignificantBefore(tokens []token, start, end int) int {
	if start < 0 {
		start = 0
	}
	if end > len(tokens) {
		end = len(tokens)
	}
	for i := start; i < end; i++ {
		if tokens[i].kind != tokenComment && tokens[i].kind != tokenEOF {
			return i
		}
	}
	return -1
}

func (d *Document) symbolFor(scope, name string, position Position) *VariableSymbol {
	scope = strings.ToLower(strings.TrimSpace(scope))
	name = strings.ToLower(strings.TrimSpace(name))
	if scope == "temp" {
		currentFunction := functionAt(d, position)
		if currentFunction == nil {
			return nil
		}
		for i := range d.Variables {
			variable := &d.Variables[i]
			if strings.EqualFold(variable.Scope, scope) && strings.EqualFold(variable.Name, name) && variable.FunctionRange == currentFunction.BodyRange {
				return variable
			}
		}
		return nil
	}
	if !isDynamicVariableScope(scope) || scope == "temp" {
		return nil
	}
	side := d.sideAtOffset(offsetAt(d.Text, position))
	for i := range d.Members {
		member := &d.Members[i]
		if strings.EqualFold(member.Scope, scope) && strings.EqualFold(member.Name, name) && memberAvailableInSide(*member, side) {
			return member
		}
	}
	return nil
}

func (d *Document) parseWithBlocks() {
	for i := 0; i < len(d.Tokens); i++ {
		if !isIdentifier(d.Tokens[i], "with") {
			continue
		}
		open := nextSignificant(d.Tokens, i+1)
		if open < 0 || d.Tokens[open].text != "(" {
			continue
		}
		close := matchingToken(d.Tokens, open, "(", ")")
		if close < 0 {
			continue
		}
		bodyOpen := nextSignificant(d.Tokens, close+1)
		if bodyOpen < 0 || d.Tokens[bodyOpen].text != "{" {
			continue
		}
		bodyClose := matchingToken(d.Tokens, bodyOpen, "{", "}")
		if bodyClose < 0 {
			bodyClose = len(d.Tokens) - 1
		}
		receiver := tokenTextBetween(d.Text, d.Tokens, open+1, close)
		block := WithBlock{
			Range:    Range{Start: d.Tokens[bodyOpen].startPos, End: d.Tokens[bodyClose].endPos},
			Receiver: strings.TrimSpace(receiver),
		}
		d.With = append(d.With, block)
		d.AST.Children = append(d.AST.Children, ASTNode{Kind: nodeWith, Name: block.Receiver, Range: block.Range})
	}
}

func (d *Document) parseCalls() {
	for i := 0; i < len(d.Tokens); i++ {
		if d.Tokens[i].kind != tokenIdentifier {
			continue
		}
		open := nextSignificant(d.Tokens, i+1)
		if open < 0 || d.Tokens[open].text != "(" {
			continue
		}
		d.AST.Children = append(d.AST.Children, ASTNode{
			Kind: nodeCall, Name: d.Tokens[i].text,
			Range: Range{Start: d.Tokens[i].startPos, End: d.Tokens[open].endPos},
		})
	}
}

func parseParameterNames(tokens []token, start, end int) []string {
	params := []string{}
	for i := start; i < end; i++ {
		if tokens[i].kind != tokenIdentifier {
			continue
		}
		if i > start && tokens[i-1].text == "." {
			continue
		}
		params = appendUnique(params, tokens[i].text)
	}
	return params
}

func nextSignificant(tokens []token, start int) int {
	for i := start; i < len(tokens); i++ {
		if tokens[i].kind != tokenComment && tokens[i].kind != tokenEOF {
			return i
		}
	}
	return -1
}

func previousSignificant(tokens []token, start int) int {
	for i := start; i >= 0; i-- {
		if tokens[i].kind != tokenComment && tokens[i].kind != tokenEOF {
			return i
		}
	}
	return -1
}

func matchingToken(tokens []token, start int, open, close string) int {
	depth := 0
	for i := start; i < len(tokens); i++ {
		if tokens[i].text == open {
			depth++
		}
		if tokens[i].text == close {
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func isIdentifier(tok token, value string) bool {
	return tok.kind == tokenIdentifier && strings.EqualFold(tok.text, value)
}

func tokenStringValue(tok token) string {
	value := strings.TrimSpace(tok.text)
	if tok.kind == tokenString {
		if decoded, err := strconv.Unquote(value); err == nil {
			return decoded
		}
		value = strings.Trim(value, `"`)
	}
	return strings.TrimSpace(value)
}

func appendUnique(items []string, value string) []string {
	for _, item := range items {
		if strings.EqualFold(item, value) {
			return items
		}
	}
	return append(items, value)
}

func tokenTextBetween(text string, tokens []token, start, end int) string {
	if start >= end || start < 0 || end > len(tokens) {
		return ""
	}
	return text[tokens[start].start:tokens[end-1].end]
}

func lineStarts(text string) []int {
	starts := []int{0}
	for offset, r := range text {
		if r == '\n' {
			starts = append(starts, offset+1)
		}
	}
	return starts
}

func endPosition(text string) Position {
	starts := lineStarts(text)
	line := len(starts) - 1
	last := text[starts[line]:]
	return Position{Line: line, Character: utf16Length(last)}
}

func clientSideOffset(text string) int {
	offset := 0
	for offset <= len(text) {
		end := strings.IndexByte(text[offset:], '\n')
		if end < 0 {
			end = len(text)
		} else {
			end += offset
		}
		line := strings.TrimSpace(strings.TrimPrefix(text[offset:end], "\ufeff"))
		if strings.HasPrefix(strings.ToLower(line), "//#clientside") {
			return offset
		}
		if end >= len(text) {
			break
		}
		offset = end + 1
	}
	return -1
}

func (d *Document) sideAtOffset(offset int) string {
	if d.ClientOffset >= 0 && offset >= d.ClientOffset {
		return scriptSideClient
	}
	return scriptSideServer
}

func utf16Length(text string) int {
	length := 0
	for _, r := range text {
		if r == '\n' {
			break
		}
		if r > 0xffff {
			length += 2
		} else {
			length++
		}
	}
	return length
}

func pathStem(uri string) string {
	base := filepath.Base(uri)
	if idx := strings.LastIndexByte(base, '.'); idx >= 0 {
		base = base[:idx]
	}
	return base
}

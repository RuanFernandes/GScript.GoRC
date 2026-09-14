package graalscript

import (
	"fmt"
	"strings"
	"testing"
)

func largeScriptForBenchmark() string {
	var source strings.Builder
	source.Grow(100_000)
	for i := 0; i < 3999; i++ {
		fmt.Fprintf(&source, "temp.value%d = %d;\n", i, i)
	}
	source.WriteString("temp.missing = 1")
	return source.String()
}

func largeMemberScriptForBenchmark() string {
	var source strings.Builder
	source.Grow(100_000)
	source.WriteString("function onCreated() {\n")
	for i := 0; i < 3997; i++ {
		fmt.Fprintf(&source, "this.value%d = %d;\n", i, i)
	}
	source.WriteString("this.missing = 1\n}\n")
	return source.String()
}

func BenchmarkParseDocumentLargeScript(b *testing.B) {
	text := largeScriptForBenchmark()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = parseDocument("memory://large", text, i)
	}
}

func BenchmarkParseDocumentLargeMemberScript(b *testing.B) {
	text := largeMemberScriptForBenchmark()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = parseDocument("memory://large-members", text, i)
	}
}

func BenchmarkDiagnosticsLargeScript(b *testing.B) {
	doc := parseDocument("memory://large", largeScriptForBenchmark(), 1)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = doc.diagnostics()
	}
}

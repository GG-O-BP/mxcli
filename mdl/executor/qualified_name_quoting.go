// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"strings"

	antlr "github.com/antlr4-go/antlr/v4"
	"github.com/mendixlabs/mxcli/mdl/grammar/parser"
)

// quoteQualifiedName renders a dotted model reference so it re-parses as a
// qualifiedName, quoting only the segments that actually need it.
//
// This is deliberately *not* mdlIdent. mdlIdent quotes anything that does not
// lex as a bare IDENTIFIER, which is right where the grammar wants an
// IDENTIFIER — but a qualifiedName segment is an `identifierOrKeyword`, and that
// rule accepts keywords unquoted. Most short Atlas icon names collide with an
// MDL keyword (`home`, `user`, `add`, `folder` are all keyword tokens), so
// mdlIdent would quote nearly every icon and bury the reference in punctuation.
// What genuinely needs quoting is a shape the rule cannot accept at all —
// notably the hyphenated Atlas names (`align-center`), which lex as
// HYPHENATED_ID.
//
// Acceptance is decided by running the real parser rather than by a hardcoded
// keyword list, so it stays correct as the grammar's keyword set moves.
func quoteQualifiedName(name string) string {
	parts := strings.Split(name, ".")
	for i, p := range parts {
		if !parsesAsIdentifierOrKeyword(p) {
			// QUOTED_IDENTIFIER is '"' ~["\r\n]* '"' — no escape sequence, and
			// Mendix names never contain a double quote.
			parts[i] = `"` + p + `"`
		}
	}
	return strings.Join(parts, ".")
}

// parsesAsIdentifierOrKeyword reports whether s is accepted whole by the
// identifierOrKeyword rule — i.e. whether it can be written unquoted inside a
// qualifiedName.
func parsesAsIdentifierOrKeyword(s string) bool {
	if s == "" {
		return false
	}
	lexer := parser.NewMDLLexer(antlr.NewInputStream(s))
	lexer.RemoveErrorListeners()
	stream := antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel)
	p := parser.NewMDLParser(stream)
	p.RemoveErrorListeners()
	errs := &countingErrorListener{}
	p.AddErrorListener(errs)
	ctx := p.IdentifierOrKeyword()
	if errs.count > 0 || ctx == nil {
		return false
	}
	// The rule must have consumed the whole string; a trailing remainder means
	// only a prefix matched.
	return ctx.GetStop() != nil && ctx.GetStop().GetStop() == len(s)-1
}

// countingErrorListener counts syntax errors without printing them.
type countingErrorListener struct {
	*antlr.DefaultErrorListener
	count int
}

func (l *countingErrorListener) SyntaxError(_ antlr.Recognizer, _ interface{},
	_, _ int, _ string, _ antlr.RecognitionException) {
	l.count++
}

// SPDX-License-Identifier: Apache-2.0

package visitor

import (
	"github.com/antlr4-go/antlr/v4"
	"github.com/mendixlabs/mxcli/mdl/ast"
	"github.com/mendixlabs/mxcli/mdl/grammar/parser"
)

// ParseXPathConstraint parses a raw XPath constraint string — including the outer
// [ ] brackets stored by Mendix in the XPathConstraint BSON field — and returns the
// AST expression. Returns (nil, false) if the input cannot be parsed (e.g. empty,
// malformed, or not starting with '[').
//
// The rule matches a SINGLE bracket group. Mendix stores sibling groups
// concatenated — `[a][b][c]` — and with the error listeners removed ANTLR happily
// parsed the first and left the rest on the stream, returning true. Callers read
// that as "fully parsed" and re-rendered only what came back, silently dropping
// every later group (mendixlabs/mxcli#772). A partial parse is therefore reported
// as a failure so callers fall back to the untouched string; use
// SplitXPathPredicateGroups to handle each group in turn.
func ParseXPathConstraint(input string) (ast.Expression, bool) {
	if input == "" {
		return nil, false
	}

	is := antlr.NewInputStream(input)
	lexer := parser.NewMDLLexer(is)
	stream := antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel)
	p := parser.NewMDLParser(stream)
	p.RemoveErrorListeners()

	ctx := p.XpathConstraint()
	xcCtx, ok := ctx.(*parser.XpathConstraintContext)
	if !ok {
		return nil, false
	}
	xpathExpr := xcCtx.XpathExpr()
	if xpathExpr == nil {
		return nil, false
	}
	// Anything left on the stream means the rule consumed only a prefix.
	if stream.LA(1) != antlr.TokenEOF {
		return nil, false
	}
	return buildXPathExpr(xpathExpr), true
}

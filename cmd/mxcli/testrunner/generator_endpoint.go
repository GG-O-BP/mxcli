// SPDX-License-Identifier: Apache-2.0

package testrunner

import (
	"fmt"
	"strings"
)

// Verdict protocol. A test microflow returns one string: either verdictPass, or
// verdictFailPrefix followed by the reason. The endpoint hands that string back
// as the HTTP response's "result" field, so a result is a returned value rather
// than something recovered from the runtime log.
const (
	verdictPass       = "PASS"
	verdictFailPrefix = "FAIL:"
)

// GenerateTestFlows returns the MDL declaring one microflow per test case.
//
// This is the endpoint path's counterpart to GenerateTestRunner, which compiles
// the whole suite into a single after-startup microflow. One microflow per test
// buys three things that the monolith cannot give:
//
//   - Each test can be invoked, re-invoked, or skipped on its own, so --filter
//     and single-test runs are a matter of which URL is called.
//   - A test that throws fails only itself. In the monolith an uncaught error
//     ends the whole flow, and because that flow is the after-startup action it
//     also fails the boot.
//   - Every test gets its own variable scope, so the suffix-renaming the
//     monolith needs to keep `$result` in test 1 from colliding with `$result`
//     in test 2 is simply not required here.
func GenerateTestFlows(suite *TestSuite) string {
	var b strings.Builder
	b.WriteString("CREATE MODULE " + mxTestModule + ";\n\n")
	for _, tc := range suite.Tests {
		writeTestFlow(&b, tc)
		b.WriteString("\n")
	}
	return b.String()
}

// writeTestFlow writes one test's microflow.
func writeTestFlow(b *strings.Builder, tc TestCase) {
	fmt.Fprintf(b, "/** %s */\n", escapeMDLComment(tc.Name))
	fmt.Fprintf(b, "CREATE OR REPLACE MICROFLOW %s ()\n", testFlowName(tc))
	b.WriteString("RETURNS String AS $Verdict\n")
	b.WriteString("BEGIN\n")
	fmt.Fprintf(b, "  DECLARE $Verdict String = '%s';\n", verdictPass)

	if tc.Throws != "" {
		writeThrowsFlowBody(b, tc)
	} else {
		writeExpectFlowBody(b, tc)
	}

	b.WriteString("  RETURN $Verdict;\n")
	b.WriteString("END;\n")
	b.WriteString("/\n")
}

// writeExpectFlowBody writes the body of a normal test: run the MDL, then check
// each @expect. An error during the body short-circuits to a FAIL verdict.
func writeExpectFlowBody(b *strings.Builder, tc TestCase) {
	for _, line := range rewriteBodyForVerdict(strings.Split(tc.MDL, "\n"), tc) {
		b.WriteString("  ")
		b.WriteString(line)
		b.WriteString("\n")
	}
	for _, exp := range tc.Expects {
		writeExpectCheck(b, exp)
	}
}

// writeThrowsFlowBody writes the body of an @throws test: the verdict starts as
// a failure and only the error handler can clear it, so a body that completes
// without throwing fails — which is the point of the annotation.
func writeThrowsFlowBody(b *strings.Builder, tc TestCase) {
	fmt.Fprintf(b, "  SET $Verdict = '%s';\n",
		escapeMDLString(verdictFailPrefix+"expected an exception but none was thrown"))
	for _, line := range rewriteBodyForThrows(strings.Split(tc.MDL, "\n")) {
		b.WriteString("  ")
		b.WriteString(line)
		b.WriteString("\n")
	}
}

// writeExpectCheck writes one @expect assertion.
//
// Only the pass condition is expressed with `=`; a `<>` expectation is compiled
// as the same equality with the branches swapped. That is deliberate and
// inherited from the monolithic generator: `<>` in a generated Mendix expression
// produced expression errors, so the operator never reaches the model.
func writeExpectCheck(b *strings.Builder, exp Expect) {
	equal := fmt.Sprintf("%s = %s", exp.Variable, exp.Value)
	failMsg := escapeMDLString(fmt.Sprintf("%sexpected %s %s %s",
		verdictFailPrefix, exp.Variable, exp.Operator, exp.Value))

	// An earlier statement may already have failed the test; never overwrite an
	// existing failure with a later assertion's result.
	fmt.Fprintf(b, "  IF $Verdict = '%s' THEN\n", verdictPass)
	if exp.Operator == "<>" {
		fmt.Fprintf(b, "    IF %s THEN\n", equal)
		fmt.Fprintf(b, "      SET $Verdict = '%s';\n", failMsg)
		b.WriteString("    END IF;\n")
	} else {
		fmt.Fprintf(b, "    IF %s THEN\n", equal)
		b.WriteString("    ELSE\n")
		fmt.Fprintf(b, "      SET $Verdict = '%s';\n", failMsg)
		b.WriteString("    END IF;\n")
	}
	b.WriteString("  END IF;\n")
}

// rewriteBodyForVerdict attaches an ON ERROR handler to every CALL in the test
// body, turning a thrown error into a FAIL verdict and an early return.
func rewriteBodyForVerdict(lines []string, tc TestCase) []string {
	handler := []string{
		fmt.Sprintf("  SET $Verdict = '%s';",
			escapeMDLString(verdictFailPrefix+"exception during execution")),
		"  RETURN $Verdict;",
	}
	return attachOnError(lines, handler)
}

// rewriteBodyForThrows attaches an ON ERROR handler that clears the pre-set
// failure verdict — the error is the expected outcome.
func rewriteBodyForThrows(lines []string) []string {
	handler := []string{fmt.Sprintf("  SET $Verdict = '%s';", verdictPass)}
	return attachOnError(lines, handler)
}

// attachOnError appends `ON ERROR { ... }` to each CALL statement in the body,
// joining a statement that spans several lines first.
func attachOnError(lines, handler []string) []string {
	var out []string
	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if !containsCallMicroflow(trimmed) {
			out = append(out, lines[i])
			continue
		}

		stmt := lines[i]
		for !strings.HasSuffix(strings.TrimSpace(stmt), ";") && i+1 < len(lines) {
			i++
			stmt += "\n" + lines[i]
		}
		stmt = strings.TrimSuffix(strings.TrimSpace(stmt), ";")

		out = append(out, stmt+" ON ERROR {")
		out = append(out, handler...)
		out = append(out, "};")
	}
	return out
}

// escapeMDLComment keeps a test name from closing the javadoc block it sits in.
func escapeMDLComment(s string) string {
	return strings.ReplaceAll(s, "*/", "* /")
}

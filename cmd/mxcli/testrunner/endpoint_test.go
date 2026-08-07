// SPDX-License-Identifier: Apache-2.0

package testrunner

import (
	"strings"
	"testing"
)

func TestNewEndpointTokenIsUniqueAndLongEnough(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		tok, err := newEndpointToken()
		if err != nil {
			t.Fatalf("newEndpointToken: %v", err)
		}
		if len(tok) != 64 {
			t.Fatalf("token %q is %d hex chars, want 64 (256 bits)", tok, len(tok))
		}
		if seen[tok] {
			t.Fatalf("newEndpointToken returned a duplicate: %q", tok)
		}
		seen[tok] = true
	}
}

// TestEndpointJavaFailsClosed pins the property that makes it safe to leave the
// MxTest module in a project: with no token in the environment the handler is
// never registered, so there is nothing to reach.
func TestEndpointJavaFailsClosed(t *testing.T) {
	guard := strings.Index(endpointJava, `System.getenv("`+endpointTokenEnv+`")`)
	if guard < 0 {
		t.Fatal("the handler does not read the token from the environment")
	}
	register := strings.Index(endpointJava, "Core.addRequestHandler")
	if register < 0 {
		t.Fatal("the handler is never registered")
	}
	if guard > register {
		t.Error("the token is read after the handler is registered; it must gate registration")
	}

	// The early return between them is what makes the guard load-bearing.
	between := endpointJava[guard:register]
	if !strings.Contains(between, "return true;") {
		t.Error("no early return between reading the token and registering: an empty token would still register the handler")
	}
}

// TestEndpointJavaNeverEmbedsASecret pins that the token reaches the runtime
// through the environment only. Interpolating it into the generated Java would
// write a live credential into the user's javasource/ tree, where a failed
// cleanup leaves it behind.
func TestEndpointJavaNeverEmbedsASecret(t *testing.T) {
	tok, err := newEndpointToken()
	if err != nil {
		t.Fatalf("newEndpointToken: %v", err)
	}
	mdl := GenerateEndpointMDL()
	if strings.Contains(mdl, tok) {
		t.Fatal("the generated MDL contains the token")
	}
	// GenerateEndpointMDL takes no token argument at all, so the only way one
	// could appear is via the environment read.
	if !strings.Contains(mdl, endpointTokenEnv) {
		t.Errorf("the generated MDL does not reference %s", endpointTokenEnv)
	}
}

func TestEndpointJavaChecksTheToken(t *testing.T) {
	// Match the return statement, not the word: an earlier version of this test
	// looked for "MessageDigest.isEqual" anywhere in the source and was satisfied
	// by the comment above the method, so it stayed green when the body was
	// swapped for String.equals.
	if !strings.Contains(endpointJava, "return java.security.MessageDigest.isEqual(") {
		t.Error("token comparison is not constant-time (use MessageDigest.isEqual, not String.equals)")
	}
	if strings.Contains(endpointJava, "presented.equals(") {
		t.Error("the token is compared with String.equals, which returns early on the first differing byte")
	}
	if !strings.Contains(endpointJava, endpointTokenHeader) {
		t.Errorf("the handler does not read the %s header", endpointTokenHeader)
	}
	if !strings.Contains(endpointJava, "return java.net.InetAddress.getByName(addr).isLoopbackAddress();") {
		t.Error("the handler does not refuse non-loopback callers")
	}
}

// TestEndpointJavaOnlyRunsTestMicroflows pins that the endpoint is not a general
// microflow-invocation API even for a caller holding the token.
func TestEndpointJavaOnlyRunsTestMicroflows(t *testing.T) {
	if !strings.Contains(endpointJava, `mf.startsWith("`+testFlowPrefix+`")`) {
		t.Errorf("the handler does not restrict execution to %s* microflows", testFlowPrefix)
	}
}

// TestEndpointListCannotEnumerateTheApp pins that /list is clamped to the test
// namespace. Found by probing the live runtime: an absent prefix returned every
// microflow in the app, Administration.* included. The endpoint will not run
// those, so it must not disclose them either.
func TestEndpointListCannotEnumerateTheApp(t *testing.T) {
	if !strings.Contains(endpointJava, `!prefix.startsWith("`+testFlowPrefix+`")`) {
		t.Error("a caller-supplied prefix is not clamped to the test namespace")
	}
	if strings.Contains(endpointJava, "if (prefix == null || n.startsWith(prefix))") {
		t.Error("a null prefix still lists every microflow in the app")
	}
}

// TestEndpointRejectsBeforeItActs pins the ordering of the two gates: both the
// loopback check and the token check must precede any use of the request.
func TestEndpointRejectsBeforeItActs(t *testing.T) {
	loopback := strings.Index(endpointJava, "if (!isLoopback(")
	token := strings.Index(endpointJava, "if (!tokenOK(")
	execute := strings.Index(endpointJava, "Core.microflowCall(mf).execute")
	names := strings.Index(endpointJava, "Core.getMicroflowNames()")

	for _, tc := range []struct {
		name       string
		gate, work int
	}{
		{"loopback check precedes listing microflow names", loopback, names},
		{"token check precedes listing microflow names", token, names},
		{"loopback check precedes execution", loopback, execute},
		{"token check precedes execution", token, execute},
	} {
		if tc.gate < 0 || tc.work < 0 {
			t.Fatalf("%s: a landmark is missing (gate=%d work=%d)", tc.name, tc.gate, tc.work)
		}
		if tc.gate > tc.work {
			t.Errorf("%s: gate at %d comes after the work at %d", tc.name, tc.gate, tc.work)
		}
	}
}

func TestGenerateEndpointMDLShape(t *testing.T) {
	mdl := GenerateEndpointMDL()
	for _, want := range []string{
		"CREATE MODULE " + mxTestModule + ";",
		"CREATE OR REPLACE JAVA ACTION " + endpointRegisterAction + "() RETURNS Boolean",
		"CREATE OR REPLACE MICROFLOW " + endpointStartupFlow + " ()",
		"RETURNS Boolean AS $Registered",
	} {
		if !strings.Contains(mdl, want) {
			t.Errorf("generated MDL is missing %q", want)
		}
	}
	// The startup microflow must return Boolean or Mendix fails the build with
	// CE0142 on a void after-startup microflow.
	if !strings.Contains(mdl, "RETURN $Registered;") {
		t.Error("the startup microflow does not return a Boolean (CE0142)")
	}
}

// TestGenerateEndpointMDLIsTestIndependent pins the property the whole design
// rests on: the endpoint MDL does not mention any test, so it never has to be
// regenerated when tests change.
func TestGenerateEndpointMDLIsTestIndependent(t *testing.T) {
	a := GenerateEndpointMDL()
	b := GenerateEndpointMDL()
	if a != b {
		t.Fatal("GenerateEndpointMDL is not deterministic")
	}
	if strings.Contains(a, "test_1") {
		t.Error("the endpoint MDL references a specific test")
	}
}

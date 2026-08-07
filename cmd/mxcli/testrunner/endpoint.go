// SPDX-License-Identifier: Apache-2.0

package testrunner

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
)

const (
	// endpointPath is the path the request handler is registered at. Mendix
	// matches on the leading segment, so the trailing slash is part of the name.
	endpointPath = "mxtest/"
	// endpointTokenEnv is the environment variable the runtime JVM reads its
	// per-run token from. It is passed via the process environment and never
	// written into the project, so a cleanup that fails cannot leave a working
	// credential behind in javasource/.
	endpointTokenEnv = "MXCLI_TEST_TOKEN"
	// endpointTokenHeader carries the token on each request.
	endpointTokenHeader = "X-MxTest-Token"

	// endpointRegisterAction is the Java action that registers the handler.
	endpointRegisterAction = mxTestModule + ".RegisterTestEndpoint"
	// endpointStartupFlow is the after-startup microflow that calls it. It only
	// registers the endpoint — unlike the log-scraping runner it replaces, no test
	// ever executes during startup.
	endpointStartupFlow = mxTestModule + ".RegisterEndpoint"
	// testFlowPrefix prefixes every generated per-test microflow.
	testFlowPrefix = mxTestModule + ".Test_"
)

// newEndpointToken returns a fresh 256-bit token as hex.
func newEndpointToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generating endpoint token: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// testFlowName is the microflow generated for a test case.
func testFlowName(tc TestCase) string { return testFlowPrefix + tc.ID }

// GenerateEndpointMDL returns the MDL that installs the test endpoint: a Java
// action registering a request handler, plus the after-startup microflow that
// calls it once at boot.
//
// The handler is generic — it resolves microflows by name from
// Core.getMicroflowNames() at request time — so this MDL does not mention any
// test and never has to be regenerated when tests change. That is what lets a
// re-run be an HTTP call instead of a restart.
//
// Three properties make an endpoint that executes arbitrary microflows under a
// system context safe enough to install in a developer's project:
//
//  1. It fails closed. With no token in the environment the handler is not
//     registered at all, so a project whose cleanup failed and still carries the
//     MxTest module exposes nothing when deployed anywhere else.
//  2. Every request must present the token, compared with a length-independent
//     constant-time equality so a wrong guess leaks no timing signal.
//  3. Non-loopback callers are refused outright. mxcli always talks to
//     127.0.0.1; nothing legitimate reaches this handler from off-box.
//
// chainAfterStartup, when non-empty, is a microflow the generated startup flow
// calls after registering the endpoint — the project's own after-startup
// microflow, which this one displaces.
//
// The test runner passes "" : a test run wants a known starting state, and the
// suite is the only thing that should execute. A dev loop hosting the endpoint
// (`run --local --test-endpoint`) passes the real one, because the developer's
// app must still seed its data and do whatever else it does at boot.
func GenerateEndpointMDL(chainAfterStartup string) string {
	var b strings.Builder

	b.WriteString("CREATE MODULE " + mxTestModule + ";\n\n")
	b.WriteString("/** Registers the mxcli test endpoint. Called once at startup. */\n")
	b.WriteString("CREATE OR REPLACE JAVA ACTION " + endpointRegisterAction + "() RETURNS Boolean\n")
	b.WriteString("AS $$\n")
	b.WriteString(endpointJava)
	b.WriteString("\n$$;\n/\n\n")

	b.WriteString("/** Registers the mxcli test endpoint at boot. Runs no tests. */\n")
	b.WriteString("CREATE OR REPLACE MICROFLOW " + endpointStartupFlow + " ()\n")
	b.WriteString("RETURNS Boolean AS $Registered\n")
	b.WriteString("BEGIN\n")
	b.WriteString("  $Registered = CALL JAVA ACTION " + endpointRegisterAction + "();\n")
	if chainAfterStartup != "" {
		// Register first, then hand over: if the project's own startup microflow
		// fails, the endpoint is already up and the failure is diagnosable over
		// HTTP instead of only in the log.
		b.WriteString("  $Chained = CALL MICROFLOW " + chainAfterStartup + "();\n")
	}
	b.WriteString("  RETURN $Registered;\n")
	b.WriteString("END;\n")
	b.WriteString("/\n")

	return b.String()
}

// endpointJava is the body of the registration Java action. It is a constant,
// not a template: nothing about a particular run is interpolated into it, and in
// particular the token is read from the environment rather than baked in.
//
// Fully-qualified type names throughout — the generated .java file's import list
// is fixed by the Java-action scaffold and cannot be extended from MDL.
const endpointJava = `final com.mendix.logging.ILogNode log = com.mendix.core.Core.getLogger("MxTest");

// Fail closed: no token in the environment means this is not an mxcli test run,
// so the endpoint is never exposed. A project that kept the MxTest module
// through a failed cleanup is inert everywhere else, including production.
final String expectedToken = System.getenv("` + endpointTokenEnv + `");
if (expectedToken == null || expectedToken.isEmpty()) {
    log.info("MxTest: no ` + endpointTokenEnv + ` in the environment; test endpoint NOT registered");
    return true;
}

com.mendix.core.Core.addRequestHandler("` + endpointPath + `", new com.mendix.externalinterface.connector.RequestHandler() {

    private String esc(String s) {
        if (s == null) return "null";
        StringBuilder b = new StringBuilder("\"");
        for (int i = 0; i < s.length(); i++) {
            char c = s.charAt(i);
            switch (c) {
                case '"':  b.append("\\\""); break;
                case '\\': b.append("\\\\"); break;
                case '\n': b.append("\\n");  break;
                case '\r': b.append("\\r");  break;
                case '\t': b.append("\\t");  break;
                default:
                    if (c < 0x20) b.append(String.format("\\u%04x", (int) c));
                    else b.append(c);
            }
        }
        return b.append('"').toString();
    }

    // Constant-time comparison. String.equals returns early on the first
    // differing byte; MessageDigest.isEqual does not, and is also safe when the
    // lengths differ.
    private boolean tokenOK(String presented) {
        if (presented == null) return false;
        return java.security.MessageDigest.isEqual(
            presented.getBytes(java.nio.charset.StandardCharsets.UTF_8),
            expectedToken.getBytes(java.nio.charset.StandardCharsets.UTF_8));
    }

    private boolean isLoopback(String addr) {
        if (addr == null || addr.isEmpty()) return false;
        try {
            return java.net.InetAddress.getByName(addr).isLoopbackAddress();
        } catch (java.net.UnknownHostException e) {
            return false;
        }
    }

    @Override
    protected void processRequest(com.mendix.m2ee.api.IMxRuntimeRequest request,
                                  com.mendix.m2ee.api.IMxRuntimeResponse response,
                                  String path) throws Exception {
        response.setContentType("application/json");
        java.io.Writer out = response.getWriter();

        // mxcli always calls 127.0.0.1. Anything else is not a test run.
        if (!isLoopback(request.getRemoteAddr())) {
            log.warn("MxTest: refused non-loopback request from " + request.getRemoteAddr());
            response.setStatus(com.mendix.m2ee.api.IMxRuntimeResponse.FORBIDDEN);
            out.write("{\"error\":\"forbidden\"}");
            out.flush();
            return;
        }
        if (!tokenOK(request.getHeader("` + endpointTokenHeader + `"))) {
            log.warn("MxTest: refused request with a missing or incorrect token");
            response.setStatus(com.mendix.m2ee.api.IMxRuntimeResponse.UNAUTHORIZED);
            out.write("{\"error\":\"unauthorized\"}");
            out.flush();
            return;
        }

        java.util.Set<String> known = com.mendix.core.Core.getMicroflowNames();

        if ("list".equals(path)) {
            // Never widen past the test namespace. An unfiltered list would hand
            // back the app's entire microflow inventory, which this endpoint has
            // no business disclosing — it will not run those microflows either.
            // A caller-supplied prefix can only narrow further.
            String prefix = request.getParameter("prefix");
            if (prefix == null || !prefix.startsWith("` + testFlowPrefix + `")) {
                prefix = "` + testFlowPrefix + `";
            }
            java.util.List<String> names = new java.util.ArrayList<String>();
            for (String n : known) {
                if (n.startsWith(prefix)) names.add(n);
            }
            java.util.Collections.sort(names);
            StringBuilder b = new StringBuilder("{\"microflows\":[");
            for (int i = 0; i < names.size(); i++) {
                if (i > 0) b.append(',');
                b.append(esc(names.get(i)));
            }
            b.append("]}");
            out.write(b.toString());
            out.flush();
            return;
        }

        if (!"run".equals(path)) {
            response.setStatus(com.mendix.m2ee.api.IMxRuntimeResponse.NOT_FOUND);
            out.write("{\"error\":\"no such route\",\"path\":" + esc(path) + "}");
            out.flush();
            return;
        }

        String mf = request.getParameter("mf");
        if (mf == null || mf.isEmpty()) {
            response.setStatus(com.mendix.m2ee.api.IMxRuntimeResponse.BAD_REQUEST);
            out.write("{\"error\":\"missing mf parameter\"}");
            out.flush();
            return;
        }
        // Only ever run a microflow this runner generated. Even behind the token
        // this handler should not be a way to invoke the rest of the app.
        if (!mf.startsWith("` + testFlowPrefix + `")) {
            response.setStatus(com.mendix.m2ee.api.IMxRuntimeResponse.FORBIDDEN);
            out.write("{\"error\":\"not a test microflow\",\"mf\":" + esc(mf) + "}");
            out.flush();
            return;
        }
        if (!known.contains(mf)) {
            response.setStatus(com.mendix.m2ee.api.IMxRuntimeResponse.NOT_FOUND);
            out.write("{\"error\":\"unknown microflow\",\"mf\":" + esc(mf) + "}");
            out.flush();
            return;
        }

        long t0 = System.nanoTime();
        com.mendix.systemwideinterfaces.core.IContext ctx = com.mendix.core.Core.createSystemContext();
        Object result = null;
        String error = null;
        try {
            result = com.mendix.core.Core.microflowCall(mf).execute(ctx);
        } catch (Throwable t) {
            Throwable root = t;
            while (root.getCause() != null && root.getCause() != root) root = root.getCause();
            String msg = root.getMessage();
            error = (msg == null || msg.isEmpty()) ? root.getClass().getName() : msg;
        }
        long micros = (System.nanoTime() - t0) / 1000L;

        StringBuilder b = new StringBuilder("{");
        b.append("\"mf\":").append(esc(mf));
        b.append(",\"ok\":").append(error == null);
        b.append(",\"durationMicros\":").append(micros);
        b.append(",\"result\":").append(result == null ? "null" : esc(String.valueOf(result)));
        if (error != null) b.append(",\"error\":").append(esc(error));
        b.append('}');
        out.write(b.toString());
        out.flush();
    }
});

log.info("MxTest: test endpoint registered at /` + endpointPath + `");
return true;`

# Makefile for MDL grammar generation
#
# Usage:
#   make bootstrap   - Install the pinned ANTLR4 toolchain (pip + a JVM)
#   make generate    - Generate Go parser from MDLLexer.g4 and MDLParser.g4
#   make clean       - Remove generated files
#
# Prerequisites: an `antlr4` launcher on PATH and a JVM. The generator version
# is PINNED and load-bearing: a generator/runtime mismatch is a classic ANTLR
# failure mode, and go.mod requires the runtime below. CI pins the same pair.
# `antlr4-tools` downloads the ANTLR jar on first run, so the build needs
# network and a JVM, not only Go.

LEXER = MDLLexer.g4
PARSER = MDLParser.g4
OUTPUT_DIR = parser
PACKAGE = parser

# Domain grammar files (imported by MDLParser.g4 via the `import` directive)
DOMAIN_FILES = \
    domains/MDLDomainModel.g4 \
    domains/MDLMicroflow.g4 \
    domains/MDLPage.g4 \
    domains/MDLSecurity.g4 \
    domains/MDLAgent.g4 \
    domains/MDLWorkflow.g4 \
    domains/MDLService.g4 \
    domains/MDLCatalog.g4 \
    domains/MDLSettings.g4

# Pinned toolchain. Keep in step with the CI workflows and with the
# github.com/antlr4-go/antlr/v4 requirement in go.mod.
ANTLR_TOOLS_VERSION = 0.2.2
ANTLR_VERSION = 4.13.2
ANTLR_RUNTIME_VERSION = 4.13.1

# Find antlr4 command (try common locations)
ANTLR4 := $(shell which antlr4 2>/dev/null || which antlr 2>/dev/null)

.PHONY: generate clean check-antlr bootstrap

check-antlr:
ifndef ANTLR4
	$(error ANTLR4 not found. Run `make -C mdl/grammar bootstrap`, or install it yourself: \
	  pip install 'antlr4-tools==$(ANTLR_TOOLS_VERSION)' && export ANTLR4_TOOLS_ANTLR_VERSION=$(ANTLR_VERSION) \
	  (macOS alternative: brew install antlr4). The generator version is pinned to $(ANTLR_VERSION) \
	  against runtime $(ANTLR_RUNTIME_VERSION) in go.mod — a mismatch produces a parser that does not compile)
endif

# bootstrap installs the pinned generator. It needs pip and a JVM; antlr4-tools
# downloads antlr4-$(ANTLR_VERSION)-complete.jar on first run.
bootstrap:
	pip install 'antlr4-tools==$(ANTLR_TOOLS_VERSION)'
	@echo ""
	@echo "Installed antlr4-tools $(ANTLR_TOOLS_VERSION)."
	@echo "Export the pinned generator version before building:"
	@echo "    export ANTLR4_TOOLS_ANTLR_VERSION=$(ANTLR_VERSION)"

generate: check-antlr $(LEXER) $(PARSER) $(DOMAIN_FILES)
	@mkdir -p $(OUTPUT_DIR)
	@if [ -z "$$ANTLR4_TOOLS_ANTLR_VERSION" ]; then \
	  echo "Note: ANTLR4_TOOLS_ANTLR_VERSION is unset; this build pins $(ANTLR_VERSION)."; \
	  echo "      export ANTLR4_TOOLS_ANTLR_VERSION=$(ANTLR_VERSION)"; \
	fi
	$(ANTLR4) -Dlanguage=Go -no-visitor -package $(PACKAGE) -lib domains -o $(OUTPUT_DIR) $(LEXER) $(PARSER)
	@echo "Generated Go parser in $(OUTPUT_DIR)/"

clean:
	rm -rf $(OUTPUT_DIR)

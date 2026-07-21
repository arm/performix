# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

.PHONY: ai-insights-eval-deps apx agent all build clean deps generate prebuild protobuf test robot-test tools FORCE

ENGINE_DIR := ../apap-engine
BINARY_DIR := ../apap-cli
BINARY_NAME := apx
TOOLS_VENV_DIR := ../.venv
TOOLS_REQUIREMENTS := ../robot/requirements.txt
AI_INSIGHTS_EVAL_VENV_DIR := ../scripts/ai-insights-evaluation/.venv
AI_INSIGHTS_EVAL_REQUIREMENTS := ../scripts/ai-insights-evaluation/requirements.txt

# Auto-detect platform (can override with: make build GOOS=linux GOARCH=arm64)
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
HOST_OS := $(shell go env GOHOSTOS)

TOOL_BUNDLE := $(BINARY_DIR)/tools
TOOLS_STAMP := $(TOOL_BUNDLE)/.fetched
TOOLS_VENV_STAMP := $(TOOLS_VENV_DIR)/.deps-installed
AI_INSIGHTS_EVAL_VENV_STAMP := $(AI_INSIGHTS_EVAL_VENV_DIR)/.deps-installed

ifeq ($(HOST_OS),windows)
	TOOLS_VENV_PYTHON := $(TOOLS_VENV_DIR)/Scripts/python.exe
	AI_INSIGHTS_EVAL_VENV_PYTHON := $(AI_INSIGHTS_EVAL_VENV_DIR)/Scripts/python.exe
else
	TOOLS_VENV_PYTHON := $(TOOLS_VENV_DIR)/bin/python
	AI_INSIGHTS_EVAL_VENV_PYTHON := $(AI_INSIGHTS_EVAL_VENV_DIR)/bin/python
endif

# Determine the right extension for the binary based on OS
ifeq ($(GOOS),windows)
	BINARY_EXT := .exe
else
	BINARY_EXT :=
endif

BINARY := $(BINARY_DIR)/$(BINARY_NAME)$(BINARY_EXT)

all: build lint test

build: deps apx

apx: prebuild generate $(STREAMLINE_LIB)
	@echo "Building $(BINARY) for $(GOOS)/$(GOARCH)"
	@go build -tags=duckdb_arrow -v -o $(BINARY)

agent: $(TOOLS_VENV_STAMP)
	@echo "Building and bundling target agent"
	@mkdir -p $(TOOL_BUNDLE)
	@$(TOOLS_VENV_PYTHON) ../scripts/get-tools.py target_agent

deps: $(STREAMLINE_LIB) $(TOOLS_STAMP)

$(TOOLS_VENV_PYTHON):
	@echo "Bootstrapping tooling Python virtual environment"
	@python3 -m venv $(TOOLS_VENV_DIR)

$(TOOLS_VENV_STAMP): $(TOOLS_VENV_PYTHON) $(TOOLS_REQUIREMENTS)
	@echo "Installing tooling Python dependencies from $(TOOLS_REQUIREMENTS)"
	@$(TOOLS_VENV_PYTHON) -m pip install --disable-pip-version-check -r $(TOOLS_REQUIREMENTS)
	@touch $@

$(AI_INSIGHTS_EVAL_VENV_PYTHON):
	@echo "Bootstrapping AI Insights evaluation Python virtual environment"
	@python3 -m venv $(AI_INSIGHTS_EVAL_VENV_DIR)

$(AI_INSIGHTS_EVAL_VENV_STAMP): $(AI_INSIGHTS_EVAL_VENV_PYTHON) $(AI_INSIGHTS_EVAL_REQUIREMENTS)
	@echo "Installing AI Insights evaluation Python dependencies from $(AI_INSIGHTS_EVAL_REQUIREMENTS)"
	@$(AI_INSIGHTS_EVAL_VENV_PYTHON) -m pip install --disable-pip-version-check -r $(AI_INSIGHTS_EVAL_REQUIREMENTS)
	@touch $@

ai-insights-eval-deps: $(AI_INSIGHTS_EVAL_VENV_STAMP)

clean:
	@rm -f $(BINARY)
	@rm -f $(BINARY_DIR)/*.lib
	@rm -rf ${TOOL_BUNDLE}

generate:
	@echo "Generating Go code, uses ../scripts/go/generate-all.sh, does not regenerate protobuf code (see 'make protobuf')"
	@../scripts/go/generate-all.sh

lint:
	@../scripts/lint-all.sh --fix

prebuild:
# Attempt to stop the daemon if it's running
ifeq ($(HOST_OS),windows)
	@if exist "$(BINARY)" ( \
		tasklist /FI "IMAGENAME eq $(BINARY_NAME)$(BINARY_EXT)" 2>NUL | findstr /I /C:"$(BINARY_NAME)$(BINARY_EXT)" >NUL && ( \
			echo Stopping running $(BINARY_NAME) daemon & \
			"$(BINARY)" daemon stop \
		) \
	)
else
	-@if [ -x "$(BINARY)" ] && pgrep -x "$(BINARY_NAME)" >/dev/null 2>&1; then \
		echo "Stopping running $(BINARY_NAME) daemon"; \
		"$(BINARY)" daemon stop; \
	fi
endif

protobuf:
	@../scripts/protobuf/generate.sh

# Runs all unit tests (Go and Python)
test:
	@../scripts/test-all.sh

# Run Robot Framework functional tests against a remote target.
# Basic usage (skips workload-dependent and remote_localhost tests): make robot-test TARGET=<name>
# See robot/README.md for more options and further details.
robot-test:
	$(if $(TARGET),,$(error TARGET is required. Usage: make robot-test TARGET=<name>))
	@../scripts/python/run-robot.sh --target $(TARGET) $(ROBOT_ARGS)

tools: FORCE $(TOOLS_VENV_STAMP)
	@echo "Fetching tool bundle"
	@$(TOOLS_VENV_PYTHON) ../scripts/get-tools.py
	@touch $(TOOLS_STAMP)

$(TOOLS_STAMP): | $(TOOLS_VENV_STAMP)
	@echo "Fetching tool bundle"
	@$(TOOLS_VENV_PYTHON) ../scripts/get-tools.py
	@touch $@

FORCE:

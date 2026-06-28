POWERSHELL := $(shell command -v pwsh 2>/dev/null || command -v powershell 2>/dev/null)

.PHONY: setup fmt lint test windows-resource build

setup:
	@if [ -n "$(POWERSHELL)" ]; then \
		"$(POWERSHELL)" -NoProfile -ExecutionPolicy Bypass -File scripts/setup.ps1; \
	else \
		bash scripts/local-go.sh setup; \
	fi

fmt:
	@if [ -n "$(POWERSHELL)" ]; then \
		"$(POWERSHELL)" -NoProfile -ExecutionPolicy Bypass -File scripts/fmt.ps1; \
	else \
		bash scripts/local-go.sh fmt; \
	fi

lint:
	@if [ -n "$(POWERSHELL)" ]; then \
		"$(POWERSHELL)" -NoProfile -ExecutionPolicy Bypass -File scripts/lint.ps1; \
	else \
		bash scripts/local-go.sh lint; \
	fi

test:
	@if [ -n "$(POWERSHELL)" ]; then \
		"$(POWERSHELL)" -NoProfile -ExecutionPolicy Bypass -File scripts/test.ps1; \
	else \
		bash scripts/local-go.sh test; \
	fi

windows-resource:
	@if [ -n "$(POWERSHELL)" ]; then \
		"$(POWERSHELL)" -NoProfile -ExecutionPolicy Bypass -File scripts/generate-windows-resources.ps1; \
	else \
		echo "PowerShell is required for windows-resource"; \
		exit 1; \
	fi

build:
	@if [ -n "$(POWERSHELL)" ]; then \
		"$(POWERSHELL)" -NoProfile -ExecutionPolicy Bypass -File scripts/build.ps1; \
	else \
		bash scripts/local-go.sh build; \
	fi

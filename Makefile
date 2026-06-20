.PHONY: setup fmt lint test windows-resource build

setup:
	powershell -NoProfile -ExecutionPolicy Bypass -File scripts/setup.ps1

fmt:
	powershell -NoProfile -ExecutionPolicy Bypass -File scripts/fmt.ps1

lint:
	powershell -NoProfile -ExecutionPolicy Bypass -File scripts/lint.ps1

test:
	powershell -NoProfile -ExecutionPolicy Bypass -File scripts/test.ps1

windows-resource:
	powershell -NoProfile -ExecutionPolicy Bypass -File scripts/generate-windows-resources.ps1

build:
	powershell -NoProfile -ExecutionPolicy Bypass -File scripts/build.ps1

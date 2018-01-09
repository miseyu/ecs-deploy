.PHONY: release
.DEFAULT_GOAL := help

release: ## relase
	go run bin/cross-compile.go ${VERSION}
	bin/upload-github ${VERSION}


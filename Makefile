GO ?= go
STATICCHECK ?= $(GO) run honnef.co/go/tools/cmd/staticcheck@latest
GOVULNCHECK ?= $(GO) run golang.org/x/vuln/cmd/govulncheck@latest

.PHONY: build deps test vet fmt tidy tidy-check lint vuln check release clean

build:
	$(GO) build ./...

# The module must build from the standard library alone.
deps:
	@deps=$$($(GO) list -deps -f '{{if not .Standard}}{{.ImportPath}}{{end}}' ./... \
	  | grep -v '^github.com/ChristopherDavenport/agentsmd' || true); \
	  test -z "$$deps" || { echo "module depends on: $$deps"; exit 1; }

test:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...

tidy:
	$(GO) mod tidy

# Fails when go mod tidy would change go.mod or go.sum, without writing,
# so a stray dependency shows up in make check and not only in CI's diff.
tidy-check:
	$(GO) mod tidy -diff

fmt:
	gofmt -l . && test -z "$$(gofmt -l .)"

lint:
	$(STATICCHECK) ./...

vuln:
	$(GOVULNCHECK) ./...

# Everything CI runs.
check: fmt tidy-check vet deps lint vuln test

MODULE := $(shell $(GO) list -m)
NOTES := $(shell mktemp)

# Cut a release: the changelog's Unreleased section is dated, everything
# is checked, one commit is made, the root is tagged VERSION with the
# changelog section as the message, and the branch and tag are pushed.
# TRAILER, when set, is appended to the commit message.
release:
	@test -n "$(VERSION)" || { echo "usage: make release VERSION=vX.Y.Z"; exit 1; }
	@grep -q '^## Unreleased$$' CHANGELOG.md || { echo "CHANGELOG.md has no Unreleased section"; exit 1; }
	@test -z "$$(git status --porcelain)" || { echo "working tree is not clean"; exit 1; }
	sed -i 's/^## Unreleased$$/## $(VERSION) - '"$$(date +%F)"'/' CHANGELOG.md
	$(MAKE) tidy
	$(MAKE) check
	git add -A && git commit -q -m "Release $(VERSION)" $(if $(TRAILER),-m "$(TRAILER)")
	@awk -v v="$(VERSION)" '/^## /{p=($$2==v)} p' CHANGELOG.md | sed '1s/.*/$(VERSION)/' > $(NOTES)
	git tag -a $(VERSION) -F $(NOTES)
	@rm -f $(NOTES)
	git push origin HEAD
	git push origin $(VERSION)

clean:
	rm -rf .cache

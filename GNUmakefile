# The git-derived version of this checkout (the Authwise standard; see
# kit guide/build/VERSIONING.md and github.com/activatedio/go-version):
# clean X.Y.Z exactly on an annotated release tag, X.Y.(Z+1)-<sha8> between
# releases. Lazily computed on first use; CI overrides it
# (`make <target> VERSION=...`) with the value the pipeline computed once
# up front.
VERSION ?= $(shell go tool go-version)

default: testacc

.PHONY: version bump build generate test testacc

version:
	@go tool go-version

# Cut the next release tag (annotated, LOCAL ONLY — pushing the tag is the
# release act and triggers the release pipeline).
#   make bump                  # minor (default): v1.5.10 -> v1.6.0
#   make bump LEVEL=patch      #                  v1.5.10 -> v1.5.11
#   make bump LEVEL=major      #                  v1.5.10 -> v2.0.0
#   make bump DRY_RUN=true     # preview only
#   make bump PUSH=true        # tag + push (releases)
LEVEL ?= minor
bump:
	go tool go-version bump -level=$(LEVEL) $(if $(filter true,$(DRY_RUN)),-dry-run) $(if $(filter true,$(PUSH)),-push)

build:
	go build -ldflags "-X main.version=$(VERSION)" -o bin/terraform-provider-authwise .

# Regenerate the provider surface from the spec table.
generate:
	cd gen && go run .

test:
	go test -cover ./...

# Run acceptance tests
testacc:
	TF_ACC=1 go test ./... -v $(TESTARGS) -timeout 120m

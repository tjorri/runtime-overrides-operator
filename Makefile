# Image URL to use all building/pushing image targets
IMG ?= controller:latest

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# CONTAINER_TOOL defines the container tool to be used for building images.
# Be aware that the target commands are only tested with Docker which is
# scaffolded by default. However, you might want to replace it to use other
# tools. (i.e. podman)
CONTAINER_TOOL ?= docker

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

.PHONY: all
all: build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk command is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: manifests
manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	"$(CONTROLLER_GEN)" rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases

.PHONY: chart-sync
chart-sync: manifests ## Sync regenerated CRDs into both Helm charts + the release-asset crds.yaml.
	@# Helm-managed CRDs in both charts carry `helm.sh/resource-policy: keep`
	@# so that `helm upgrade --set crds.install=false` (migrating from
	@# bundled CRDs to the dedicated CRD chart) and `helm uninstall` do NOT
	@# delete the CRDs from the cluster — deleting a CRD cascade-deletes
	@# every CR of that kind, which would silently wipe every tenant
	@# override. The annotation has no effect on UPDATE behavior, so
	@# schema changes still propagate via `helm upgrade`. Users who
	@# actually want to remove the CRDs do so explicitly with kubectl.
	@# The aggregated dist/crds.yaml asset does not get the annotation
	@# because it's not Helm-managed.
	@awk_keep='/^  annotations:$$/ {print; print "    helm.sh/resource-policy: keep"; next} {print}' ; \
	for crd in lokitenantoverride mimirtenantoverride ; do \
	  src=config/crd/bases/runtimeoverrides.io_$${crd}s.yaml ; \
	  main_dst=deploy/charts/runtime-overrides-operator/templates/crds/$${crd}.yaml ; \
	  crds_dst=deploy/charts/runtime-overrides-operator-crds/templates/$${crd}.yaml ; \
	  awk "$$awk_keep" $$src > $$src.keep.tmp ; \
	  { printf '{{- if .Values.crds.install }}\n' ; \
	    cat $$src.keep.tmp ; \
	    printf '{{- end }}\n' ; } > $$main_dst ; \
	  cp $$src.keep.tmp $$crds_dst ; \
	  rm -f $$src.keep.tmp ; \
	done
	@# Aggregated release asset: a single YAML for `kubectl apply -f` users.
	@# Bare CRD YAML — no helm.sh annotation since this asset is not used
	@# by Helm.
	@mkdir -p dist
	@{ cat config/crd/bases/runtimeoverrides.io_lokitenantoverrides.yaml ; \
	   printf '\n---\n' ; \
	   cat config/crd/bases/runtimeoverrides.io_mimirtenantoverrides.yaml ; } > dist/crds.yaml
	@echo "chart-sync: regenerated CRDs into both charts + dist/crds.yaml"

.PHONY: check-image-consistency
check-image-consistency: ## Assert .ko.yaml carries the required OCI labels.
	@set -e ; \
	for label in org.opencontainers.image.source org.opencontainers.image.licenses \
	             org.opencontainers.image.title org.opencontainers.image.documentation ; do \
	  grep -q "$$label" .ko.yaml || { echo "FAIL: .ko.yaml missing OCI label $$label" ; exit 1; } ; \
	done ; \
	echo "Image build OK: .ko.yaml carries all 4 required OCI labels."

.PHONY: chart-docs
chart-docs: ## Regenerate the Helm chart READMEs via helm-docs.
	@command -v helm-docs >/dev/null \
	  || { echo "helm-docs not installed; install from https://github.com/norwoodj/helm-docs" >&2; exit 1; }
	helm-docs \
	  --chart-search-root deploy/charts/runtime-overrides-operator \
	  --template-files README.md.gotmpl \
	  --output-file README.md
	helm-docs \
	  --chart-search-root deploy/charts/runtime-overrides-operator-crds \
	  --template-files README.md.gotmpl \
	  --output-file README.md

.PHONY: chart-docs-check
chart-docs-check: chart-docs ## Fail if either committed chart README is stale.
	@git diff --exit-code \
	    deploy/charts/runtime-overrides-operator/README.md \
	    deploy/charts/runtime-overrides-operator-crds/README.md \
	  || { echo "chart README is stale — run 'make chart-docs' and commit the result" >&2; exit 1; }

.PHONY: chart-lint
chart-lint: chart-docs-check ## Lint, render, doc-check, and unit-test both Helm charts.
	@# Main chart — default values + Mimir-enabled variant.
	helm lint deploy/charts/runtime-overrides-operator/
	helm template ro-op deploy/charts/runtime-overrides-operator/ \
	  --namespace runtime-overrides-system > /dev/null
	helm template ro-op deploy/charts/runtime-overrides-operator/ \
	  --namespace runtime-overrides-system \
	  --set operator.targets.mimir.enabled=true > /dev/null
	@# Main chart with CRDs disabled — used in tandem with the CRD chart.
	helm template ro-op deploy/charts/runtime-overrides-operator/ \
	  --namespace runtime-overrides-system \
	  --set crds.install=false > /dev/null
	@# CRD-only chart.
	helm lint deploy/charts/runtime-overrides-operator-crds/
	helm template ro-op-crds deploy/charts/runtime-overrides-operator-crds/ > /dev/null
	@command -v helm >/dev/null && helm plugin list 2>/dev/null | grep -q '^unittest' \
	  || { echo "helm unittest plugin not installed; skipping unittest"; exit 0; }
	helm unittest deploy/charts/runtime-overrides-operator/
	@test -d deploy/charts/runtime-overrides-operator-crds/tests \
	  && helm unittest deploy/charts/runtime-overrides-operator-crds/ \
	  || echo "no helm-unittest suite for the CRD chart yet; skipping"

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	"$(CONTROLLER_GEN)" object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: test
test: manifests generate fmt vet setup-envtest ## Run tests.
	KUBEBUILDER_ASSETS="$(shell "$(ENVTEST)" use $(ENVTEST_K8S_VERSION) --bin-dir "$(LOCALBIN)" -p path)" go test $$(go list ./... | grep -v /e2e) -coverprofile cover.out

# TODO(user): To use a different vendor for e2e tests, modify the setup under 'tests/e2e'.
# The default setup assumes Kind is pre-installed and builds/loads the Manager Docker image locally.
# CertManager is installed by default; skip with:
# - CERT_MANAGER_INSTALL_SKIP=true
KIND_CLUSTER ?= runtime-overrides-operator-test-e2e
KIND_REGISTRY_NAME ?= kind-registry
KIND_REGISTRY_PORT ?= 5001
E2E_IMAGE_REPO ?= localhost:$(KIND_REGISTRY_PORT)/runtime-overrides-operator
E2E_IMAGE_TAG ?= e2e

.PHONY: e2e-registry-up
e2e-registry-up: ## Start the local OCI registry sidecar and put it on the kind network before cluster create.
	@# Pre-create the `kind` Docker network so the registry can join it
	@# BEFORE `kind create cluster` runs. Without this, containerd in the
	@# cluster boots with a mirror config pointing at `kind-registry:5000`
	@# but the hostname doesn't resolve (the registry is still on bridge
	@# only), and containerd's startup hangs trying the mirror — kubelet
	@# never goes healthy and `kind create cluster` times out at the
	@# 4-minute mark. Joining the kind network upfront keeps the
	@# in-network hostname resolvable from the first containerd boot.
	@if ! docker network inspect kind >/dev/null 2>&1; then \
	  echo "Creating Docker network 'kind'..." ; \
	  docker network create kind >/dev/null ; \
	fi
	@if [ -z "$$(docker ps -q -f name=^/$(KIND_REGISTRY_NAME)$$)" ]; then \
	  echo "Starting local registry $(KIND_REGISTRY_NAME) on 127.0.0.1:$(KIND_REGISTRY_PORT)" ; \
	  docker run -d --restart=always \
	    -p 127.0.0.1:$(KIND_REGISTRY_PORT):5000 \
	    --network kind \
	    --name $(KIND_REGISTRY_NAME) \
	    registry:2 >/dev/null ; \
	else \
	  echo "Local registry $(KIND_REGISTRY_NAME) already running." ; \
	  if ! docker network inspect kind --format '{{range .Containers}}{{.Name}} {{end}}' 2>/dev/null | grep -qw $(KIND_REGISTRY_NAME); then \
	    echo "Reconnecting $(KIND_REGISTRY_NAME) to the kind network..." ; \
	    docker network connect kind $(KIND_REGISTRY_NAME) >/dev/null 2>&1 || true ; \
	  fi ; \
	fi

.PHONY: e2e-registry-down
e2e-registry-down: ## Stop and remove the local OCI registry sidecar.
	@if [ -n "$$(docker ps -aq -f name=^/$(KIND_REGISTRY_NAME)$$)" ]; then \
	  echo "Removing local registry $(KIND_REGISTRY_NAME)..." ; \
	  docker rm -f $(KIND_REGISTRY_NAME) >/dev/null ; \
	fi

.PHONY: setup-test-e2e
setup-test-e2e: e2e-registry-up ## Set up a Kind cluster (with local-registry mirror) for e2e tests if it does not exist
	@command -v $(KIND) >/dev/null 2>&1 || { \
		echo "Kind is not installed. Please install Kind manually."; \
		exit 1; \
	}
	@case "$$($(KIND) get clusters)" in \
		*"$(KIND_CLUSTER)"*) \
			echo "Kind cluster '$(KIND_CLUSTER)' already exists. Skipping creation." ;; \
		*) \
			echo "Creating Kind cluster '$(KIND_CLUSTER)' with local-registry containerd patch..."; \
			$(KIND) create cluster --name $(KIND_CLUSTER) --config test/e2e/kind-config.yaml ;; \
	esac
	@# Write the per-host containerd config that maps `localhost:$(KIND_REGISTRY_PORT)`
	@# (what `ko build` pushes to and what the chart's image.repository
	@# references) to `http://kind-registry:5000` (the in-cluster
	@# hostname for the registry sidecar). The hosts.toml-per-directory
	@# layout is containerd 2.x's modern registry config — the legacy
	@# `[plugins."io.containerd.grpc.v1.cri".registry.mirrors.*]` form is
	@# rejected by containerd 2.x and would crash containerd at startup.
	@echo "Writing containerd hosts.toml on each kind node..."
	@for node in $$($(KIND) get nodes --name $(KIND_CLUSTER)); do \
	  docker exec "$$node" mkdir -p "/etc/containerd/certs.d/localhost:$(KIND_REGISTRY_PORT)" ; \
	  printf '[host."http://%s:5000"]\n  capabilities = ["pull", "resolve"]\n' "$(KIND_REGISTRY_NAME)" \
	    | docker exec -i "$$node" tee "/etc/containerd/certs.d/localhost:$(KIND_REGISTRY_PORT)/hosts.toml" >/dev/null ; \
	done

.PHONY: e2e-image
e2e-image: ko ## Build the operator image and push it to the e2e local registry (much faster than `kind load`).
	@host_platform="linux/$$(go env GOARCH)" ; \
	  echo "ko build → $(E2E_IMAGE_REPO):$(E2E_IMAGE_TAG) ($$host_platform, local registry)" ; \
	  VERSION="$(E2E_IMAGE_TAG)" KO_DOCKER_REPO="$(E2E_IMAGE_REPO)" \
	    $(KO) build --bare --tags "$(E2E_IMAGE_TAG)" --platform="$$host_platform" ./cmd

.PHONY: test-e2e
# Run via the ginkgo CLI so we can use --procs=N to parallelize specs
# across N processes (the parallel runner spawns multiple test binaries
# and distributes specs between them). Each parallel-safe spec creates
# its own ephemeral tenant namespace; specs marked Serial in the suite
# run in a single process while the others drain.
#
# --procs=2 matches the GitHub-hosted runner's CPU count. Local hardware
# with more cores can override (e.g. `make test-e2e GINKGO_PROCS=4`).
#
# --timeout=25m caps total runtime; locally the parallel suite finishes
# in ~4-5min, but the timeout gives meaningful headroom on slow CI.
GINKGO_PROCS ?= 2
test-e2e: setup-test-e2e manifests generate fmt vet ginkgo ## Run the e2e tests. Expected an isolated environment using Kind.
	KIND=$(KIND) KIND_CLUSTER=$(KIND_CLUSTER) \
	  "$(GINKGO)" --tags=e2e -v --procs=$(GINKGO_PROCS) --timeout=25m ./test/e2e/
	$(MAKE) cleanup-test-e2e

.PHONY: cleanup-test-e2e
cleanup-test-e2e: ## Tear down the Kind cluster used for e2e tests
	@$(KIND) delete cluster --name $(KIND_CLUSTER)
	@# Leave the registry container running by default — re-runs reuse the
	@# already-pulled image layers. Use `make e2e-registry-down` to remove.

.PHONY: lint
lint: golangci-lint ## Run golangci-lint linter
	"$(GOLANGCI_LINT)" run

.PHONY: lint-fix
lint-fix: golangci-lint ## Run golangci-lint linter and perform fixes
	"$(GOLANGCI_LINT)" run --fix

.PHONY: lint-config
lint-config: golangci-lint ## Verify golangci-lint linter configuration
	"$(GOLANGCI_LINT)" config verify

##@ Build

.PHONY: build
build: manifests generate fmt vet ## Build manager binary.
	go build -o bin/manager cmd/main.go

.PHONY: run
run: manifests generate fmt vet ## Run a controller from your host.
	go run ./cmd/main.go

# If you wish to build the manager image targeting other platforms you can use the --platform flag.
# (i.e. docker build --platform linux/arm64). However, you must enable docker buildKit for it.
# Image builds use ko (.ko.yaml), not a Dockerfile. ko reads go.mod for the
# Go toolchain, so there's no separate Go-version pin to keep in sync, and
# it builds the WHOLE ./cmd package (not a single .go file), which fixes the
# "undefined: writeBanner" trap a Dockerfile that does `go build cmd/main.go`
# falls into.
.PHONY: docker-build
docker-build: ko ## Build the operator image and load it into the local Docker daemon as ${IMG}.
	@: "$${IMG:?IMG=<repo>:<tag> required}"
	@repo="$${IMG%%:*}" ; tag="$${IMG##*:}" ; \
	  if [ "$$repo" = "$$tag" ]; then tag="latest" ; fi ; \
	  host_platform="linux/$$(go env GOARCH)" ; \
	  echo "ko build → $$repo:$$tag ($$host_platform, local Docker)" ; \
	  VERSION="$$tag" KO_DOCKER_REPO="$$repo" \
	    $(KO) build --bare --tags "$$tag" --local --platform="$$host_platform" ./cmd

.PHONY: docker-push
docker-push: ko ## Build + push the operator image to ${IMG}'s repo (multi-arch).
	@: "$${IMG:?IMG=<repo>:<tag> required}"
	@repo="$${IMG%%:*}" ; tag="$${IMG##*:}" ; \
	  if [ "$$repo" = "$$tag" ]; then tag="latest" ; fi ; \
	  echo "ko build → $$repo:$$tag (pushed)" ; \
	  VERSION="$$tag" KO_DOCKER_REPO="$$repo" \
	    $(KO) build --bare --tags "$$tag" \
	    --platform=linux/amd64,linux/arm64 ./cmd

.PHONY: build-installer
build-installer: manifests generate kustomize ## Generate a consolidated YAML with CRDs and deployment.
	mkdir -p dist
	cd config/manager && "$(KUSTOMIZE)" edit set image controller=${IMG}
	"$(KUSTOMIZE)" build config/default > dist/install.yaml

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: install
install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	@out="$$( "$(KUSTOMIZE)" build config/crd 2>/dev/null || true )"; \
	if [ -n "$$out" ]; then echo "$$out" | "$(KUBECTL)" apply -f -; else echo "No CRDs to install; skipping."; fi

.PHONY: uninstall
uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	@out="$$( "$(KUSTOMIZE)" build config/crd 2>/dev/null || true )"; \
	if [ -n "$$out" ]; then echo "$$out" | "$(KUBECTL)" delete --ignore-not-found=$(ignore-not-found) -f -; else echo "No CRDs to delete; skipping."; fi

.PHONY: deploy
deploy: manifests kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && "$(KUSTOMIZE)" edit set image controller=${IMG}
	"$(KUSTOMIZE)" build config/default | "$(KUBECTL)" apply -f -

.PHONY: undeploy
undeploy: kustomize ## Undeploy controller from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	"$(KUSTOMIZE)" build config/default | "$(KUBECTL)" delete --ignore-not-found=$(ignore-not-found) -f -

##@ Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p "$(LOCALBIN)"

## Tool Binaries
KUBECTL ?= kubectl
KIND ?= kind
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest
GOLANGCI_LINT = $(LOCALBIN)/golangci-lint
KO ?= $(LOCALBIN)/ko
GINKGO ?= $(LOCALBIN)/ginkgo

## Tool Versions
KUSTOMIZE_VERSION ?= v5.8.1
CONTROLLER_TOOLS_VERSION ?= v0.21.0
# Keep aligned with the github.com/onsi/ginkgo/v2 version in go.mod —
# the ginkgo CLI's parallel runner needs to match the library's
# internal protocol version.
GINKGO_VERSION ?= v2.29.0

#ENVTEST_VERSION is the version of controller-runtime release branch to fetch the envtest setup script (i.e. release-0.20)
ENVTEST_VERSION ?= $(shell v='$(call gomodver,sigs.k8s.io/controller-runtime)'; \
  [ -n "$$v" ] || { echo "Set ENVTEST_VERSION manually (controller-runtime replace has no tag)" >&2; exit 1; }; \
  printf '%s\n' "$$v" | sed -E 's/^v?([0-9]+)\.([0-9]+).*/release-\1.\2/')

#ENVTEST_K8S_VERSION is the version of Kubernetes to use for setting up ENVTEST binaries (i.e. 1.31)
ENVTEST_K8S_VERSION ?= $(shell v='$(call gomodver,k8s.io/api)'; \
  [ -n "$$v" ] || { echo "Set ENVTEST_K8S_VERSION manually (k8s.io/api replace has no tag)" >&2; exit 1; }; \
  printf '%s\n' "$$v" | sed -E 's/^v?[0-9]+\.([0-9]+).*/1.\1/')

GOLANGCI_LINT_VERSION ?= v2.12.2
KO_VERSION ?= v0.18.1
.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary.
$(KUSTOMIZE): $(LOCALBIN)
	$(call go-install-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v5,$(KUSTOMIZE_VERSION))

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	$(call go-install-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen,$(CONTROLLER_TOOLS_VERSION))

.PHONY: setup-envtest
setup-envtest: envtest ## Download the binaries required for ENVTEST in the local bin directory.
	@echo "Setting up envtest binaries for Kubernetes version $(ENVTEST_K8S_VERSION)..."
	@"$(ENVTEST)" use $(ENVTEST_K8S_VERSION) --bin-dir "$(LOCALBIN)" -p path || { \
		echo "Error: Failed to set up envtest binaries for version $(ENVTEST_K8S_VERSION)."; \
		exit 1; \
	}

.PHONY: envtest
envtest: $(ENVTEST) ## Download setup-envtest locally if necessary.
$(ENVTEST): $(LOCALBIN)
	$(call go-install-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest,$(ENVTEST_VERSION))

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Download golangci-lint locally if necessary.
$(GOLANGCI_LINT): $(LOCALBIN)
	$(call go-install-tool,$(GOLANGCI_LINT),github.com/golangci/golangci-lint/v2/cmd/golangci-lint,$(GOLANGCI_LINT_VERSION))

.PHONY: ko
ko: $(KO) ## Download ko locally if necessary.
$(KO): $(LOCALBIN)
	$(call go-install-tool,$(KO),github.com/google/ko,$(KO_VERSION))

.PHONY: ginkgo
ginkgo: $(GINKGO) ## Download ginkgo CLI locally if necessary.
$(GINKGO): $(LOCALBIN)
	$(call go-install-tool,$(GINKGO),github.com/onsi/ginkgo/v2/ginkgo,$(GINKGO_VERSION))

# go-install-tool will 'go install' any package with custom target and name of binary, if it doesn't exist
# $1 - target path with name of binary
# $2 - package url which can be installed
# $3 - specific version of package
define go-install-tool
@[ -f "$(1)-$(3)" ] && [ "$$(readlink -- "$(1)" 2>/dev/null)" = "$(1)-$(3)" ] || { \
set -e; \
package=$(2)@$(3) ;\
echo "Downloading $${package}" ;\
rm -f "$(1)" ;\
GOBIN="$(LOCALBIN)" go install $${package} ;\
mv "$(LOCALBIN)/$$(basename "$(1)")" "$(1)-$(3)" ;\
} ;\
ln -sf "$$(realpath "$(1)-$(3)")" "$(1)"
endef

define gomodver
$(shell go list -m -f '{{if .Replace}}{{.Replace.Version}}{{else}}{{.Version}}{{end}}' $(1) 2>/dev/null)
endef

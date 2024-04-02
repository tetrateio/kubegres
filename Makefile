
# Image URL to use all building/pushing image targets
LATEST = kubegres:latest
IMG ?= $(LATEST)

PLATFORMS ?= linux/amd64,linux/arm64

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

comma  := ,
space  := $(empty) $(empty)

.PHONY: all
all: build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk commands is responsible for reading the
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
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: test
test: build envtest kind ## Run tests.
	KIND_EXEC_PATH=$(KIND) go test $(shell pwd)/test -run $(shell pwd)/test/suite_test.go -v -test.timeout 10000s

##@ Build

.PHONY: build
build: manifests generate fmt vet ## Build manager binary.
	go generate
	go build -o bin/manager main.go

.PHONY: run
run: install ## Run a controller from your host.
	go run ./main.go

DOCKER_BUILDER_NAME=kubegres
.PHONY: run
docker-buildx:
	docker buildx inspect $(DOCKER_BUILDER_NAME) || \
	docker buildx create --name $(DOCKER_BUILDER_NAME) --driver docker-container --driver-opt network=host --buildkitd-flags '--allow-insecure-entitlement network.host'

#docker-build: test ## Build docker image with the manager.
.PHONY: docker-build-push
docker-build-push: build docker-buildx ## Build docker image with the manager.
	docker buildx build --builder $(DOCKER_BUILDER_NAME) --platform ${PLATFORMS} -t ${IMG} --push .

.PHONY: docker-build
docker-build: $(addprefix docker-build/,$(subst $(comma),$(space),$(PLATFORMS))) ## Build docker images for all platforms.

# Intentionally build the image for a specific platform, using arch as the image tag suffix so we avoid overwriting the multi-arch images.
.PHONY: docker-build/%
docker-build/%: PLATFORM=$(*)
docker-build/%: DOCKER_ARCH=$(notdir $(PLATFORM))
docker-build/%: docker-buildx ## Build docker image with ARCH as image tag suffix.
	docker buildx build --builder $(DOCKER_BUILDER_NAME) --platform ${PLATFORM} -t ${IMG}-${DOCKER_ARCH} --load .

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: install
install: build kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

.PHONY: uninstall
uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/crd | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

.PHONY: deploy-check
deploy-check:
ifeq ($(IMG),$(LATEST))
	@echo "PLEASE PROVIDE THE ARGUMENT 'IMG' WHEN RUNNING 'make deploy'. EXAMPLE OF USAGE: 'make deploy IMG=reactivetechio/kubegres:1.16'"
	exit 1
endif
	@echo "RUNNING THE ACCEPTANCE TESTS AND THEN WILL DEPLOY $(IMG) INTO DOCKER HUB."

## Usage: 'make deploy IMG=reactivetechio/kubegres:[version]'
## eg: 'make deploy IMG=reactivetechio/kubegres:1.16'
## Run acceptance tests then deploy into Docker Hub the controller as the Docker image provided in arg ${IMG}
## and update the local file "kubegres.yaml" with the image ${IMG}
.PHONY: deploy
deploy: deploy-check kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config/default > kubegres.yaml
	@echo "DEPLOYED $(IMG) INTO DOCKER HUB. UPDATED 'kubegres.yaml' WITH '$(IMG)'. YOU CAN COMMIT 'kubegres.yaml' AND CREATE A RELEASE IN GITHUB."

.PHONY: undeploy
undeploy: ## Undeploy controller from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/default | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

##@ Build Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest
KIND ?= $(LOCALBIN)/kind

## Tool Versions
KUSTOMIZE_VERSION ?= v3.8.7
CONTROLLER_TOOLS_VERSION ?= v0.9.2
KIND_VERSION ?= v0.19.0
KUBEBUILDER_TOOLS_VERSION := 1.24.2

## Kubebuilder Tools (etcd, kube-apiserver)
# using tar instead of go install to be able to pin the version
KUBEBUILDER_TOOLS_OS ?= $(shell go env GOOS)
KUBEBUILDER_TOOLS_ARCH ?= $(shell go env GOARCH)
KUBEBUILDER_TOOLS_TGZ := $(LOCALBIN)/kubebuilder-tools-$(KUBEBUILDER_TOOLS_VERSION)-$(KUBEBUILDER_TOOLS_OS)-$(KUBEBUILDER_TOOLS_ARCH).tar.gz
KUBEBUILDER_TOOLS_DIR := $(LOCALBIN)/kubebuilder-tools-$(KUBEBUILDER_TOOLS_VERSION)-$(KUBEBUILDER_TOOLS_OS)-$(KUBEBUILDER_TOOLS_ARCH)
KUBEBUILDER_TOOLS_BINARIES := bin/etcd bin/kube-apiserver
KUBEBUILDER_TOOLS := $(foreach binary,$(KUBEBUILDER_TOOLS_BINARIES),$(KUBEBUILDER_TOOLS_DIR)/$(binary))
# KUBEBUILDER_ASSETS environment variable will be recognized by the `controller-runtime` test framework
export KUBEBUILDER_ASSETS=$(KUBEBUILDER_TOOLS_DIR)/bin

KUSTOMIZE_INSTALL_SCRIPT ?= "https://raw.githubusercontent.com/kubernetes-sigs/kustomize/master/hack/install_kustomize.sh"
.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary.
$(KUSTOMIZE): $(LOCALBIN)
	test -s $(LOCALBIN)/kustomize || { curl -s $(KUSTOMIZE_INSTALL_SCRIPT) | bash -s -- $(subst v,,$(KUSTOMIZE_VERSION)) $(LOCALBIN); }

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	test -s $(LOCALBIN)/controller-gen || GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)

.PHONY: kind
kind: $(KIND) ## Download kind locally if necessary.
$(KIND): $(LOCALBIN)
	test -s $(LOCALBIN)/kind || GOBIN=$(LOCALBIN) go install sigs.k8s.io/kind@$(KIND_VERSION)

.PHONY: envtest
envtest: $(KUBEBUILDER_TOOLS) ## Download envtest-setup locally if necessary.
$(KUBEBUILDER_TOOLS):
	@echo "(re)installing kubebuilder-tools-$(KUBEBUILDER_TOOLS_VERSION)"
	@mkdir -p $(dir $(KUBEBUILDER_TOOLS_TGZ))
	@curl -Lo $(KUBEBUILDER_TOOLS_TGZ) \
	  "https://storage.googleapis.com/kubebuilder-tools/kubebuilder-tools-$(KUBEBUILDER_TOOLS_VERSION)-$(KUBEBUILDER_TOOLS_OS)-$(KUBEBUILDER_TOOLS_ARCH).tar.gz"
	@mkdir -p $(KUBEBUILDER_TOOLS_DIR)
	@tar -xvf $(KUBEBUILDER_TOOLS_TGZ) -C $(KUBEBUILDER_TOOLS_DIR) --strip-components 1

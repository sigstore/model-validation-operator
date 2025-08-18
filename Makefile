# VERSION defines the project version for the bundle.
# Update this value when you upgrade the version of your project.
# To re-generate a bundle for another specific version without changing the standard setup, you can:
# - use the VERSION as arg of the bundle target (e.g make bundle VERSION=0.0.2)
# - use environment variables to overwrite this value (e.g export VERSION=0.0.2)
VERSION ?= 0.0.1

# CHANNELS define the bundle channels used in the bundle.
# Add a new line here if you would like to change its default config. (E.g CHANNELS = "candidate,fast,stable")
# To re-generate a bundle for other specific channels without changing the standard setup, you can:
# - use the CHANNELS as arg of the bundle target (e.g make bundle CHANNELS=candidate,fast,stable)
# - use environment variables to overwrite this value (e.g export CHANNELS="candidate,fast,stable")
ifneq ($(origin CHANNELS), undefined)
BUNDLE_CHANNELS := --channels=$(CHANNELS)
endif

# DEFAULT_CHANNEL defines the default channel used in the bundle.
# Add a new line here if you would like to change its default config. (E.g DEFAULT_CHANNEL = "stable")
# To re-generate a bundle for any other default channel without changing the default setup, you can:
# - use the DEFAULT_CHANNEL as arg of the bundle target (e.g make bundle DEFAULT_CHANNEL=stable)
# - use environment variables to overwrite this value (e.g export DEFAULT_CHANNEL="stable")
ifneq ($(origin DEFAULT_CHANNEL), undefined)
BUNDLE_DEFAULT_CHANNEL := --default-channel=$(DEFAULT_CHANNEL)
endif
BUNDLE_METADATA_OPTS ?= $(BUNDLE_CHANNELS) $(BUNDLE_DEFAULT_CHANNEL)

# IMAGE_TAG_BASE defines the docker.io namespace and part of the image name for remote images.
# This variable is used to construct full image tags for bundle and catalog images.
#
# For example, running 'make bundle-build bundle-push catalog-build catalog-push' will build and push both
# ghcr.io/sigstore/model-validation-operator-bundle:$VERSION and ghcr.io/sigstore/model-validation-operator-catalog:$VERSION.
IMAGE_TAG_BASE ?= ghcr.io/sigstore/model-validation-operator

# IMG defines the image:tag used for the operator.
IMG ?= $(IMAGE_TAG_BASE):v$(VERSION)

# BUNDLE_IMG defines the image:tag used for the bundle.
# You can use it as an arg. (E.g make bundle-build BUNDLE_IMG=<some-registry>/<project-name-bundle>:<tag>)
BUNDLE_IMG ?= $(IMAGE_TAG_BASE)-bundle:v$(VERSION)

# BUNDLE_GEN_FLAGS are the flags passed to the operator-sdk generate bundle command
BUNDLE_GEN_FLAGS ?= -q --overwrite --version $(VERSION) $(BUNDLE_METADATA_OPTS)

# BUNDLE_OVERLAY defines which overlay to use for bundle generation (e.g. make bundle BUNDLE_OVERLAY=production)
BUNDLE_OVERLAY ?= olm

# USE_IMAGE_DIGESTS defines if images are resolved via tags or digests
# You can enable this value if you would like to use SHA Based Digests
# To enable set flag to true
USE_IMAGE_DIGESTS ?= false
ifeq ($(USE_IMAGE_DIGESTS), true)
	BUNDLE_GEN_FLAGS += --use-image-digests
endif

# Set the Operator SDK version to use. By default, what is installed on the system is used.
# This is useful for CI or a project to utilize a specific version of the operator-sdk toolkit.
OPERATOR_SDK_VERSION ?= v1.41.1

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
# Dockerfile was renamed to Containerfile, presumably for podman support, however
# this makefile explicitly mentions Dockerfile, so we parameterize it
CONTAINER_FILE ?= Dockerfile

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
test: manifests generate fmt vet setup-envtest ## Run tests.
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" go test $$(go list ./... | grep -v /e2e) -coverprofile cover.out

.PHONY: lint
lint: golangci-lint ## Run golangci-lint linter
	$(GOLANGCI_LINT) run

.PHONY: lint-fix
lint-fix: golangci-lint ## Run golangci-lint linter and perform fixes
	$(GOLANGCI_LINT) run --fix

.PHONY: lint-config
lint-config: golangci-lint ## Verify golangci-lint linter configuration
	$(GOLANGCI_LINT) config verify

##@ Build

.PHONY: build
build: manifests generate fmt vet ## Build manager binary.
	go build -o bin/manager cmd/main.go

.PHONY: run
run: manifests generate fmt vet generate-local-certs ## Run a controller from your host.
	go run ./cmd/main.go

.PHONY: generate-certs
generate-certs: ## Generate TLS certificates to specified directory (use CERT_DIR=path)
	@if [ -z "$(CERT_DIR)" ]; then \
		echo "Error: CERT_DIR must be specified. Usage: make generate-certs CERT_DIR=/path/to/certs"; \
		exit 1; \
	fi
	@echo "Generating TLS certificates in $(CERT_DIR)..."
	@if command -v cfssl &> /dev/null && [[ -f "generate-tls.sh" ]]; then \
		echo "Using cfssl-based certificate generation"; \
		./generate-tls.sh $(CERT_DIR); \
	elif [[ -f "generate-tls-openssl.sh" ]]; then \
		echo "Using OpenSSL-based certificate generation"; \
		./generate-tls-openssl.sh $(CERT_DIR); \
	else \
		echo "Error: No TLS generation script found. Either install cfssl or ensure generate-tls-openssl.sh exists."; \
		exit 1; \
	fi

.PHONY: generate-local-certs
generate-local-certs: ## Generate TLS certificates for local development
	@echo "Generating local webhook certificates..."
	@CERT_DIR=$$(mktemp -d) && \
	$(MAKE) generate-certs CERT_DIR=$$CERT_DIR && \
	mkdir -p "$$TMPDIR/k8s-webhook-server/serving-certs" && \
	cp $$CERT_DIR/tls.crt "$$TMPDIR/k8s-webhook-server/serving-certs/" && \
	cp $$CERT_DIR/tls.key "$$TMPDIR/k8s-webhook-server/serving-certs/" && \
	echo "Certificates generated and placed in $$TMPDIR/k8s-webhook-server/serving-certs/" && \
	echo "You can now run 'make run' to start the controller with webhook support"

# If you wish to build the manager image targeting other platforms you can use the --platform flag.
# (i.e. docker build --platform linux/arm64). However, you must enable docker buildKit for it.
# More info: https://docs.docker.com/develop/develop-images/build_enhancements/
.PHONY: docker-build
docker-build: test ## Build docker image with the manager.
	$(CONTAINER_TOOL) build -t ${IMG} -f ${CONTAINER_FILE} .

.PHONY: docker-push
docker-push: ## Push docker image with the manager.
	$(CONTAINER_TOOL) push ${IMG}

# PLATFORMS defines the target platforms for the manager image be built to provide support to multiple
# architectures. (i.e. make docker-buildx IMG=myregistry/mypoperator:0.0.1). To use this option you need to:
# - be able to use docker buildx. More info: https://docs.docker.com/build/buildx/
# - have enabled BuildKit. More info: https://docs.docker.com/develop/develop-images/build_enhancements/
# - be able to push the image to your registry (i.e. if you do not set a valid value via IMG=<myregistry/image:<tag>> then the export will fail)
# To adequately provide solutions that are compatible with multiple platforms, you should consider using this option.
PLATFORMS ?= linux/arm64,linux/amd64,linux/s390x,linux/ppc64le
.PHONY: docker-buildx
docker-buildx: ## Build and push docker image for the manager for cross-platform support
	# copy existing Dockerfile and insert --platform=${BUILDPLATFORM} into Dockerfile.cross, and preserve the original Dockerfile
	sed -e '1 s/\(^FROM\)/FROM --platform=\$$\{BUILDPLATFORM\}/; t' -e ' 1,// s//FROM --platform=\$$\{BUILDPLATFORM\}/' ${CONTAINER_FILE} > ${CONTAINER_FILE}.cross
	- $(CONTAINER_TOOL) buildx create --name model-validation-operator-builder
	$(CONTAINER_TOOL) buildx use model-validation-operator-builder
	- $(CONTAINER_TOOL) buildx build --push --platform=$(PLATFORMS) --tag ${IMG} -f ${CONTAINER_FILE}.cross .
	- $(CONTAINER_TOOL) buildx rm model-validation-operator-builder
	rm ${CONTAINER_FILE}.cross

.PHONY: build-installer
build-installer: manifests ## Generate a consolidated YAML with CRDs and deployment.
	./scripts/generate-manifests.sh production dist
	mv dist/production.yaml dist/install.yaml

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: install
install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | $(KUBECTL) apply -f -

.PHONY: uninstall
uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/crd | $(KUBECTL) delete --ignore-not-found=$(ignore-not-found) -f -

.PHONY: deploy
deploy: manifests kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/overlays/production && $(KUSTOMIZE) edit set image controller=${IMG}
	cd config/overlays/production && $(KUSTOMIZE) edit set replicas controller-manager=1
	$(KUSTOMIZE) build config/overlays/production | $(KUBECTL) apply -f -

.PHONY: undeploy
undeploy: kustomize ## Undeploy controller from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/overlays/production | $(KUBECTL) delete --ignore-not-found=$(ignore-not-found) -f -

##@ Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
KUBECTL ?= kubectl
KIND ?= kind
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest
GOLANGCI_LINT = $(LOCALBIN)/golangci-lint

## Tool Versions
KUSTOMIZE_VERSION ?= v5.7.0
CONTROLLER_TOOLS_VERSION ?= v0.18.0
ENVTEST_VERSION ?= $(shell go list -m -f "{{ .Version }}" sigs.k8s.io/controller-runtime | awk -F'[v.]' '{printf "release-%d.%d", $$2, $$3}')
#ENVTEST_K8S_VERSION is the version of Kubernetes to use for setting up ENVTEST binaries (i.e. 1.31)
ENVTEST_K8S_VERSION ?= $(shell go list -m -f "{{ .Version }}" k8s.io/api | awk -F'[v.]' '{printf "1.%d", $$3}')
GOLANGCI_LINT_VERSION ?= v2.3.0

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
	@$(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path || { \
		echo "Error: Failed to set up envtest binaries for version $(ENVTEST_K8S_VERSION)."; \
		exit 1; \
	} ; echo

.PHONY: envtest
envtest: $(ENVTEST) ## Download setup-envtest locally if necessary.
$(ENVTEST): $(LOCALBIN)
	$(call go-install-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest,$(ENVTEST_VERSION))

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Download golangci-lint locally if necessary.
$(GOLANGCI_LINT): $(LOCALBIN)
	$(call go-install-tool,$(GOLANGCI_LINT),github.com/golangci/golangci-lint/v2/cmd/golangci-lint,$(GOLANGCI_LINT_VERSION))

# go-install-tool will 'go install' any package with custom target and name of binary, if it doesn't exist
# $1 - target path with name of binary
# $2 - package url which can be installed
# $3 - specific version of package
define go-install-tool
@[ -f "$(1)-$(3)" ] || { \
set -e; \
package=$(2)@$(3) ;\
echo "Downloading $${package}" ;\
rm -f $(1) || true ;\
GOBIN=$(LOCALBIN) go install $${package} ;\
mv $(1) $(1)-$(3) ;\
} ;\
ln -sf $(1)-$(3) $(1)
endef

.PHONY: operator-sdk
OPERATOR_SDK ?= $(LOCALBIN)/operator-sdk
operator-sdk: ## Download operator-sdk locally if necessary.
ifeq (,$(wildcard $(OPERATOR_SDK)))
ifeq (, $(shell which operator-sdk 2>/dev/null))
	@{ \
	set -e ;\
	mkdir -p $(dir $(OPERATOR_SDK)) ;\
	OS=$(shell go env GOOS) && ARCH=$(shell go env GOARCH) && \
	curl -sSLo $(OPERATOR_SDK) https://github.com/operator-framework/operator-sdk/releases/download/$(OPERATOR_SDK_VERSION)/operator-sdk_$${OS}_$${ARCH} ;\
	chmod +x $(OPERATOR_SDK) ;\
	}
else
OPERATOR_SDK = $(shell which operator-sdk)
endif
endif

.PHONY: bundle
bundle: manifests kustomize operator-sdk ## Generate bundle manifests and metadata from $(BUNDLE_OVERLAY) overlay, then validate generated files.
	$(OPERATOR_SDK) generate kustomize manifests -q
	cd config/overlays/$(BUNDLE_OVERLAY) && $(KUSTOMIZE) edit set image controller=$(IMG)
	$(KUSTOMIZE) build config/overlays/$(BUNDLE_OVERLAY) | $(OPERATOR_SDK) generate bundle $(BUNDLE_GEN_FLAGS)
	# Fix webhook configuration in CSV
	@if [ -f bundle/manifests/model-validation-operator.clusterserviceversion.yaml ]; then \
		sed -i.bak 's/deploymentName: webhook/deploymentName: model-validation-controller-manager/' bundle/manifests/model-validation-operator.clusterserviceversion.yaml && \
		sed -i.bak2 's/deploymentName: model-validation-controller-manager/deploymentName: model-validation-controller-manager\n    serviceName: model-validation-webhook/' bundle/manifests/model-validation-operator.clusterserviceversion.yaml && \
		rm -f bundle/manifests/model-validation-operator.clusterserviceversion.yaml.bak bundle/manifests/model-validation-operator.clusterserviceversion.yaml.bak2; \
	fi
	$(OPERATOR_SDK) bundle validate ./bundle

.PHONY: bundle-build
bundle-build: ## Build the bundle image.
	$(CONTAINER_TOOL) build -f bundle.${CONTAINER_FILE} -t $(BUNDLE_IMG) .

.PHONY: bundle-push
bundle-push: ## Push the bundle image.
	$(MAKE) docker-push IMG=$(BUNDLE_IMG)

.PHONY: opm
OPM = $(LOCALBIN)/opm
OPM_VERSION=v1.56.0
opm: ## Download opm locally if necessary.
ifeq (,$(wildcard $(OPM)))
ifeq (,$(shell which opm 2>/dev/null))
	@{ \
	set -e ;\
	mkdir -p $(dir $(OPM)) ;\
	OS=$(shell go env GOOS) && ARCH=$(shell go env GOARCH) && \
	curl -sSLo $(OPM) https://github.com/operator-framework/operator-registry/releases/download/${OPM_VERSION}/$${OS}-$${ARCH}-opm ;\
	chmod +x $(OPM) ;\
	}
else
OPM = $(shell which opm)
endif
endif

# A comma-separated list of bundle images (e.g. make catalog-build BUNDLE_IMGS=ghcr.io/sigstore/model-validation-operator-bundle:v0.1.0,ghcr.io/sigstore/model-validation-operator-bundle:v0.2.0).
# These images MUST exist in a registry and be pull-able.
BUNDLE_IMGS ?= $(BUNDLE_IMG)

# The image tag given to the resulting catalog image (e.g. make catalog-build CATALOG_IMG=ghcr.io/sigstore/model-validation-operator-catalog:v0.2.0).
CATALOG_IMG ?= $(IMAGE_TAG_BASE)-catalog:v$(VERSION)

# Set CATALOG_BASE_IMG to an existing catalog image tag to add $BUNDLE_IMGS to that image.
ifneq ($(origin CATALOG_BASE_IMG), undefined)
FROM_INDEX_OPT := --from-index $(CATALOG_BASE_IMG)
endif

# Build a catalog image by adding bundle images to an empty catalog using the operator package manager tool, 'opm'.
# This recipe invokes 'opm' in 'semver' bundle add mode. For more information on add modes, see:
# https://github.com/operator-framework/community-operators/blob/7f1438c/docs/packaging-operator.md#updating-your-existing-operator
.PHONY: catalog-build
catalog-build: opm ## Build a catalog image.
	$(OPM) index add --container-tool ${CONTAINER_TOOL} --mode semver --tag $(CATALOG_IMG) --bundles $(BUNDLE_IMGS) $(FROM_INDEX_OPT)

# Push the catalog image.
.PHONY: catalog-push
catalog-push: ## Push a catalog image.
	$(MAKE) docker-push IMG=$(CATALOG_IMG)

##@ Overlay Deployment

# Define available environments
ENVIRONMENTS := testing production development olm

# Generic deployment target using generate-manifests script
define deploy-environment
.PHONY: deploy-$(1)
deploy-$(1): manifests ## Deploy to $(1) environment
	./scripts/generate-manifests.sh $(1) manifests
	kubectl apply -f manifests/$(1).yaml
endef

# Generic undeployment target
define undeploy-environment
.PHONY: undeploy-$(1)
undeploy-$(1): ## Undeploy from $(1) environment
	kubectl delete -f manifests/$(1).yaml --ignore-not-found=true
endef

# Generate targets for all environments
$(foreach env,$(ENVIRONMENTS),$(eval $(call deploy-environment,$(env))))
$(foreach env,$(ENVIRONMENTS),$(eval $(call undeploy-environment,$(env))))

# Convenience targets for all environments
.PHONY: deploy-all
deploy-all: $(addprefix deploy-,$(ENVIRONMENTS)) ## Deploy to all environments (use with caution)

.PHONY: undeploy-all
undeploy-all: $(addprefix undeploy-,$(ENVIRONMENTS)) ## Undeploy from all environments

# Generate manifests using script (replaces removed render targets)
.PHONY: generate-manifests
generate-manifests: manifests ## Generate manifests for all environments using generate-manifests script
	@for env in $(ENVIRONMENTS); do \
		echo "Generating manifests for $$env environment..."; \
		./scripts/generate-manifests.sh $$env manifests; \
	done

##@ E2E Test Infrastructure

# E2E Test Variables
E2E_OPERATOR_NAMESPACE ?= model-validation-operator-system
E2E_TEST_NAMESPACE ?= e2e-webhook-test-ns
E2E_TEST_MODEL ?= model-validation-test-model:latest
MODEL_TRANSPARENCY_IMG ?= ghcr.io/sigstore/model-transparency-cli:v1.0.1
CERTMANAGER_VERSION ?= v1.18.2
CERT_MANAGER_YAML ?= https://github.com/cert-manager/cert-manager/releases/download/$(CERTMANAGER_VERSION)/cert-manager.yaml
KIND_CLUSTER ?= kind

# Build and sign test model
.PHONY: e2e-generate-test-keys
e2e-generate-test-keys:
	@echo "Generating ECDSA P-256 test keys for model signing..."
	@if [ ! -f testdata/docker/test_private_key.priv ]; then \
		echo "Generating private key..."; \
		openssl ecparam -name prime256v1 -genkey -noout -out testdata/docker/test_private_key.priv; \
	fi
	@if [ ! -f testdata/docker/test_public_key.pub ]; then \
		echo "Generating public key..."; \
		openssl ec -in testdata/docker/test_private_key.priv -pubout -out testdata/docker/test_public_key.pub; \
	fi
	@if [ ! -f testdata/docker/test_invalid_private_key.priv ]; then \
		echo "Generating invalid private key for failure tests..."; \
		openssl ecparam -name prime256v1 -genkey -noout -out testdata/docker/test_invalid_private_key.priv; \
	fi
	@if [ ! -f testdata/docker/test_invalid_public_key.pub ]; then \
		echo "Generating invalid public key for failure tests..."; \
		openssl ec -in testdata/docker/test_invalid_private_key.priv -pubout -out testdata/docker/test_invalid_public_key.pub; \
	fi

.PHONY: e2e-sign-test-model
e2e-sign-test-model: e2e-generate-test-keys
	@echo "Signing test model with private key..."
	@# Remove public key from model directory before signing to avoid including it in signature
	@rm -f testdata/tensorflow_saved_model/test_public_key.pub
	$(CONTAINER_TOOL) run --rm \
		-v $(PWD)/testdata/tensorflow_saved_model:/model \
		-v $(PWD)/testdata/docker/test_private_key.priv:/test_private_key.priv \
		--entrypoint="" \
		ghcr.io/sigstore/model-transparency-cli:v1.0.1 \
		/usr/local/bin/model_signing sign key /model \
		--private_key /test_private_key.priv \
		--signature /model/model.sig

.PHONY: e2e-build-test-model
e2e-build-test-model: e2e-sign-test-model
	@echo "Building test model image..."
	cd testdata && $(CONTAINER_TOOL) build --no-cache -t $(E2E_TEST_MODEL) -f docker/test-model.Dockerfile .

# install and uninstall cert-manager for tests

.PHONY: e2e-install-certmanager
e2e-install-certmanager:
	@echo "Installing cert-manager..."
	$(KUBECTL) apply -f $(CERT_MANAGER_YAML)
	@echo "Waiting for cert-manager to be ready..."
	$(KUBECTL) wait --for=condition=Available deployment -n cert-manager --all --timeout=120s

.PHONY: e2e-uninstall-certmanager
e2e-uninstall-certmanager: ## Uninstall cert-manager
	@echo "Uninstalling cert-manager..."
	-$(KUBECTL) delete -f $(CERT_MANAGER_YAML)

# Load test images into the kind cluster

.PHONY: e2e-build-image
e2e-build-image:
	$(CONTAINER_TOOL) build -t $(IMG) -f $(CONTAINER_FILE) .

.PHONY: e2e-load-images
e2e-load-images: e2e-build-image e2e-build-test-model
	@echo "Pulling model-transparency-cli image..."
	$(CONTAINER_TOOL) pull $(MODEL_TRANSPARENCY_IMG)
	@echo "Loading manager image into Kind cluster..."
	$(KIND) load docker-image -n $(KIND_CLUSTER) $(IMG)
	@echo "Loading model-transparency-cli image into Kind cluster..."  
	$(KIND) load docker-image -n $(KIND_CLUSTER) $(MODEL_TRANSPARENCY_IMG)
	@echo "Loading test model image into Kind cluster..."
	$(KIND) load docker-image -n $(KIND_CLUSTER) $(E2E_TEST_MODEL)

# Setup test environment (namespaces, local models on kind cluster, operator)

.PHONY: e2e-setup-namespaces
e2e-setup-namespaces:
	@echo "Creating operator namespace..."
	$(KUBECTL) create ns $(E2E_OPERATOR_NAMESPACE) || true
	@echo "Labeling operator namespace with restricted security policy..."
	$(KUBECTL) label --overwrite ns $(E2E_OPERATOR_NAMESPACE) pod-security.kubernetes.io/enforce=restricted
	@echo "Labeling operator namespace to be ignored by webhook..."
	$(KUBECTL) label --overwrite ns $(E2E_OPERATOR_NAMESPACE) validation.ml.sigstore.dev/ignore=true
	@echo "Creating test namespace..."
	$(KUBECTL) create ns $(E2E_TEST_NAMESPACE) || true

.PHONY: e2e-setup-model-data
e2e-setup-model-data: e2e-load-images e2e-setup-namespaces
	@echo "Cleaning up any existing model data DaemonSet..."
	-$(KUBECTL) delete daemonset model-data-setup -n $(E2E_TEST_NAMESPACE) 2>/dev/null || true
	@echo "Waiting for cleanup to complete..."
	@sleep 5
	@echo "Deploying model data setup DaemonSet..."
	$(KUBECTL) apply -f test/e2e/testdata/model-data-daemonset.yaml
	@echo "Waiting for model data to be available on all nodes..."
	$(KUBECTL) rollout status daemonset/model-data-setup -n $(E2E_TEST_NAMESPACE) --timeout=120s

.PHONY: e2e-deploy-operator
e2e-deploy-operator: e2e-setup-namespaces deploy
	@echo "E2E operator deployment complete"

.PHONY: e2e-wait-operator
e2e-wait-operator: ## Wait for operator pod to be ready
	@echo "Waiting for controller pod to be ready..."
	$(KUBECTL) wait --for=condition=Ready pod -l control-plane=controller-manager -n $(E2E_OPERATOR_NAMESPACE) --timeout=120s

# test environment setup and teardown - certmanager, operator and test model for testing

.PHONY: e2e-setup
e2e-setup: e2e-install-certmanager e2e-setup-model-data e2e-deploy-operator e2e-wait-operator ## Complete e2e test setup
	@echo "E2E test environment setup complete"

.PHONY: e2e-cleanup-resources
e2e-cleanup-resources: ## Clean up test resources before removing operator
	@echo "Cleaning up test resources..."
	-$(KUBECTL) delete pods --all -n $(E2E_TEST_NAMESPACE) --timeout=30s
	-$(KUBECTL) delete modelvalidations --all -n $(E2E_TEST_NAMESPACE) --timeout=30s
	-$(KUBECTL) delete daemonset model-data-setup -n $(E2E_TEST_NAMESPACE) --timeout=30s

.PHONY: e2e-teardown
e2e-teardown: e2e-cleanup-resources undeploy e2e-uninstall-certmanager
	@echo "Tearing down e2e test environment..."
	-$(KUBECTL) delete ns $(E2E_OPERATOR_NAMESPACE) --timeout=60s
	-$(KUBECTL) delete ns $(E2E_TEST_NAMESPACE) --timeout=60s

# run e2e tests

.PHONY: test-e2e
test-e2e: manifests generate fmt vet ## Run the e2e tests, no setup and teardown.  Expects the operator to be deployed.
	@echo "Running e2e tests (assumes infrastructure is already set up)..."
	go test ./test/e2e/ -v -ginkgo.v

.PHONY: test-e2e-full
test-e2e-full: manifests generate fmt vet e2e-setup ## Run e2e tests with setup and teardown
	@echo "Running e2e tests with full infrastructure setup..."
	go test ./test/e2e/ -v -ginkgo.v; \
	TEST_RESULT=$$?; \
	$(MAKE) e2e-teardown; \
	exit $$TEST_RESULT

.PHONY: test-e2e-ci
test-e2e-ci: manifests generate fmt vet e2e-setup ## Run the e2e tests, with setup.  No teardown as the CI workflow will throw away kind
	@echo "Running e2e tests with infrastructure setup for CI..."
	go test ./test/e2e/ -v -ginkgo.v


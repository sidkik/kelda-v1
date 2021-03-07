UPGRADE_TOKEN?=upgrade
KELDA_VERSION?=$(shell ./scripts/dev_version.sh)
DOCKER_REPO = gcr.io/kelda-images
DOCKER_IMAGE = ${DOCKER_REPO}/kelda:${KELDA_VERSION}
LD_FLAGS = "-X github.com/kelda-inc/kelda/pkg/version.Version=$(KELDA_VERSION) -X github.com/kelda-inc/kelda/pkg/version.KeldaImage=$(DOCKER_IMAGE) -X github.com/kelda-inc/kelda/cmd/upgradecli.Token=$(UPGRADE_TOKEN)"
VENV_BIN = venv/bin
KELDA_INC_PATH = $(GOPATH)/src/github.com/kelda-inc
CI_EXAMPLES_REPO_PATH = $(KELDA_INC_PATH)/examples

# Include all .mk files so you can have your own local configurations
include $(wildcard *.mk)

all:
	CGO_ENABLED=0 go build -ldflags $(LD_FLAGS) .

build-osx:
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -ldflags $(LD_FLAGS) -o kelda-osx .

build-linux:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags $(LD_FLAGS) -o kelda-linux .

install:
	CGO_ENABLED=0 go install -ldflags $(LD_FLAGS) .

.PHONY: clean
clean:
	rm kelda

LINTER_REPOS = \
	github.com/golangci/golangci-lint/cmd/golangci-lint \
	github.com/client9/misspell/cmd/misspell

setup-dev-tools:
	go get -u $(LINTER_REPOS)
	go install $(LINTER_REPOS)

	go get -u github.com/vektra/mockery/.../

	if [ ! -d $$GOPATH/src/k8s.io/code-generator ]; then \
		git clone --branch kubernetes-1.16.0 https://github.com/kubernetes/code-generator $$GOPATH/src/k8s.io/code-generator; \
	fi

ci-setup: setup-dev-tools
	curl https://raw.githubusercontent.com/fossas/fossa-cli/master/install.sh | bash

venv: requirements.txt
	python3 -m venv venv
	$(VENV_BIN)/pip install wheel
	$(VENV_BIN)/pip install -r requirements.txt

build-docs: venv
	$(VENV_BIN)/mkdocs build -f docs/mkdocs.yml

.PHONY: docs
docs: build-docs
	$(VENV_BIN)/mkdocs serve -f docs/mkdocs.yml

.PHONY: lint
lint:
	GOGC=50 $(GOPATH)/bin/golangci-lint run
	find ./docs -type f -not -path './docs/site/*' | xargs $(GOPATH)/bin/misspell -error

.PHONY: lint-fix
lint-fix:
	$(GOPATH)/bin/golangci-lint run --fix

.PHONY: test
test:
	go test -p 2 ./...

.PHONY: integration-test-local
integration-test-local: install docker-push
	CI_EXAMPLES_REPO_PATH="$(CI_EXAMPLES_REPO_PATH)" \
	CI_ROOT_PATH="$(KELDA_INC_PATH)/kelda/ci" \
		CI_NO_DELETE_NAMESPACE="true" \
		ci/scripts/run_local.sh

build-circle-image:
	docker build -f .circleci/Dockerfile . -t keldaio/circleci

docker-build:
	docker build --build-arg KELDA_VERSION=$(KELDA_VERSION) -t $(DOCKER_IMAGE) .

docker-push: docker-build
	docker push $(DOCKER_IMAGE)

generate:
	# Generated with libprotoc v3.7.1 and protoc-gen-go v1.3.1.
	protoc -I _proto _proto/kelda/minion/v0/minion.proto --go_out=plugins=grpc:$$GOPATH/src
	protoc -I _proto _proto/kelda/dev/v0/dev.proto --go_out=plugins=grpc:$$GOPATH/src
	protoc _proto/kelda/errors/v0/errors.proto --go_out=plugins=grpc:$$GOPATH/src
	protoc _proto/kelda/messages/v0/messages.proto --go_out=plugins=grpc:$$GOPATH/src
	go generate ./...

generate-crd:
	$$GOPATH/src/k8s.io/code-generator/generate-groups.sh "deepcopy,client,informer,lister" \
		github.com/kelda-inc/kelda/pkg/crd/client github.com/kelda-inc/kelda/pkg/crd/apis kelda:v1alpha1

coverage:
	go test -p 2 -coverpkg=./... -coverprofile=coverage.txt ./...

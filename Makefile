NAME:=mesos-dns
BUILD_CONTAINER:=$(NAME)-build
BUILD_DIR:=$(PWD)/build
CIRCLE_TEST_REPORTS?=$(PWD)
CIRCLE_ARTIFACTS?=$(PWD)/build
COVERALLS_REPO_TOKEN?="tokennotset"
DOCKER?=
GOOS:=linux
VERSION:=$(shell git describe --abbrev=4 --always --tags)


.PHONY: help
help:
	@echo "all         - run all tests and compile mesos-dns"
	@echo "artifacts   - compiles the program using gox for linux darwin and windows platforms"
	@echo "build       - compiles the program, env GOOS can be set to specifiy platform"
	@echo "buildstatic - compiles a statically linked program, env GOOS can be set to specify platform"
	@echo "clean       - remove mesos-dns and mesos-dns-build docker images"
	@echo "coveralls   - runs goveralls and uploads data to coveralls, env COVERALLS_REPO_TOKEN is needed"
	@echo "dockerbuild - create mesos-dns-build docker image"
	@echo "fmttest     - runs gofmt, returns 0 for success, 1 for failure"
	@echo "gocov       - runs gocov"
	@echo "gorace      - runs go test with race detection enabled"
	@echo "junit       - runs go test and generates junit output"
	@echo "metalinter  - runs gometalinter"
	@echo "test        - runs fmttest and the various tests against the source code"
	@echo "version     - display current version"

.PHONY: all
all: $(if $(DOCKER), dockerbuild) test build

.PHONY: artifacts
artifacts: $(if $(DOCKER), dockerbuild)
	$(if $(DOCKER), docker run --rm -v "$(CIRCLE_ARTIFACTS)":$(PWD)/output $(BUILD_CONTAINER)) gox \
		-arch=amd64 \
		-os="linux darwin windows" \
		-output="$(PWD)/output/{{.Dir}}-$(VERSION)-{{.OS}}-{{.Arch}}" \
		-ldflags="-X main.Version=$(VERSION)"

.PHONY: build
build: $(if $(DOCKER), dockerbuild)
	$(if $(DOCKER), docker run --rm -v "$(PWD)/build":$(PWD)/build -e GOOS=$(GOOS) $(BUILD_CONTAINER), env GOOS=$(GOOS)) go build \
		-ldflags="-X main.Version=$(VERSION)" \
		-o $(PWD)/build/mesos-dns

.PHONY: buildstatic
buildstatic: $(if $(DOCKER), dockerbuild)
	$(if $(DOCKER), docker run --rm -v "$(PWD)/build":$(PWD)/build -e CGO_ENABLED=0 -e GOOS=$(GOOS) $(BUILD_CONTAINER), env CGO_ENABLED=0 GOOS=$(GOOS)) go build \
		-a \
		-installsuffix cgo \
		-ldflags "-s -X main.Version=$(VERSION)" \
		-o $(PWD)/build/mesos-dns

.PHONY: clean
clean:
	docker rmi $(BUILD_CONTAINER) || true
	docker rmi $(BUILD_CONTAINER) || true

.PHONY: coveralls
coveralls: $(if $(DOCKER), dockerbuild)
	$(if $(DOCKER), docker run --rm -v "$(PWD)":/coveralls $(BUILD_CONTAINER)) goveralls \
		-service=circleci \
		-gocovdata=/coveralls/cov.json \
		-repotoken=$(COVERALLS_REPO_TOKEN) || true

.PHONY: deps
deps:
	go get -u github.com/kardianos/govendor
	go get -u github.com/mitchellh/gox
	go get -u github.com/alecthomas/gometalinter
	go get -u github.com/axw/gocov/gocov
	go get -u github.com/mattn/goveralls
	go get -u github.com/jstemmer/go-junit-report
	gometalinter --install
	govendor sync
	go install ./...
	go test -i ./...

.PHONY: dockerbuild
dockerbuild:
	docker build -t $(BUILD_CONTAINER) -f Dockerfile.test .

## Ignore vendored dependencies
## Return 1 if any files found that haven't been formatted
.PHONY: fmttest
fmttest: $(if $(DOCKER), dockerbuild)
	$(if $(DOCKER), docker run --rm $(BUILD_CONTAINER)) gofmt -l . | \
		awk '!/^vendor/ { print $0; err=1 }; END{ exit err }'

.PHONY: gocov
gocov: $(if $(DOCKER), dockerbuild)
	$(if $(DOCKER), docker run --rm $(BUILD_CONTAINER)) gocov test ./... -short -timeout=10m > $(PWD)/cov.json

.PHONY: gorace
gorace: $(if $(DOCKER), dockerbuild)
	$(if $(DOCKER), docker run --rm $(BUILD_CONTAINER)) go test -v -short -race -timeout=10m ./...

.PHONY: junit
junit: $(if $(DOCKER), dockerbuild)
	mkdir -p $(CIRCLE_TEST_REPORTS)/junit
	@# This seems odd, but if you attach stdin without attaching to an
	@# output ( stdout or stderr ), the container runs in the background
	$(if $(DOCKER), docker run --rm $(BUILD_CONTAINER)) go test -v -timeout=10m ./... | \
	$(if $(DOCKER), docker run --rm -i $(BUILD_CONTAINER)) go-junit-report > $(CIRCLE_TEST_REPORTS)/junit/alltests.xml

.PHONY: metalinter
metalinter: $(if $(DOCKER), dockerbuild)
	$(if $(DOCKER), docker run --rm $(BUILD_CONTAINER)) gometalinter \
		--vendor \
		--concurrency=6 \
		--cyclo-over=12 \
		--tests \
		--exclude='TLS InsecureSkipVerify may be true.' \
		--exclude='Use of unsafe calls should be audited' \
		--deadline=300s ./...

.PHONY: test
test: fmttest metalinter gocov junit gorace

.PHONY: version
version:
	@echo "$(VERSION)"

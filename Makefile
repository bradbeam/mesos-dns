GOOS:=linux
VERSION:=$(shell git describe --abbrev=4 --always --tags)

dockerbuild:
	docker build -t mesos-dns-test -f Dockerfile.test .

docker: buildstatic
	docker build -t mesos-dns:$(VERSION) -f Dockerfile.build .

# Ignore vendored dependencies
# Return 1 if any files found that haven't been formatted
fmttest: dockerbuild
	docker run --rm mesos-dns-test gofmt -l . | awk '!/^Godep/ { print $0; err=1 }; END{ exit err }'

test: dockerbuild
	docker run --rm mesos-dns-test godep go test -v ./...
	docker run --rm mesos-dns-test godep go test -v -short -race ./...

testall: fmttest test 

build: dockerbuild
	docker run --rm -v "$(PWD)/build":/build -e GOOS=$(GOOS) mesos-dns-test godep go build -ldflags="-X main.Version=$(VERSION)" -o /build/mesos-dns
	
buildstatic: dockerbuild
	docker run --rm -v "$(PWD)/build":/build -e CGO_ENABLED=0 -e GOOS=$(GOOS) mesos-dns-test godep go build -a -installsuffix cgo -ldflags "-s -X main.Version=$(VERSION)" -o /build/mesos-dns

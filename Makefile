GOOS="linux"

dockerbuild:
	docker build -t mesos-dns-test -f Dockerfile.test .

test: dockerbuild
	docker run -it --rm mesos-dns-test godep go test -v ./...

build: dockerbuild
	docker run -it --rm mesos-dns-test godep go build
	
buildstatic: dockerbuild
	docker run -it --rm -v "$(PWD)/build":/build -e CGO_ENABLED=0 -e  GOOS=$(GOOS) mesos-dns-test godep go build -a -installsuffix cgo -ldflags "-s" -o /build/mesos-dns

.PHONY: build docker run-cluster test

all: build

GO := $(shell command -v go 2> /dev/null)
# go source code files
GOSRCS=$(shell find . -name \*.go)
BUILDENV=CGO_ENABLED=1
BUILDTAGS=

ifeq ($(GO),)
  $(error could not find go. Is it in PATH? $(GO))
endif

ifneq ($(TARGET),)
  BUILDENV += GOOS=$(TARGET)
endif

PROFCMD=go test -cpuprofile=cpu.prof -tags "$(BUILDTAGS) cleveldb" -bench .

tags: $(GOSRCS)
	gotags -R -f tags .

build:
	@echo "--> Building amo daemon (amod)"
	$(BUILDENV) go build ./cmd/amod

# compatibility target
build_c:
	@echo "--> Building amo daemon (amod)"
	$(BUILDENV) go build -tags "$(BUILDTAGS) cleveldb" ./cmd/amod

install:
	@echo "--> Installing amo daemon (amod)"
	$(BUILDENV) go install ./cmd/amod

# compatibility target
install_c:
	@echo "--> Installing amo daemon (amod)"
	$(BUILDENV) go install -tags "$(BUILDTAGS) cleveldb" ./cmd/amod

test:
	go test ./...

test_c:
	go test -tags "$(BUILDTAGS) cleveldb" ./...

bench:
	cd amo; $(PROFCMD)
	cd amo/store; $(PROFCMD)

docker:
	COPYFILE_DISABLE=true tar zcf amoabci-docker.tar.gz Makefile go.mod go.sum cmd amo crypto Dockerfile DOCKER contrib
	docker build -t amolabs/amod - < amoabci-docker.tar.gz

clean:
	rm -f amod

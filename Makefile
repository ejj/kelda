export GO15VENDOREXPERIMENT=1
PACKAGES=$(shell govendor list -no-status +local)
NOVENDOR=$(shell find . -path -prune -o -path ./vendor -prune -o -name '*.go' -print)
LINE_LENGTH_EXCLUDE=./api/pb/pb.pb.go \
		    ./cluster/amazon/mock_client.go \
		    ./cluster/cloudcfg/template.go \
		    ./cluster/digitalocean/mock_client.go \
		    ./cluster/google/mock_client_test.go \
		    ./cluster/machine/amazon.go \
		    ./cluster/machine/google.go \
		    ./minion/network/link_test.go \
		    ./minion/ovsdb/mock_transact.go \
		    ./minion/ovsdb/mocks/Client.go \
		    ./minion/pb/pb.pb.go \
		    ./stitch/bindings.js.go

REPO = quilt
DOCKER = docker
SHELL := /bin/bash

all:
	cd -P . && go build .

install:
	cd -P . && go install .

check: format-check
	govendor test +local

clean:
	govendor clean -x +local
	rm -f *.cov.coverprofile cluster/*.cov.coverprofile minion/*.cov.coverprofile
	rm -f *.cov.html cluster/*.cov.html minion/*.cov.html

windows:
	cd -P . && GOOS=windows GOARCH=386 go build .

COV_SKIP= /api/client/mocks \
	  /api/pb \
	  /cluster/provider/mocks \
	  /constants \
	  /minion/pprofile \
	  /minion/ovsdb/mocks \
	  /minion/network/mocks \
	  /quiltctl/testutils \
	  /scripts \
	  /scripts/format \
	  /scripts/specs-tester \
	  /scripts/specs-tester/tests \
	  /minion/pb \
	  /version

COV_PKG = $(subst github.com/quilt/quilt,,$(PACKAGES))
coverage: $(addsuffix .cov, $(filter-out $(COV_SKIP), $(COV_PKG)))
	echo "" > coverage.txt
	for f in $^ ; do \
	    cat .$$f >> coverage.txt ; \
	done

%.cov:
	go test -coverprofile=.$@ .$*
	go tool cover -html=.$@ -o .$@.html

format: scripts/format/format
	gofmt -w -s $(NOVENDOR)
	scripts/format/format $(filter-out $(LINE_LENGTH_EXCLUDE),$(NOVENDOR))

scripts/format/format: scripts/format/format.go
	cd scripts/format && go build format.go

format-check:
	RESULT=`gofmt -s -l $(NOVENDOR)` && \
	if [[ -n "$$RESULT"  ]] ; then \
	    echo $$RESULT && \
	    exit 1 ; \
	fi

build-specs-tester: scripts/specs-tester/*
	cd scripts/specs-tester && go build .

check-specs: build-specs-tester
	scripts/specs-tester/specs-tester

lint: format
	cd -P . && govendor vet +local
	EXIT_CODE=0; \
	for package in $(PACKAGES) ; do \
		if [[ $$package != *minion/pb* && $$package != *api/pb* ]] ; then \
			golint -min_confidence .25 -set_exit_status $$package || EXIT_CODE=1; \
		fi \
	done ; \
	find . -path ./vendor -prune -o -name '*' -type f -print | xargs misspell -error || EXIT_CODE=1; \
	ineffassign . || EXIT_CODE=1; \
	exit $$EXIT_CODE

generate:
	govendor generate +local

providers:
	python3 scripts/gce-descriptions > cluster/machine/google.go

# This is what's strictly required for `make check lint` to run.
get-build-tools:
	go get -v -u \
	    github.com/client9/misspell/cmd/misspell \
	    github.com/golang/lint/golint \
	    github.com/gordonklaus/ineffassign \
	    github.com/kardianos/govendor

# This additionally contains the tools needed for `go generate` to work.
go-get: get-build-tools
	go get -v -u \
	    github.com/golang/protobuf/{proto,protoc-gen-go} \
	    github.com/vektra/mockery/.../

docker-build-quilt:
	cd -P . && git show --pretty=medium --no-patch > buildinfo \
		&& CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build . \
	    && ${DOCKER} build -t ${REPO}/quilt .

docker-push-quilt:
	${DOCKER} push ${REPO}/quilt

docker-build-ovs:
	cd -P ovs && docker build -t ${REPO}/ovs .

# Include all .mk files so you can have your own local configurations
include $(wildcard *.mk)

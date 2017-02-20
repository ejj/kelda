export GO15VENDOREXPERIMENT=1
PACKAGES=$(shell govendor list -no-status +local)
NOVENDOR=$(shell find . -path -prune -o -path ./vendor -prune -o -name '*.go' -print)
LINE_LENGTH_EXCLUDE=./api/pb/pb.pb.go \
		    ./cluster/amazon/mock_client.go \
		    ./cluster/cloudcfg/template.go \
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

COV_SKIP= /api/client/mocks \
	  /api/pb \
	  /cluster/provider/mocks \
	  /constants \
	  /minion/pprofile \
	  /minion/ovsdb/mocks \
	  /quilt-tester \
	  /quilt-tester/tests/10-network \
	  /quilt-tester/tests/15-bandwidth \
	  /quilt-tester/tests/20-spark \
	  /quilt-tester/tests/30-mean \
	  /quilt-tester/tests/40-stop \
	  /quilt-tester/tests/100-logs \
	  /quilt-tester/tests/75-network \
	  /quilt-tester/tests/pub-facing \
	  /quiltctl/testutils \
	  /scripts \
	  /minion/pb

COV_PKG = $(subst github.com/quilt/quilt,,$(PACKAGES))
coverage: $(addsuffix .cov, $(filter-out $(COV_SKIP), $(COV_PKG)))
	echo "" > coverage.txt
	for f in $^ ; do \
	    cat .$$f >> coverage.txt ; \
	done

%.cov:
	go test -coverprofile=.$@ .$*
	go tool cover -html=.$@ -o .$@.html

format: scripts/format
	gofmt -w -s $(NOVENDOR)
	scripts/format $(filter-out $(LINE_LENGTH_EXCLUDE),$(NOVENDOR))

scripts/format: scripts/format.go
	cd scripts && go build format.go

format-check:
	RESULT=`gofmt -s -l $(NOVENDOR)` && \
	if [[ -n "$$RESULT"  ]] ; then \
	    echo $$RESULT && \
	    exit 1 ; \
	fi

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

tests:
	cd -P quilt-tester && \
	for suite in tests/* ; do \
		for f in $$suite/* ; do \
			if [ $${f: -3} == ".go" ] ; then \
				CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o $${f%???} $$f ; \
			fi \
		done \
	done

docker-build-quilt:
	cd -P . && git show --pretty=medium --no-patch > buildinfo \
		&& CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build . \
	    && ${DOCKER} build -t ${REPO}/quilt .

docker-push-quilt:
	${DOCKER} push ${REPO}/quilt

docker-build-tester: tests
	cd -P quilt-tester \
	    && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
	       go build -o bin/quilt-tester . \
	    && ${DOCKER} build -t ${REPO}/tester .

docker-build-ovs:
	cd -P ovs && docker build -t ${REPO}/ovs .

# Include all .mk files so you can have your own local configurations
include $(wildcard *.mk)

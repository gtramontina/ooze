gotestsum-version = v1.9.0

.bin/gotestsum: makefile.gotestsum.mk
	@GOBIN="$(PWD)/$(dir $@)" go install gotest.tools/gotestsum@$(gotestsum-version)
pre-reqs += .bin/gotestsum

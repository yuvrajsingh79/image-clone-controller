EXE_CONTROLLER_NAME=image-clone-controller
IMAGE = backupregistry/${EXE_CONTROLLER_NAME}
VERSION := latest
ARCH=$(shell docker version -f {{.Client.Arch}})

.PHONY: buildcontrollerimage
buildcontrollerimage: build-systemutil
	docker build	\
	-t $(IMAGE):$(VERSION) -f Dockerfile .

.PHONY: build-systemutil
build-systemutil:
	docker build  --build-arg OS=linux --build-arg ARCH=$(ARCH) -t controller-builder --pull -f Dockerfile.builder .
	docker run controller-builder
	docker cp `docker ps -q -n=1`:/go/bin/${EXE_CONTROLLER_NAME} ./${EXE_CONTROLLER_NAME}
#!/bin/bash

set -ex

REGISTRY=localhost:5000
docker image pull busybox
docker image tag busybox:latest $REGISTRY/busybox:latest
docker image push $REGISTRY/busybox:latest

for I in `seq 7`; do
    dd if=/dev/urandom of=./nydus-test/image/file-$I bs=2M count=1
done

# ------ build oci and nydus
test_case_1 () {
    NYDUS_FS_VERSION=$1
    buildctl prune --all

    # build oci
    buildctl build --frontend dockerfile.v0 \
        --local context=./nydus-test/image \
        --local dockerfile=./nydus-test/image/image-1 \
        --output type=image,name=$REGISTRY/test:oci,push=true,compression=gzip,oci-mediatypes=true

    # build nydus with zstd compressor
    buildctl build --frontend dockerfile.v0 \
        --local context=./nydus-test/image \
        --local dockerfile=./nydus-test/image/image-1 \
        --output type=image,name=$REGISTRY/test:nydus,push=true,compression=nydus,nydus-fs-version=$NYDUS_FS_VERSION,nydus-compressor=zstd,oci-mediatypes=true,force-compression=true

    # check
    nydusify check --source $REGISTRY/test:oci --target $REGISTRY/test:nydus

    # build nydus with lz4_block compressor
    buildctl build --frontend dockerfile.v0 \
        --local context=./nydus-test/image \
        --local dockerfile=./nydus-test/image/image-1 \
        --output type=image,name=$REGISTRY/test:nydus,push=true,compression=nydus,nydus-fs-version=$NYDUS_FS_VERSION,nydus-compressor=lz4_block,oci-mediatypes=true,force-compression=true

    # check
    nydusify check --source $REGISTRY/test:oci --target $REGISTRY/test:nydus
}

# ------ build nydus and oci
test_case_2 () {
    NYDUS_FS_VERSION=$1
    buildctl prune --all

    # build nydus
    buildctl build --frontend dockerfile.v0 \
        --local context=./nydus-test/image \
        --local dockerfile=./nydus-test/image/image-1 \
        --output type=image,name=$REGISTRY/test:nydus,push=true,compression=nydus,nydus-fs-version=$NYDUS_FS_VERSION,oci-mediatypes=true,force-compression=true

    # build oci
    buildctl build --frontend dockerfile.v0 \
        --local context=./nydus-test/image \
        --local dockerfile=./nydus-test/image/image-1 \
        --output type=image,name=$REGISTRY/test:oci,push=true,compression=gzip,oci-mediatypes=true,force-compression=true

    # check
    nydusify check --source $REGISTRY/test:oci --target $REGISTRY/test:nydus
}

# ------ build nydus with local chunk dict
test_case_3 () {
    NYDUS_FS_VERSION=$1
    buildctl prune --all

    # build nydus chunk dict 1
    buildctl build --frontend dockerfile.v0 \
        --local context=./nydus-test/image \
        --local dockerfile=./nydus-test/image/chunk-dict-1 \
        --output type=image,name=$REGISTRY/test:nydus-chunk-dict-1,push=true,compression=nydus,nydus-fs-version=$NYDUS_FS_VERSION,force-compression=true

    # build with nydus chunk dict 1
    buildctl build --frontend dockerfile.v0 \
        --local context=./nydus-test/image \
        --local dockerfile=./nydus-test/image/image-1 \
        --output type=image,name=$REGISTRY/test:nydus,push=true,compression=nydus,nydus-fs-version=$NYDUS_FS_VERSION,nydus-chunk-dict-image=$REGISTRY/test:nydus-chunk-dict-1,force-compression=true

    # build with nydus chunk dict 1 (local cache) again
    buildctl build --frontend dockerfile.v0 \
        --local context=./nydus-test/image \
        --local dockerfile=./nydus-test/image/image-1 \
        --output type=image,name=$REGISTRY/test:nydus,push=true,compression=nydus,nydus-fs-version=$NYDUS_FS_VERSION,nydus-chunk-dict-image=$REGISTRY/test:nydus-chunk-dict-1,force-compression=true

    # check
    nydusify check --source $REGISTRY/test:oci --target $REGISTRY/test:nydus

    # build without nydus chunk dict 1
    buildctl build --frontend dockerfile.v0 \
        --local context=./nydus-test/image \
        --local dockerfile=./nydus-test/image/image-1 \
        --output type=image,name=$REGISTRY/test:nydus,push=true,compression=nydus,nydus-fs-version=$NYDUS_FS_VERSION,force-compression=true

    # check
    nydusify check --source $REGISTRY/test:oci --target $REGISTRY/test:nydus
}

# ------ build nydus with remote chunk dict
test_case_4 () {
    NYDUS_FS_VERSION=$1
    buildctl prune --all

    # build with nydus chunk dict 1
    buildctl build --frontend dockerfile.v0 \
        --local context=./nydus-test/image \
        --local dockerfile=./nydus-test/image/image-1 \
        --output type=image,name=$REGISTRY/test:nydus,push=true,compression=nydus,nydus-fs-version=$NYDUS_FS_VERSION,nydus-chunk-dict-image=$REGISTRY/test:nydus-chunk-dict-1,force-compression=true

    # check
    nydusify check --source $REGISTRY/test:oci --target $REGISTRY/test:nydus

    # build nydus chunk dict 2
    buildctl build --frontend dockerfile.v0 \
        --local context=./nydus-test/image \
        --local dockerfile=./nydus-test/image/chunk-dict-2 \
        --output type=image,name=$REGISTRY/test:nydus-chunk-dict-2,push=true,compression=nydus,nydus-fs-version=$NYDUS_FS_VERSION,force-compression=true

    # build with nydus chunk dict 2
    buildctl build --frontend dockerfile.v0 \
        --local context=./nydus-test/image \
        --local dockerfile=./nydus-test/image/image-1 \
        --output type=image,name=$REGISTRY/test:nydus,push=true,compression=nydus,force-compression=true,nydus-fs-version=$NYDUS_FS_VERSION,nydus-chunk-dict-image=$REGISTRY/test:nydus-chunk-dict-2

    # check
    nydusify check --source $REGISTRY/test:oci --target $REGISTRY/test:nydus

    # build without nydus chunk dict
    buildctl build --frontend dockerfile.v0 \
        --local context=./nydus-test/image \
        --local dockerfile=./nydus-test/image/image-1 \
        --output type=image,name=$REGISTRY/test:nydus,push=true,compression=nydus,nydus-fs-version=$NYDUS_FS_VERSION,force-compression=true

    # check
    nydusify check --source $REGISTRY/test:oci --target $REGISTRY/test:nydus
}

test () {
    NYDUS_FS_VERSION=$1

    test_case_1 $NYDUS_FS_VERSION
    test_case_2 $NYDUS_FS_VERSION
    test_case_3 $NYDUS_FS_VERSION
    test_case_4 $NYDUS_FS_VERSION
}

test 5
test 6

#!/bin/bash

set -ex

if [ $# -ne 2 ]; then
    echo "Usage: ./test_chunk_dick.sh image_list remote_repo"
    exit 1
fi

IMAGE_LIST=$1
REMOTE_REPO=$2
LOCAL_REPO=localhost:5000/library

test () {
    NYDUS_FS_VERSION=$1
    BASE_IMAGE="alpine"

    for I in $(cat $IMAGE_LIST); do
        LOCAL_DICT=$LOCAL_REPO/$BASE_IMAGE
        REMOTE_DICT=$REMOTE_REPO/$BASE_IMAGE
        SRC_IMAGE=$I
        DEST_IMAGE=$REMOTE_REPO/$I

        buildctl prune --all

        echo "FROM $BASE_IMAGE" > ./nydus-test/top_images/Dockerfile
        # build local dict
        buildctl build --frontend dockerfile.v0 \
            --local context=./nydus-test/image \
            --local dockerfile=./nydus-test/top_images/ \
            --output type=image,name=$LOCAL_DICT,push=true,compression=nydus,nydus-fs-version=$NYDUS_FS_VERSION,oci-mediatypes=true
        # build remote dict
        buildctl build --frontend dockerfile.v0 \
            --local context=./nydus-test/image \
            --local dockerfile=./nydus-test/top_images/ \
            --output type=image,name=$REMOTE_DICT,push=true,compression=nydus,nydus-fs-version=$NYDUS_FS_VERSION,oci-mediatypes=true

        echo "FROM $SRC_IMAGE" > ./nydus-test/top_images/Dockerfile
        # build with local and remote dict
        buildctl build --frontend dockerfile.v0 \
            --local context=./nydus-test/image \
            --local dockerfile=./nydus-test/top_images/ \
            --output type=image,name=$DEST_IMAGE,push=true,compression=nydus,nydus-fs-version=$NYDUS_FS_VERSION,nydus-chunk-dict-image=$LOCAL_DICT

        nydusify check --source $SRC_IMAGE --target $DEST_IMAGE
        BASE_IMAGE=$I
    done
}

test 5
test 6
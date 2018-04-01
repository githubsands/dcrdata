#!/usr/bin/env bash
set -ex

# The script does automatic checking on a Go package and its sub-packages,
# To run on docker on windows, symlink /mnt/c to /c and then execute the script
# from the repo path under /c.  See:
# https://github.com/Microsoft/BashOnWindows/issues/1854
# for more details.

REPO=dcrdata 
DOCKER_IMAGE_TAG=dcrdata

buildconfig () {

    cp sample-dcrdata.conf dcrdata.conf 

    # Update Files 
    ./dcrdata --debuglevel=debug \
        --dcrduser= \
        --dcrdpass= \
        --testnet=1 \
        --simnet=1  \
        --dcrdserve=localhost:9109
        --dcrdcert=/home/me/.dcrd/rpc.cert \
        --apilisten=127.0.0.1:777 \
        --apiproto=http \
        --indentjson= "   " \
        --userealip=true \
        --cachecontrol-maxage=86400 \
        --pg=false
        --pgdbname=dcrdata \
        --pguser=dcrdata \
        --pgpass= \
        --pghost=127.0.0.1:5432 \
        --pghost=/run/postgresql1 \
        ./...
        if [ $? != 0 ]; then
            echo 'errors'
            exit 1
        fi

        echo "dcrdata config updated to default settings"
}


if [ $GOVERSION == "local" ]; then
    buildconfig
    exit
fi

docker pull decred/$DOCKER_IMAGE_TAG
if [ $? != 0 ]; then
        echo 'docker pull failed'
        exit 1
fi

docker run --rm -it -v $(pwd):/src githubsands/$DOCKER_IMAGE_TAG /bin/bash -c "\
  rsync -ra --filter=':- .gitignore'  \
  /src/ /go/src/github.com/decred/$REPO/ && \
  cd github.com/decred/$REPO/ && \
  bash buildconfig.sh local"
if [ $? != 0 ]; then
        echo 'docker run failed'
        exit 1
fi



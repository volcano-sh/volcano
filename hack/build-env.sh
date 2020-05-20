#!/bin/bash

ENV=$1

GitSHA=`git rev-parse HEAD`

cp defs/Makefile.${ENV}.def Makefile.def

sed -i "s/__git_sha__/"${GitSHA}"/g" Makefile.def
sed -i "s/__release_ver__/"${RELEASE_VER}"/g" Makefile.def

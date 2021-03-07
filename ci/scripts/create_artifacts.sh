#!/bin/bash

mkdir -p /tmp/artifacts
cd /tmp/artifacts

kelda bug-tool

# Extract the tarballs so that the contents can be directly viewed in the Circle UI.
for f in *.tar.gz; do
    dir="${f%%.tar.gz}"
    mkdir ${dir}
    tar -C ${dir} -xvf $f
done

# Ignore errors.
exit 0

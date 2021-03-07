#!/bin/sh
# Upload the release to S3. Called by Circle.
set -e

# The name of the bucket where we store the releases. This bucket should match
# the one used by the downloader Lambda function.
s3_bucket="kelda-releases"

# The prep directory used to stage the archive that will be uploaded.
stage_dir=/tmp

# Create the release build.
KELDA_VERSION=${CIRCLE_TAG} make build-linux build-osx
cp kelda-osx kelda-linux ${stage_dir}
chmod +x ${stage_dir}/kelda-osx ${stage_dir}/kelda-linux

# Create the archives that'll be downloaded by the installer script and CLI.
osx_archive_name=kelda-osx-${CIRCLE_TAG}.tar.gz
linux_archive_name=kelda-linux-${CIRCLE_TAG}.tar.gz

mv ${stage_dir}/kelda-osx ${stage_dir}/kelda
tar -C ${stage_dir} -czf ${stage_dir}/${osx_archive_name} kelda
mv ${stage_dir}/kelda ${stage_dir}/kelda-osx

mv ${stage_dir}/kelda-linux ${stage_dir}/kelda
tar -C ${stage_dir} -czf ${stage_dir}/${linux_archive_name} kelda
mv ${stage_dir}/kelda ${stage_dir}/kelda-linux

aws s3 cp ${stage_dir}/${osx_archive_name} s3://${s3_bucket}/${osx_archive_name} --content-type application/x-gzip
aws s3 cp ${stage_dir}/${linux_archive_name} s3://${s3_bucket}/${linux_archive_name} --content-type application/x-gzip

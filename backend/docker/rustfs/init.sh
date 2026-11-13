#!/bin/sh
# Creates the dev bucket and applies its CORS rules. Safe to run repeatedly:
# the bucket is created only if missing and CORS is overwritten each time.
set -eu

aws_s3() { aws --endpoint-url "$S3_INTERNAL_ENDPOINT" s3api "$@"; }

echo "waiting for rustfs at $S3_INTERNAL_ENDPOINT"
i=0
until aws_s3 list-buckets >/dev/null 2>&1; do
  i=$((i + 1))
  if [ "$i" -ge 60 ]; then
    echo "rustfs not reachable after 60s" >&2
    exit 1
  fi
  sleep 1
done

if aws_s3 head-bucket --bucket "$S3_BUCKET" >/dev/null 2>&1; then
  echo "bucket $S3_BUCKET exists"
else
  aws_s3 create-bucket --bucket "$S3_BUCKET" >/dev/null
  echo "bucket $S3_BUCKET created"
fi

aws_s3 put-bucket-cors --bucket "$S3_BUCKET" --cors-configuration file:///init/cors.dev.json
echo "cors applied to $S3_BUCKET"

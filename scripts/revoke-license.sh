#!/bin/sh
set -e

export AWS_DEFAULT_REGION="us-west-2"

company="${1}"
if [ -z "${company}" ]; then
    echo "Company is required"
    exit 1
fi

user_name="${company}-analytics"
bucket_name="kelda-analytics-${company}"

echo "Deleting user's access key.."
access_key=$(aws iam list-access-keys \
    --user-name "${user_name}" \
    --query 'AccessKeyMetadata[*].AccessKeyId' \
    --output text)
aws iam delete-access-key \
    --user-name ${user_name} \
    --access-key-id ${access_key}

# To delete the policy, we have to first detach it from the user, then remove
# all of its old versions, and finally delete the top-level policy object.
echo "Deleting the policy.."
policy_arns=$(aws iam list-policies \
    --query "Policies[?PolicyName==\`${bucket_name}-upload\`].Arn" \
    --output text)
for policy_arn in ${policy_arns}; do
    aws iam detach-user-policy \
        --user-name "${user_name}" \
        --policy-arn "${policy_arn}"

    policy_versions=$(aws iam list-policy-versions \
        --policy-arn "${policy_arn}" \
        --query 'Versions[?IsDefaultVersion==`false`].VersionId' \
        --output text)
    for policy_version in ${policy_versions}; do
        aws iam delete-policy-version \
            --version-id "${policy_version}" \
            --policy-arn "${policy_arn}"
    done
done

aws iam delete-policy --policy-arn "${policy_arn}"

# Delete the user.
echo "Deleting the user.."
aws iam delete-user --user-name "${user_name}"

echo "Done!"
echo
echo "Not deleting the bucket. It can be deleted by running the following command:"
echo "aws --region us-west-2 s3 rb --force s3://${bucket_name}"

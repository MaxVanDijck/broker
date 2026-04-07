---
title: AWS IAM Permissions
weight: 3
---

## Minimum IAM policy

The broker server needs AWS credentials with the following permissions to manage EC2 instances. This is the minimum required policy -- no more, no less.

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "EC2InstanceManagement",
      "Effect": "Allow",
      "Action": [
        "ec2:RunInstances",
        "ec2:TerminateInstances",
        "ec2:StopInstances",
        "ec2:StartInstances",
        "ec2:DescribeInstances",
        "ec2:DescribeInstanceStatus"
      ],
      "Resource": "*"
    },
    {
      "Sid": "EC2SecurityGroups",
      "Effect": "Allow",
      "Action": [
        "ec2:CreateSecurityGroup",
        "ec2:DeleteSecurityGroup",
        "ec2:DescribeSecurityGroups",
        "ec2:CreateTags"
      ],
      "Resource": "*"
    },
    {
      "Sid": "AMIResolution",
      "Effect": "Allow",
      "Action": [
        "ssm:GetParameter"
      ],
      "Resource": [
        "arn:aws:ssm:*:*:parameter/aws/service/deeplearning/*",
        "arn:aws:ssm:*:*:parameter/aws/service/ami-amazon-linux-latest/*"
      ]
    }
  ]
}
```

## Credential configuration

The server uses the standard AWS SDK credential chain. In order of precedence:

1. **Environment variables** -- `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_SESSION_TOKEN`
2. **Shared credentials file** -- `~/.aws/credentials`
3. **IAM instance role** -- when running on EC2
4. **ECS task role** -- when running in ECS/Fargate
5. **IRSA** -- when running in EKS with IAM Roles for Service Accounts

For production, use an IAM role (instance role, task role, or IRSA) rather than long-lived access keys.

## Resource tagging

All EC2 instances and security groups created by broker are tagged with:

| Tag | Value |
|---|---|
| `broker-cluster` | Cluster name |

These tags are used to identify and manage broker-created resources. You can scope the IAM policy to only allow operations on resources with this tag using a condition:

```json
{
  "Condition": {
    "StringEquals": {
      "ec2:ResourceTag/broker-cluster": "*"
    }
  }
}
```

## Multi-region support

Broker automatically fails over across regions when capacity is unavailable. The IAM policy above uses `Resource: "*"` to allow operations in any region. If you want to restrict to specific regions, replace `"*"` with region-specific ARNs.

Default failover regions: `us-east-1`, `us-west-2`, `us-east-2`, `eu-west-1`, `ap-southeast-1`.

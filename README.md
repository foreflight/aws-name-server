A DNS server that serves up your ec2 instances via the AWS SDK.

Usage
=====

```
aws-name-server --domain aws.example.com --aws-region us-east-1
```

This will serve up DNS records for the following:

* `<name>.aws.example.com` all your EC2 instances tagged with Name=&lt;name>
* `<n>.<name>.aws.example.com` the nth instances tagged with Name=&lt;name>
* `<role>.role.aws.example.com` all your EC2 instances tagged with Role=&lt;role>
* `<n>.<role>.role.aws.example.com` the nth instances tagged with Role=&lt;role>
* `<instance-id>.aws.example.com` all your EC2 instances by instance id.
* `<n>.<instance-id>.aws.example.com` all your EC2 instances by instance id.

Currently, it always resolves the internal addresses.

Quick start
===========

There's a long-winded [Setup guide](#setup), but if you already know your way around EC2 and DNS, you'll need to:

1. Open up port 53 (UDP and TCP) on your security group.
2. Boot an instance with an IAM Role with `ec2:DescribeInstances` permission. (or use an IAM user and
   configure `aws-name-server` manually).
3. Install `aws-name-server`.
4. Setup your NS records correctly.

Parameters
==========

### `--domain`

This is the domain you wish to serve. i.e. `aws.example.com`. It is the
only required parameter.

### `--hostname`

The publicly resolvable hostname of the current machine. This defaults
sensibly, so you only need to set this if you see a warning in the logs.


### `--configFile`

A case sensitive json configuration file containing any sub accounts:

    [
      {
        "NickName": "prod",
        "ARN": "arn:aws:iam::123456789012:role/AWSNameServer",
        "Region": "us-east-1"
      },
      {
        "NickName": "preprod",
        "ARN": "arn:aws:iam::123456789012:role/AWSNameServer",
        "Region": "us-east-1"
      },
      {
        "NickName": "apollo",
        "ARN": "arn:aws:iam::123456789012:role/AWSNameServer",
        "Region": "us-east-1"
      },
      {
        "NickName": "skylab",
        "ARN": "arn:aws:iam::123456789012:role/AWSNameServer",
        "Region": "us-east-1"
      }
    ]
# Cloud Provider Configuration

This section describes the basic configuration of the cloud providers supported
by Kelda, and gives some details about how to enable extra features (e.g.,
floating IP addresses) on each cloud provider.

Kelda needs access to your cloud provider credentials in order to make the API
calls needed to boot your deployment. Don't worry, Kelda will never store
your credentials or use them for anything else than deploying your application.

## Amazon EC2

### Set Up Credentials
1. If you don't already have an account with
   [Amazon Web Services](https://aws.amazon.com/ec2/), go ahead and create one.

2. Get your access credentials from the [Security Credentials](https://console.aws.amazon.com/iam/home?#security_credential)
   page in the AWS Management Console. Choose "Access Keys" and then
   "Create New Access Key."

3. Run `kelda init` on the machine that will be running the daemon, and pass
  it your AWS credentials. The formatted credentials will be placed in
  `~/.aws/credentials`.

### Formatting Credentials
While it is recommended to use `kelda init` to format the provider credentials,
it is possible to manually create the credentials file in `~/.aws/credentials`
on the machine that will be running the daemon:

```conf
[default]
aws_access_key_id = <YOUR_ID>
aws_secret_access_key = <YOUR_SECRET_KEY>
```

The file needs to appear exactly as above (including the `[default]` at the
top), except with `<YOUR_ID>` and `<YOUR_SECRET_KEY>` filled in appropriately.

## DigitalOcean

### Set Up Credentials
1. If you don't have a DigitalOcean account, go ahead and
   [create one](https://www.digitalocean.com/).

2. Create a new token [here](https://cloud.digitalocean.com/settings/api/tokens).
   The token must have both read and write permissions.

3. Run `kelda init` on the machine that will be running the Kelda daemon, and
   pass it your token. The token will be placed in `~/.digitalocean/key`.

### Floating IPs
Unless there are already droplets running, DigitalOcean doesn't allow users to
create floating IPs under the "Networking" tab on their website. Instead, [this
link](https://cloud.digitalocean.com/networking/floating_ips/datacenter) can be
used to reserve IPs that Kelda can then assign to droplets.

## Google Compute Engine

### Set Up Credentials
1. If you don't have an account on Google Cloud Platform, go ahead and
   [create one](https://cloud.google.com/compute/).

2. Create a Google Cloud Platform Project: All instances are booted under a
   Cloud Platform project. To setup a project for use with Kelda, go to the
   [console page](http://console.cloud.google.com), then click the project
   dropdown at the top of page, and hit the plus icon. Pick a name, and create
   your project.

3. Enable the Compute API: Select your newly created project from the project
   selector at the top of the [console page](http://console.cloud.google.com),
   and then select `APIs & services -> Library` from the navbar on the left. Search
   for and enable the `Google Compute Engine API`.

4. Save the Credentials File: Go to `Credentials` on the left navbar (under `APIs
   & services`), and create credentials for a `Service account key`. Create a new
   service account with the `Project -> Editor` role, and select the JSON output
   option.

5. Run `kelda init` on the machine from which you will be running the Kelda
  daemon, and give it the path to the downloaded JSON from step 3.
  The credentials will be placed in `~/.gce/kelda.json`.

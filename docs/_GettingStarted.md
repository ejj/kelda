# Getting Started

## Installing Kelda
Kelda relies on Node.js version 6 or higher.  Start by downloading and
installing Node.js with the installer from the
[Node.js download page](https://nodejs.org/en/download/).

Next, use Node.js's package manager, `npm`, to install Kelda:

```console
$ npm install -g @kelda/install
```

You might get an error saying `Please try running this command again as
root/Administrator`. If so, use the `--unsafe-perm` flag, and install as root:

```console
$ sudo npm install -g @kelda/install --unsafe-perm
```

If Kelda was successfully installed, the `kelda` command should output a blurb
about how to use the command.

## Configuring a Cloud Provider
This section explains how to get credentials for your cloud provider. Kelda will
need these to launch machines on your account. Before proceeding:

1. Make sure you have an account with one of Kelda's supported cloud providers
  ([Google Cloud](https://cloud.google.com/),
  [Amazon EC2](https://aws.amazon.com/ec2/), or
  [DigitalOcean](https://www.digitalocean.com/))

2. Locate your credentials for that account.

For Amazon EC2, create an account with [Amazon Web Services](https://aws.amazon.com/ec2/),
find your "Access Keys" on the
[Security Credentials](https://console.aws.amazon.com/iam/home?#security_credential)
page in the AWS Management Console, and "Create New Access Key" (or use an
existing key, if you already have one).

Alternatively, follow the instructions for [Google Cloud](http://docs.kelda.io/#google-compute-engine)
or [DigitalOcean](http://docs.kelda.io/#digitalocean), but **come back to this
tutorial before running `kelda init`**. In the next step, you will run
`kelda init` with some specific inputs.

## Creating an Infrastructure
To run any applications with Kelda, you must specify the infrastructure Kelda
should launch your application on. The easiest way to do this is to run

```console
$ kelda init
```

The `kelda init` command will ask a number of questions, and then set up the
provider and infrastructure based on the given answers.

When answering the `kelda init` questions, keep these things in mind:

* **Name**: For the sake of this tutorial, make sure to use the name
  **`default`**. In the future, you can rerun `kelda init` and create other
  infrastructures with different names.
* **Credentials**: Use the provider credentials from the previous section.
* **Size**: When choosing a VM instance size, keep in mind that some
  providers have a free tier for certain instance types.
* **SSH key**: Make sure to add an SSH key -- we'll need it in [the debugging
  section](#debugging-applications-with-kelda) later. It doesn't matter whether
  you use an existing key or let Kelda generate one.
* **Worker/Master**: For this tutorial, 1 worker and 1 master is enough.

<aside class="notice">If you are unsure about how to answer any of the
questions, the default values are appropriate for this tutorial.
</aside>

For more information about `kelda init`, see [the documentation](#init).

It is also possible to use the
[Kelda blueprint API](#kelda.js-api-documentation) to specify
[`Machine`s](#Machine) directly in blueprints, but that's a topic for another
time.

## Getting the Blueprint
This section will walk you through how to run Nginx (an open-source web server)
using Kelda. In the example, we'll use Nginx to serve a simple webpage. Start by
downloading the blueprint using git:

```console
$ git clone https://github.com/kelda/nginx.git
```

The blueprint in `nginxExample.js` imports the `nginx.js` Nginx blueprint, and then
deploys the Nginx app to the base infrastructure you created with `kelda init`.

Before running anything, you'll need to download the JavaScript dependencies of
the blueprint. Navigate to the `nginx` folder, and use `npm`, the Node.js
package manager, to install all dependencies:

```console
$ cd nginx
$ npm install .
```

## Running the Application
To run a blueprint, you first need to have a Kelda daemon running.  If you
haven't already started one, open a new terminal window and start it:

```console
$ kelda daemon
```

The daemon is a long running process that periodically prints some log messages.
Leave this running. In another terminal window, navigate to the `nginx`
directory and run the blueprint:

```console
$ kelda run ./nginxExample.js
```

This command tells the daemon to launch the containers described in
`nginxExample.js` on your `default` base infrastructure.  It will return
immediately, because the `daemon` process does the heavy lifting.

It takes a few minutes to boot and configure the VMs, and for the application to
get up and running. To see how things are progressing, use Kelda's `show`
command:

```console
$ kelda show
MACHINE         ROLE      PROVIDER    REGION       SIZE     PUBLIC IP    STATUS
e5b1839d2bea    Master    Amazon      us-west-1    t2.micro              disconnected
e2401c348c78    Worker    Amazon      us-west-1    t2.micro              disconnected
```

Your output will look similar to the output above (note that you may get an
error that begins with "unable to query connections: rpc error" when you first
run `kelda show`; this error is benign and can occur while the machines are
booting).

The output above means that Kelda has launched two machines, one as a master and
one as a worker, in Amazon.  Both machines are `disconnected`, because they're
still being initialized. When a machine is fully booted and configured, it will
be marked as `connected` in the `STATUS` column.

Wait a few minutes.

## Accessing the Web App
When the VMs and containers are up and running, the output of `show` will look
something like this:

```console
$ kelda show
MACHINE         ROLE      PROVIDER    REGION       SIZE        PUBLIC IP         STATUS
e5b1839d2bea    Master    Amazon      us-west-1    t2.micro    54.183.98.15      connected
e2401c348c78    Worker    Amazon      us-west-1    t2.micro    54.241.251.192    connected

CONTAINER       MACHINE         COMMAND        HOSTNAME           STATUS     CREATED               PUBLIC IP
bd681b3d3af7    e2401c348c78    nginx:1.13     web_tier           running    About a minute ago    54.241.251.192:80
```

The bottom row lists the container that's running `nginx` -- that is, serving
our website. The website is ready when the `nginx` container's `STATUS` is
`running`. Don't worry if the `STATUS` is empty for a a few minutes -- this is
normal while things are starting up.

To access the website, simply copy-paste the `PUBLIC IP` address from the
`nginx` row into your browser. A site with "Hello, world!" text should appear.

This is all it takes to run an application on Kelda. The remainder of this
tutorial will cover some of the things you might want to do after your
application is up and running -- e.g. debugging or changing the website content,
and importantly, how to shut down the deployment.

<aside class="notice">Make sure to stop your deployment! You can skip the next
few sections, but make sure to read the section on
<a href="http://docs.kelda.io/#stopping-the-application">how to stop your
application</a> to avoid getting charged for any VMs that are left running.
</aside>

## Debugging Applications with Kelda
### SSH
Once the containers are running, you might need to log in to change something
or debug an issue.  The `kelda ssh` command makes this easy.  Use the container
ID from the `kelda show` output as the argument to `kelda ssh` to log in to that
container. For instance, to ssh into a container or VM whose ID starts with
`bd68`:

```console
$ kelda ssh bd68
```

Note that you don't need to type the whole ID; as long as Kelda gets a unique
prefix of the ID, it will log in to the correct machine.

Try SSHing into the `nginx` container, and run `ls` to see the contents of the
container's working directory. When you're done poking around, log out with the
`exit` command.

### Application Logs
Often, logs are helpful for debugging an application. To check the logs of the
same container or VM as above, use `kelda logs`:

```console
$ kelda logs bd68
```

Try running `kelda logs` on the Nginx container. You'll see that Nginx logs a
GET request each time you access the website. This is not thrilling information,
but the logs will come in handy if you ever encounter any errors.

## Changing the Website Content
You may later decide to change the contents of the simple
website.  You could do this by SSHing into the container, but for the sake of
example, let's do it using Kelda.  On your laptop, open the `nginx/index.html`
that was downloaded from github and change the contents of the page (e.g., you
can change the "Hello, World!" message).  Now, with the daemon still running,
re-deploy the webpage with Kelda:

```console
$ kelda run ./nginxExample.js
```

Kelda automatically detects the changes to the deployment, and will update it
to implement your changes.  Note that we didn't need to tell Kelda to
stop the Nginx container and start a new one; we just updated the view of what
the deployment should look like (in this case, by changing `index.html`), and
Kelda automatically detects this and updates the cluster accordingly.  Kelda
will prompt you to accept the changes that you're making to your deployment;
type `y`.

Run `kelda show` again and notice that Kelda has stopped the old
container and is starting a new one.  When the new container is `running`,
navigate to the new IP address and check that the modified page is up.

## Stopping the Application
When you're done experimenting with Kelda, __make sure to stop the machines
you've started!__  Otherwise, your cloud provider might charge you for the
VMs that are still running.  Kelda's `stop` command will stop all VMs and
containers:

```console
$ kelda stop
```

At this point, you can safely kill the Kelda daemon; go to the terminal window
that's running `kelda daemon` and stop the process with `Ctrl+C`, or just close
the window.

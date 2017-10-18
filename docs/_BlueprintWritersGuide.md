# Blueprint Writers Guide

The previous section described how to use Kelda to run an application that
already had a blueprint. This guide describes how to write the Kelda
blueprint for a new application, using the lobste.rs application as an example.
lobste.rs is an open source project that implements a reddit-like web page,
where users can post content and vote up or down other content.

## Decomposing the application into containers

The first question you should ask yourself is "how should this application be
decomposed into different containers?"  Be sure you've read the [How Kelda
Works](#how-kelda-works) section, which gives a brief overview of containers.
If you've already figured out the containers that are needed for your
application (e.g., if you're already using Docker), you can skip the rest of
this section.

### Specifying the containers for your application

As an example of how to specify the containers for your application, let's use
the lobste.rs example.  lobste.rs requires mysql to run, so we'll use one
container for mysql.  We'll use a second container for the lobste.rs program to
run in.

For each container that your application uses, you'll need a container image.
The container image describes the filesystem that will be on the container when
it's started.  For mysql, for exampe, the container image includes all of the
dependencies that mysql needs to run, so that after starting a new mysql
container, you can simply launch mysql (no more installation is needed).  Most
popular applications already have containers that you can use, and a quick
google search yields an existing [mysql image](https://hub.docker.com/_/mysql/)
that we can use for lobste.rs.

For the container that runs lobste.rs, we'll need to create a new image by
writing our own Dockerfile, which describes how the Docker image should be
created.  In this case, the Dockerfile is relatively simple:

```docker
# This container is based on the Ruby image, which means that it
# automatically inherits the Ruby installation defined in that image.
FROM ruby:2.3.1

# Install NodeJS, which is required by lobste.rs.
RUN apt-get update && apt-get install nodejs -y

# Download and build the lobste.rs code.
RUN git clone git://github.com/jcs/lobsters.git
WORKDIR lobsters
RUN bundle

# Add a file to the container that contains startup code for lobste.rs. This
# command assumes that start-lobsters.sh is in the same directory as this
# Dockerfile.
COPY start-lobsters.sh /lobsters/

# When the container starts, it should run the lobste.rs server using the
# start-lobsters.sh bash file that we copied above.  This is a common
# "gotcha" to people new to containers: unlike VMs, each container is based
# on a process (in this case, rails, which is started at the end of
# start-lobsters.sh) and will be shutdown when that process stops.
ENTRYPOINT ["/bin/sh", "/lobsters/start-lobsters.sh"]
```

In this case, we wrote an additional bash script, [`start-lobsters.sh`](), to
help start the application.  The important thing about that script is that it
does some setup that needed to be done after the container was started, so it
couldn't be done in the Dockerfile.  For example, the first piece of setup it
does it to initialize the SQL database.  Because that requires a connection to
mysql, it needs to be done after the container is launched (and configured to
access the mysql container, as discussed below).  After initializing the
database, the `start-lobsters.sh` script launches the rails server, which is the
main process run by the container.

To create a docker image using this file, run `docker build` in the directory
with the Dockerfile (don't forget the period at the end!):

```console
$ docker build -t kayousterhout/lobsters .
```
    
In this case, we called the resulting image `kayousterhout/lobsters`, because
we'll push it to the Dockerhub for kayousterhout; you'll want to use your own
Dockerhub id to name your images.

This will take a few minutes, and creates a new image with the name
`kayousterhout/lobsters`.  If you want to play around with the new container,
you can use Docker to launch it locally:

```console
$ docker run -n lobsters-test kayousterhout/lobsters
```
    
To use a shell on your new container to poke around (while the `rails server` is
running), use:

```console
$ docker exec -it lobsters-test /bin/bash
```
    
This can be helpful for making sure everything was installed and is running as
expected (although in this case, lobste.rs won't work when you start it with
Docker, because it's not yet connected to a mysql container).

## Deploying the containers with Kelda

So far we have a mysql container image (we're using an existing one hosted on
Dockerhub) and a lobste.rs container image that we just made.  You should
similarly have the containers ready for your application.  Up until now, we
haven't done anything Kelda-specific: if you were using another container
management service like Kubernetes, you would have had to create the container
images like we did above.  These containers aren't yet configured to communicate
with each other, which is what we'll set up with Kelda.  We'll also use Kelda to
descrbie the machines to launch for the containers to run on.

To run the containers for your application with Kelda, you'll need to write a
Kelda blueprint.  Kelda blueprints are written in Javascript, and the Kelda
Javascript API
is described [here](http://docs.kelda.io/#kelda-js-api-documentation).  In this
guide, we'll walk through how to write a Kelda blueprint for lobste.rs, but the
Kelda API has more functionality than we could describe here.  See the [API
guide](http://docs.kelda.io/#kelda-js-api-documentation) for more usage
information.

### Writing the Kelda blueprint for MySQL

First, let's write the Kelda blueprint to get the MySQL container up and running.  We
need to create a container based on the mysql image:

```javascript
let sql = new Container('sql', 'mysql:5.6.32');
```
    
Here, the argument to `Container` is the hostname for the container, and the
name of an image.  You can also pass in a Dockerfile to use to create a new
image, as described in the [Javascript API
documentation](http://docs.kelda.io/#kelda-js-api-documentation).

Next, the SQL container requires some environment variables to be set.  In
particular, we need to specify a root password for SQL.  We can set the root
password to `foo` with the `setEnv` function:

```javascript
sql.setEnv('MYSQL_ROOT_PASSWORD', 'foo');
```

### Writing the Kelda blueprint for lobste.rs

Next, we can similarly initialize the lobsters container.  The lobsters container is
a little trickier to initialize because it requires an environment variable
(`DATABASE_URL`) to be set to the URL of the SQL container.  Kelda containers
are each assigned unique hostnames when they're initialized, so we can create
the lobsters container and initialize the URL as follows:

```javascript
let lobsters = new Container('kayousterhout/lobsters');
const sqlDatabaseUrl = 'mysql2://root:' + mysqlOpts.rootPassword + '@' + sqlContainer.getHostname() + ':3306/lobsters';
lobsters.setEnv('DATABASE_URL', sqlDatabaseUrl);
```

### Allowing network connections
    
At this point, we've written code to create a mysql container and a lobsters
container.  With Kelda, by default, all network connections are blocked.  To allow
lobsters to talk to mysql, we need to explicitly open the mysql port (3306):

```javascript
sql.allowFrom(lobsters, 3306);
```
    
Because lobsters is a web application, the relevant port should also be open to
the public internet on the lobsters container.  Kelda has a `publicInternet`
variable that can be used to connect containers to any IP address:

```javascript
lobsters.allowFrom(publicInternet, 3000);
```
    
### Deploying the application on infrastructure

Finally, we'll use Kelda to launch some machines, and then start our containers on
those machines.  First, we'll define a "base machine."  We'll deploy a few
machines, and creating the base machine is a useful way to create one machine
that all of the machines in our deployment will be based off of.  In this case,
the base machine will be an Amazon instance that allows ssh access from the
public key "bar":

```javascript
let baseMachine = new Machine({provider: 'Amazon', sshKeys: ['ssh-rsa bar']});
```
    
Now, using that base machine, we can deploy a master and a worker machine using
Kelda's `Infrastructure` constructor.  All infrastructures must have at least
one master, which keeps track of state for all of the machines in the cluster,
and at least one worker. The `Infrastructure` constructor accepts the master(s)
and worker(s) as parameters:

```javascript
const infra = new Infrastructure(baseMachine, baseMachine);
```

We've now defined a infrastructure with a master and worker machine.  Let's
finally deploy the two containers on that infrastructure:

```javascript
sql.deploy(infra);
lobsters.deploy(infra);
```
    
We're done!  Running the blueprint is now trivial.  With a kelda daemon running, run
your new blueprint (which, in this case, is called lobsters.js):

```console
$ kelda run ./lobsters.js
```
    
Now users of lobsters, for example, can deploy it without needing to worry about
the details of how different containers are connected with each other.  All they
need to do is to `kelda run` the existing blueprint.

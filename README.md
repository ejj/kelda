[![Build Status](https://travis-ci.org/NetSys/quilt.svg?branch=master)](https://travis-ci.org/NetSys/quilt)
[![Go Report Card](https://goreportcard.com/badge/github.com/NetSys/quilt)](https://goreportcard.com/report/github.com/NetSys/quilt)
[![Coverage Status](https://coveralls.io/repos/github/NetSys/quilt/badge.svg?branch=master)](https://coveralls.io/github/NetSys/quilt?branch=master)

# Quilt

Quilt aims to be the easiest way to deploy and network containers.

Traditional container orchestrators have a procedural API focused narrowly on
compute.  The network, usually an afterthought, must be managed by a separate
system with its own independent API.  This leaves operators with a complex
task: write a deployment script that configures everything necessary to get
their application up and running.

Quilt takes a different approach.  It relies on a new domain specific language,
Stitch, to _specify_ distributed applications, independent of the specific
infrastructure they run on.  Given a stitch, Quilt can automatically deploy in
a variety of environments: Amazon EC2, and Google Compute
Engine, with more coming soon.  Furthermore it can do this with **no setup** --
just point Quilt at a stitch and it will take care of the rest: booting virtual
machines, starting containers on those VMs, and ensuring they can communicate.

Quilt is currently in alpha and under heavy development. Please try it out!
We are eager for feedback!

You can find setup instructions and a quick tutorial at
[GettingStarted.md](docs/GettingStarted.md).

## Stitch

Stitch is a domain specific language based on Scheme and has many of the tools
one would expect: modules, functions, variables, arithmetic, etc.  In addition,
it has some primitives for describing an application -- the collection of
containers that should be running, and precisely which are _allowed_ to
communicate.

## A Simple Stitch

To declare 3 docker containers with the latest Ubuntu image and a postgres
database, one would use the following stitch:

```javascript
    var containers = new Service("containers", new Container("ubuntu").replicate(3));
    var database = new Service("database", [new Container("postgres")]);

    deployment.deploy([containers, database]);
```

This will produce a simple network:

<img src="./docs/images/quiltSimple.png">

Next, suppose we'd like to expand with a batch processing system and 5 Apache
containers. By default containers can't communicate, so we will have to add
some network connections.

```javascript
    // Create 5 Apache containers, and label them "webTier"
    var webTier = new Service("webTier", new Container("httpd").replicate(5));

    // Create 2 Spark containers, and label them "batch"
    var batch = new Service("batch", new Container("spark").replicate(2));

    // Create a Postgres container, and label it "database"
    var database = new Service("database", [new Container("postgres")]);

    // Allow the public internet to connect to the webTier over port 80
    publicInternet.connect(80, webTier);

    // Allow the webTier to connect to the database on port 1433
    webTier.connect(1433, database);

    // Allow the batch processor to connect to the database and webTier via SSH
    batch.connect(22, webTier)
    batch.connect(22, database);

    // Allow all containers in the webTier to connect to each other on any port
    webTier.connect(new PortRange(0, 65535), webTier);

    // Deploy our containers.
    deployment.deploy([webTier, batch, database]);
```

After the above commands, our application looks a lot more interesting:

<img src="./docs/images/quiltAbstractWebTierConnect.png">

With this stitch, Quilt can now boot the system. Furthermore, if the stitch is
modified, Quilt will automatically adapt by adding or removing containers as
necessary. You can learn the specifics of the Stitch language by reading
[Stitch.md](docs/Stitch.md).

## Contributing
If you are interested in contributing to Quilt, check out
[Contributing.md](Contributing.md) and [Dev.md](docs/Dev.md) for development
instructions, details about the code structure, and more.

## Contact
Quilt is led by [Ethan J. Jackson](http://ejj.github.io/), a PhD student at
the UC Berkeley NetSys lab.  Feel free to direct questions, comments, and
inquiries to his email at
[ejj@eecs.berkeley.edu](mailto:ejj@eecs.berkeley.edu). Additionally, numerous
other contact methods may be found [here](http://ejj.github.io/contact.html).

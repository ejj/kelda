[![Build Status](https://travis-ci.org/quilt/quilt.svg?branch=master)](https://travis-ci.org/quilt/quilt)
[![Go Report Card](https://goreportcard.com/badge/github.com/quilt/quilt)](https://goreportcard.com/report/github.com/quilt/quilt)
[![Code Coverage](https://codecov.io/gh/quilt/quilt/branch/master/graph/badge.svg)](https://codecov.io/gh/quilt/quilt)

# Quilt

<img src="https://github.com/quilt/mean/blob/master/images/mean.gif">

Quilt is a simple way to use JavaScript to build and manage anything from
website backends to complex distributed systems. As shown above, a few simple
commands will get your system up and running.

Building infrastructure and running applications with Quilt is simple,
intuitive, and flexible. With Quilt.js, you specify your infrastructure
declaratively in JavaScript, and Quilt then takes care of deploying it on one
or more cloud providers. Subsequently scaling and modifying the infrastructure
then becomes a matter of simply changing a few lines of JavaScript code.

The Quilt.js JavaScript framework allows for development, versioning, and
testing of infrastructure in the same way we do for application code.
Additionally, Quilt.js code is shareable, reusable and composable, making it
easy to set up and manage systems without being an expert in system
administration.

Quilt is a research project out of UC Berkeley. It is currently under heavy
development, but please try it out - we are eager for feedback!

## Example: Deploying a MEAN Stack App with Quilt
The MEAN stack (MongoDB, Express, AngularJS, and Node.js) is a popular
fullstack JavaScript framework used for web development. Deploying a flexible,
multi-node MEAN stack app can be both time consuming and costly, but Quilt
simplifies this process.

With Quilt, it takes fewer than 20 lines of JavaScript code to set up a
replicated Node.js application, connect it to MongoDB, and hook it up with a
web proxy:

[//]: # (b1)
```javascript
    var Node = require("github.com/quilt/nodejs");
    var HaProxy = require("github.com/quilt/haproxy").Haproxy;
    var Mongo = require("github.com/quilt/mongo");

    // Create 3 replicated instances of each service.
    var mongo = new Mongo(3);
    // `app` is a Node.js application using Express, AngluarJS, and MongoDB.
    var app = new Node({
      nWorker: 3,
      repo: "https://github.com/tejasmanohar/node-todo.git",
      env: {
        PORT: "80",
        MONGO_URI: mongo.uri("mean-example")
      }
    });
    var haproxy = new HaProxy(3, app.services());

    // Connect the app and database.
    mongo.connect(27017, app);
    app.connect(27017, mongo);
    // Make the proxy accessible from the public internet on port 80.
    haproxy.public();
```

The application is infrastructure agnostic, so it can be deployed on any - and
possibly many - of the Quilt supported cloud providers. Here, we specify a
possible multi-node setup on AWS:

[//]: # (b1)
```javascript
    var namespace = createDeployment({});

    // An AWS VM with 1-2 CPUs and 1-2 GiB RAM.
    // The Github user `ejj` can ssh into the VMs.
    var baseMachine = new Machine({
        provider: "Amazon",
        cpu: new Range(2),
        ram: new Range(8),
        sshKeys: githubKeys("ejj"),
    });

    // Boot VMs with the properties of `baseMachine`.
    namespace.deploy(baseMachine.asMaster());
    namespace.deploy(baseMachine.asWorker().replicate(3));
```
All that is left is to deploy the application on the specified infrastructure:

[//]: # (b1)
```javascript
    namespace.deploy(app);
    namespace.deploy(mongo);
    namespace.deploy(haproxy);
```

This spec can be found in
[`github.com/quilt/mean/example.js`](https://github.com/quilt/mean/blob/master/example.js)
and used to deploy your app. Check out [this
guide](https://github.com/quilt/mean/blob/master/README.md)
for step by step instructions on how to deploy your own application using
Quilt.

As shown in the very beginning, deploying a MEAN app with Quilt is now as simple
as running the command `quilt run github.com/quilt/mean/example.js`.

## Features
Quilt offers a lot of great features. These are some of them:

* Build infrastructure in JavaScript
* Simple deployment and management of applications
* Easy cross-cloud deployment
* Low cost
* Shareable and composable infrastructure code
* Intuitive networking
* Flexible and scalable infrastructure

There are more to come in the near future!

## Install
#### Install and Set Up Go
Install Go with your package manager or by following the directions on
[Go's website](https://golang.org/doc/install).

Setup your `GOPATH` and `PATH` environment variables in your `~/.bashrc` file.
E.g.:

    export GOPATH='~/gowork'
    export PATH='$PATH:$GOPATH/bin'

#### Download Quilt
Download and install Quilt and its dependencies using `go get`

    go get github.com/quilt/quilt

Quilt is now installed! Check out the
[Getting Started](./docs/GettingStarted.md) guide for more detailed
instructions on how to get your Quilt deployment up and running.

## Contact Us

Questions? Comments? Feedback?  Please feel free to reach out to us
[here](http://landing.quilt.io/contact.php)!

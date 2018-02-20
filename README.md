[![Build Status](https://travis-ci.org/kelda/kelda.svg?branch=master)](https://travis-ci.org/kelda/kelda)
[![Go Report Card](https://goreportcard.com/badge/github.com/kelda/kelda)](https://goreportcard.com/report/github.com/kelda/kelda)
[![Code Coverage](https://codecov.io/gh/kelda/kelda/branch/master/graph/badge.svg)](https://codecov.io/gh/kelda/kelda)

# Kelda
_Formerly known as Quilt._

Dead simple containers in the cloud.

## Why Kelda?
Deploying applications to the cloud can be painful. Booting virtual machines, configuring
networks, and setting up databases, requires massive amounts of specialized knowledge —
knowledge that’s scattered across documentation, blog posts, tutorials, and source code.

Kelda, formerly Quilt, aims to make sharing this knowledge simple by encoding
it in JavaScript.  Just as developers package, share, and reuse application
code, Kelda’s JavaScript framework makes it possible to package, share, and
reuse the knowledge necessary to run applications in the cloud.

To take this knowledge into production, simply `kelda run` the JavaScript blueprint of
your application. Kelda will set up virtual machines, configure a secure network, install
containers, and whatever else is needed to get up and running smoothly on your favorite
cloud provider.

*Kelda is currently in beta.*

## Deploy Quickly on...

![providers](./docs/source/images/providers.png)

## Install

Install Kelda with npm:

```bash
$ npm install -g @kelda/install
```
Check out more in our [Quick Start tutorial](http://docs.kelda.io/#quick-start).

## Example
For a great example of how Kelda simplifies running complex applications, see
[how easy it is to run and manage Spark clusters with Kelda](https://github.com/kelda/spark).

## API

Run any container.

[//]: # (b1)
<!-- const {Container, LoadBalancer, Machine, allowTraffic, publicInternet} = require('kelda'); -->
```javascript
let web = new Container({
  name: 'web',
  image: 'someNodejsImage',
});
```

Load balance traffic.

[//]: # (b1)
```javascript
let webContainers = [];
for (i = 0; i < 3; i += 1) {
  webContainers.push(new Container({
    name: 'web',
    image: 'someNodejsImage',
  }));
}
// A load balancer over 3 containers.
let webLoadBalancer = new LoadBalancer({
  name: 'web-lb',
  containers: webContainers,
});
```

Share and import blueprints via npm.

[//]: # (b1)
```javascript
const Redis = require('@kelda/redis');
let redis = new Redis(2, 'AUTH_PASSWORD'); // 2 Redis database replicas.
```

Set up a secure network.

[//]: # (b1)
```javascript
allowTraffic(publicInternet, webContainers, 80); // Open the webservers' port 80 to the public internet.
redis.allowTrafficFrom(webContainers); // Let the web app communicate with Redis.
```

Deploy VMs on any [supported cloud provider](#deploy-quickly-on).

[//]: # (b1)
```javascript
let vm = new Machine({
  provider: 'Amazon',
  size: 't2.micro'
});
```

For more examples, have a look at [the blueprints in the blueprint library](http://docs.kelda.io/#blueprint-library)
and [check out our docs](http://docs.kelda.io).

## Kelda CLI

```bash
# Deploy your application.
$ kelda run ./someBlueprint.js

# SSH into VMs and containers.
$ kelda ssh <ID>

# Check the status of your deployment.
$ kelda show
```

This is just a small sample of the Kelda CLI. [Check out more handy commands](http://docs.kelda.io/#kelda-cli) for managing your deployment.

## Get Started

* Get started with [our **tutorial**](http://docs.kelda.io/#quick-start)
* Check out [our **docs**](http://docs.kelda.io/)
* [**Contribute** to the project](http://docs.kelda.io/#developing-kelda). The
[issues page](https://github.com/kelda/kelda/issues) has some great issues to
get started with.
* Learn more on our [**website**](http://kelda.io)
* [**Join our Slack!**](http://slack.kelda.io)

We would love to hear if you have any questions, suggestions, or other comments!

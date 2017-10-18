# Frequently Asked Questions

This section includes answers to common questions about Kelda, and solutions
to various issues.  If you run into an issue and can't find the answer here,
don't hesitate to email us at [discuss@kelda.io](mailto:discuss@kelda.io).

### What does Kelda use SSH keys for?
Kelda `Machine`s optionally take one or more public SSH keys. It is strongly
recommended to always provide at least one SSH key, as this will allow you
to SSH into VMs and containers, and enables useful Kelda CLI commands like
`kelda ssh` and `kelda logs` from any computer that holds a private SSH key
matching the public key set on the `Machine`.

### Why can't I access my website?
There are a few possible reasons:

1. Check that you are accessing the `PUBLIC IP` of the container that's serving
  the web application (and not the IP of another container or VM).
2. Make sure that the container exposes a port to the public internet. If it
  does, the `PUBLIC IP` will show a port number after the IP -- e.g.
  `54.241.251.192:80`. This says that port 80 is exposed to the public internet
  and thus that the application should be accessible from your (or any) browser.
  If there is no port number, and you are using an imported blueprint, check if
  the blueprint exports a function that will expose the application. If so, call
  this function in your blueprint. If there is no such function, use `allowFrom`
  and `publicInternet` in your blueprint to expose the desired port.
3. When exposing a different port than `80`, make sure to paste both the
  IP address _and_ the port number into the browser as `<IP>:<PORT>`.

### How do I get persistent storage?
Kelda currently doesn't support persistent storage, so we recommend using
a hosted database like [Firebase](https://firebase.google.com/).
If you still choose to run storage applications like [MongoDB](https://github.com/kelda/mongo)
or [Elasticsearch](https://github.com/kelda/elasticsearch) on Kelda, be aware
that the data will be lost if the containers or the VMs hosting them die.

### I tried to `kelda run` a blueprint on Amazon and nothing seems to be working.
If you're running a blueprint on AWS and the containers are not getting properly
created, you may have an issue with your VPC (Virtual Private Cloud) settings
on Amazon.  When this issue occurs, if you run `kelda show`, the machines will
all have status `connected`, but the containers will never progress to the
`scheduled` state (either the status will be empty, or for Dockerfiles that are
built in the cluster, the status will say `built`).  This issue only occurs
if you've changed your default VPC on Amazon, so if you don't know what a VPC is
or you haven't used one before on Amazon, this is probably not the issue you're
experiencing.

If you previously changed your default VPC on Amazon, it may be configured in a
way that prevents Kelda containers from properly communicating with each other.
This happens if the subnet configured for your VPC overlaps with 10.0.0.0/8,
which is the subnet that Kelda uses. This problem can manifest in many ways;
typically it looks like nothing seems to be working correctly.  For example, a
recent user who experienced this problem saw the following logs on the etcd
container on a Kelda worker:

```console
2017-06-05 21:14:40.823760 W | etcdserver: could not get cluster response from http://10.201.160.199:2380: Get http://10.201.160.199:2380/members: dial tcp 10.201.160.199:2380: getsockopt: no route to host
2017-06-05 21:14:40.823787 W | etcdmain: proxy: could not retrieve cluster information from the given urls
```

You can check (and fix) your VPC settings in the
[VPC section of the online AWS console](http://console.aws.amazon.com/vpc).

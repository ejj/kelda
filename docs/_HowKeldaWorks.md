# How Kelda Works

This section describes what happens when you run an application using Kelda.

The key idea behind Kelda is a blueprint: a blueprint describes every aspect of
running a particular application in the cloud, and is written in JavaScript.
Kelda blueprints exist for many common applications.  Using Kelda, you can run
one of those applications by executing just two commands on your laptop:

![Kelda Diagram](Kelda_Diagram.png)

The first command,`kelda daemon`, starts a long-running process that handles
launching machines in the cloud (on your choice of cloud provider), configuring
those machines, and managing them (e.g., detecting when they've failed so need
to be re-started).  The `kelda daemon` command starts the daemon, but doesn't
yet launch any machines. To launch an application, call `kelda run` with a
JavaScript blueprint (in this example, the blueprint is called `my_app.js`).
The `run` command passes the parsed blueprint to the daemon, and the daemon
sets up the infrastructure described in the blueprint.

Kelda runs applications using Docker containers. You can think of a container
as being like a process: as a coarse rule-of-thumb, anything that you'd launch
as its own process should have it's own container with Kelda.  While containers
are lightweight (like processes), they each have their own environment
(including their own filesystem and their own software installed) and are
isolated from other containers running on the same machine (unlike processes).
If you've never used containers before, it may be helpful to review the
[Docker getting started guide](https://docs.docker.com/get-started).

In this example, `my_app.js` described an application made up of three
containers, and it described a cluster with one master machine and two worker
machines.  The master is responsible for managing the worker machines, and no
application containers run on the master.  The application containers are run on
the workers; in this case, Kelda ran two containers on one worker machine and
one container on the other.

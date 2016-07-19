# Quilt Motivation
Given the number of production quality container orchestrators available
(Docker Swarm, Mesos, Kubernetes, etc.) one may ask: why Quilt?  What
motivates the development of a new system when the existing alternatives are
of such high quality?  In this document, we hope to answer this questions with
a particular focus on what differentiates Quilt from Kubernetes, the current
leader in container orchestration.  We note, however, that the same reasoning
applies when comparing Quilt to other orchestrators.

## Stitch
The most important difference between Quilt and Kubernetes lay in its
configuration mechanism.  Kubernetes exposes a conventional RESTful interface —
deployments are created through a series of API calls that gradually build up
the desired application.  While this process works, it encourages a
disorganization of the *application specification*.  To get a complete picture
of the current configuration, one must query a variety of
[Kubernetes APIs](http://kubernetes.io/docs/api/) all of which have their own
specific idiosyncrasies.

Instead of a traditional API, Quilt is based on a new configuration language,
Stitch.  Stitch is a domain specific programming language that allows
specification of distributed applications.  Stitch
*decouples* application specification from container orchestration such that
Stitch specifications exist independently of the container orchestrator that
deploys them.  They can be developed, version controlled, analyzed, and shared
using standard tools (`git` for version control, Github for development and
sharing), and without interacting with Quilt (or any other Stitch
implementation).  This design has a number of important implications.

#### Shareability
Stitches developed by one individual can be shared with others with
confidence that they will work as designed.  We imagine canonical
Stitches for popular systems will emerge (much like what we've begun
[here](../specs)) that will save new users the hassle of deciphering
reams of documentation to get an unfamiliar system up and running.

Additionally, Stitch has support for sharing built directly into the language!
Inspired by a similar feature in the Go programming language, stitch allows
`import` statements to point at stitches in remote repositories.  For example:

    (import "github.com/NetSys/quilt/specs/spark/spark")
    (spark.New "spark" 1 4 (list))

The above specification imports a Stitch for Apache Spark located at
[`github.com/NetSys/quilt/specs/spark/spark`]
(https://github.com/NetSys/quilt/blob/master/specs/spark/spark.spec), and calls
the `spark.New` function to initialize one master and four workers.  Users need
not understand anything about Spark to import the specification, download it,
and get it running.  Furthermore, they can modify, extend, and publish new
Stitches using the standard Github development process: fork, modify, and push
changes and the new stitch is shared.

#### Reproducibility
A Stitch *completely* specifies everything about an application, leaving no
policy implicitly buried in the container orchestrator.  It follows that, if
the application's behavior is deterministic, its behavior will be reproducible
across runs.  Furthermore, in many cases, the behavior will be reproducible
even when the stitch is deployed in different environments.  As a result:

* Developers distributing applications to users can rely on Stitch to
  ensure proper installation and configuration without hassle.

* Quality Assurance teams have confidence that the code they test is
  *precisely* what runs in production.

* Bugs found by users can be easily reproduced by developers.

* Academic researchers can publish the stitches that produced their
  experimental results.  This will allow future researchers to re-run
  experiments, comparing them against new systems and new environments.

#### Static Analysis
Administrators, for security reasons or simple sanity checking, may want to
prove things about their distributed application.  Certain nodes should or
shouldn't be able to communicate, or perhaps they should only be allowed to
communicate through an IDS.  The customer Database should never have direct
access to the public internet.  The system should be able to survive three
independent hardware failures.  All of these questions can be answered by
analyzing Stitch specifications without needing to interpret low level details
of a particular instantiation.

## Quilt
While Stitch has it's benefits, one may reasonably wonder why it's not
implemented on top of Kubernetes or some other popular container orchestrator.
There's nothing Quilt-specific about Stitch that prevents a Kubernetes backend
from being developed, and we do plan to do so in the near future.  That said,
we do believe Quilt has value as an independent Stitch reference
implementation.  Some of the most important reasons for this are outlined
below.

#### Simplicity
Stitch, being a full-fledged programming language (as opposed to a rather
limited API), has such expressive power that Quilt can remain quite simple.
This positions it in contrast to systems like Kubernetes, which
need to bake in concepts like Pods, Deployments, Replication Controllers,
Services, etc.

Instead we decouple application specification (Stitch), from application
deployment (Quilt), thus allowing Quilt to remain incredibly simple.  It need
only concern itself with containers, their placement constraints, and the
network that connects them.  The high level constructs Kubernetes provides can
be implemented in Stitch instead.  This design makes Quilt more
robust, easier to develop, and easier to test.  We also hope it makes Stitch
more portable — it's not limited by the least common denominator of all the
backend orchestrators it may support.

#### Ease of use
Quilt has a much more limited and focused goal than Kubernetes: To be the 
easiest way for users to deploy stitches.  It doesn't 't need to scale to 1000
node clusters, have blazing fast boot times, or support endless niche
use-cases.  Quilt doesn't attempt to solve Google's problems — instead, it
strives for good enough scale, good enough performance, and extreme
ease-of-use.

Quilt will achieve this by sacrificing configurability — it exposes no options
to users beyond the details specified in Stitch.  There's no tuning, tweaking,
or customizing for odd environments.  One can't replace this or that component
with a preferred alternative.  Quilt fixes everything — the placement
engine, Docker version, deployment operating system, and network virtualization
platform.  As a result, as developers, we have confidence that the system we've
built and tested will actually work as expected.

As an aside, this design decision is somewhat analogous to what's happened in
the smartphone market.  Today's container ecosystem is somewhat like Android —
there are endless options for users to choose from, mix and match, and tweak to
their heart's desire.  Want to run Kubernetes with the rkt container engine and
Project Calico for networking? Go for it!  These systems can be tailored to
a variety of niche use cases, but at the cost of increased complexity for the
average user.  Quilt, in contrast, aims to be the iPhone of container
orchestrators.  There are no options, everything is fixed, and as a result it
"just works".  It may not be suitable for the most sophisticated users with the
most unusual requirements, but that's OK.  Quilt aims to be an easy choice for
the average user.

#### Development
Stitch is still in its infancy and is evolving rapidly.  In this early stage,
being tied to an existing platform such as Kubernetes, would hamper its
evolution.  New features in Stitch that require changes to the underlying
container orchestrator can be easily accommodated by Quilt, without having to
convince an upstream dependency to implement them.  As Stitch matures rapid
evolution of the underlying platform will, of course, become less important.
At this point implementing backends for other orchestrators will become less
costly.

#### Features
In addition to what's described above, Quilt has a number of features baked in
that may be useful.

* **Self Deployment:**  Given a stitch, Quilt can automatically deploy all
  of the necessary infrastructure to operate it on any of its supported
  cloud providers (Amazon EC2, Microsoft Azure, Google Cloud).  For those users
  that don't already have Kubernetes/Mesos/Swarm up and running,
  this lowers the barrier to trying out Stitch.

* **Micro-segementation:**  Quilt takes advantage of network
  virtualization to enforce a tight network firewall between application
  nodes.  Containers are only allowed to communicate if the Stitch
  explicitly permits it, providing an enhanced level of network security.

* **Multi-Region Deployments:** Quilt uses network virtualization to hide
  differences in physical infrastructure from applications.  As a result,
  applications can be deployed on a single host or across geographically
  separated regions without change.

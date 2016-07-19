# Quilt Motivation

Given the number of production quality container orchestrators available one
may ask: why Quilt?  What motivates the development of a new system when the
existing alternatives are of such high quality?  In this document, we hope to
answer these questions with a particular focus on what differentiates Quilt
from Kubernetes.  We note, however, that the same reasoning applies when
comparing Quilt to other orchestrators.

## API

#### Kubernetes

Kubernetes exposes a conventional conventional RESTful
[API](http://kubernetes.io/docs/api/).  Deployment scripts make a series of API
calls which serve to *specify* the application that Kubernetes is responsible
for instantiating.  As new features and functionality are added, the
APIs tend to proliferate and grow in complexity.  In fact, one can already
see this beginning with Kubernetes: The basic unit of compute is a
[Pod](http://kubernetes.io/docs/user-guide/pods/). Then there's a grouping
mechanism [Deployments](http://kubernetes.io/docs/user-guide/deployments/).
Which in some cases is less desirable than a
[Service](http://kubernetes.io/docs/user-guide/services/). Or should I be using
a [Replica Set](http://kubernetes.io/docs/user-guide/replicasets/)? Or a
[Label](http://kubernetes.io/docs/user-guide/labels/)?

Modern application are complex and demand a lot from their infrastructure —
it's completely reasonable that the primitives used to specify them should be
correspondingly complex.  In Kubernetes, this complexity converges in the API
which, in effect, necessitates that *new functionality* breed *new APIs* with
ever increasing complexity.

This is no small matter.  APIs exert a pervasive influence on the Kubernetes
ecosystem, impacting not only Kubernetes itself, but also the deployment
scripts that interface with it and the developers who write those scripts.
Furthermore, this model tightly binds developers to Kubernetes.  Deployment
scripts written to the Kubernetes API aren't portable — they can't easily be
used with alternative orchestrators.  Kubernetes, like its competitors, leans
on the complexity of it's API to lock-in users.  The more they've committed to
the platform, the more code they've written bound to their API, and the less
able they are to stray to a competing orchestrator.

#### Quilt

Instead of a traditional API we advocate for a *decoupling* of application
specification from application deployment.  Application specification should
concern itself exclusively with *what* should be deployed, while application
deployment concerns itself with *how* it should be deployed.

This separation of concerns allows the deployment infrastructure to focus on a
relatively small subset of what the Kubernetes API supports.  At core, all
that's needed is the ability to create and destroy compute nodes, a basic
mechanism for naming and service discovery, and (optionally) the ability to
control which nodes communicate.  Higher level constructs can be ignored by the
deployment system leaving them instead to the application specification
mechanism.

Application specification is handled by a high level programming language
designed for the task such as our prototype language, Stitch.  The
specification language exists completely independent of the deployment system
allowing specifications to be developed, shared, and analyzed without
consulting the infrastructure that instantiates it.

Finally, we introduce an engine that executes the specification language to
generate a simplified representation of the application.  Effectively it
flattens the specification into the set of primitives that the deployment
system understands.  This serves as the glue between specification and
deployment — between Stitch and Quilt.

All together, this architecture amounts to enforcing a strict boundary between
the deployment system and the application specification.  The deployment system
can focus on what it cares about most, deployment, while leaving the complex
abstractions needed to describe the application to the language.  Additionally,
this implies the deployment system can be interchangeable, as long as they
understand the relatively simple basic primitives that the execution engine
emits, any system can be made to deploy stitch specifications.  Stitch, or some
equivalent, could serve as a universally accepted specification language with
backends to any number of deployment APIs — from Quilt managing one of its
currently supported cloud providers (Amazon EC2, GCE, Azure), to other
container orchestrators, like Kubernetes, Mesos, Docker Swarm, etc.

## Feature Diversity

Deployment systems have a wide variety of users with wildly different
requirements and goals.  Large companies necessarily care about operating at
massive scale, efficient utilization of resources, and speedy response times.
While smaller shops emphasize ease of use, speed getting started, and operator
management overhead.

Kubernetes take a *Mix and Match* attitude toward feature diversity commonly
called "batteries included but removable".  Don't like docker? Try
[rktnetes](https://rocket.readthedocs.io/en/stable/Documentation/using-rkt-with-kubernetes/)!
Don't like kubenet for networking? Try
[Weave](https://www.weave.works/weave-for-kubernetes/)!  Or
[Calico](https://www.projectcalico.org/calico-networking-for-kubernetes/)!  Or
[OVN](https://github.com/shettyg/ovn-kubernetes)!  Or any of
[these](http://kubernetes.io/docs/admin/networking/) for that matter!

New features either too niche or too disruptive are implemented instead in
plugins implemented by third parties.  This approach creates a great deal of
flexibility, but at a significant cost:

* Users have the burden of choosing the particular combination suitable for
  their situation.

* Less common combinations have fewer users testing them and are therefore less
  robust.

* Most seriously, often these plugins come with *their own APIs* through which
  their non-standard features are configured, further worsening the API
  proliferation issue.

Quilt, on the other hand, takes a different approach to feature diversity.  It
instead advocates for a "stitch first" approach to new application features.
Instead of adding new features through an ad hoc mix-and-match replacement of
deployment infrastructure, new features should first be expressed in Stitch
before they're implemented in the deployment infrastructure.  This leaves it up
to the deployment systems whether they implement new features depending on
whether or not they'd like to support the specifications that need them.

This approach will cause deployment systems to compete on *how well* they
implement application specifications instead of how effectively their APIs
attract users.  Additionally, since Stitch deployment systems are
interchangeable they will experience less pressure to be all things to all
users.  If a particular niche use-case is important enough, another deployment
system will support it.  Ultimately, these considerations will lead to a
healthier ecosystem in which deployment systems no longer compete on the
obscurity of their APIs, relying on vendor lock-in to squash competition.  What
matters in this world is performance, easy of use, robustness, and faithfulness
to the standard specification language.

The deployment engine bundled with Quilt takes this lesson to heart.  It it has
a simple goal: become the easiest way for new users to get started deploying
Stitches.  It's the Stitch reference implementation, not the solution to all
infrastructure automation problems.  In service of a robust experience, it
supports very little configurability, the OS version, Docker version, overlay
network, and everything else are fixed and unchangeable.  Only that which can
be expressed in Stitch can be configured in Quilt.

If Kubernetes is the Android of container orchestrators, Quilt, aims to be the
iPhone.  There are no options, everything is fixed, cohesive, and as a result
it "just works".  It may not be suitable for the most sophisticated users with
the most unusual requirements, but that's OK.  Quilt aims to be an easy choice
for the average user.

## Benefits

The Quilt approach described above — application specification instead of API
proliferation with an emphasis on simplicity and robustness over
configurability — has four key benefits described below.  Shareability,
Reproducibility, Portability, and Static Analysis:

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
  experimental results.  Thus allowing future researchers to re-run
  experiments, comparing them against new systems and new environments.

#### Portability

Stitch specifies applications abstractly, without relying on Quilt specifics.
Though Quilt will remain the Stitch reference implementation, other stitch
implementations will be developed.  Once these appear, applications described
in Stitch will be trivially portable across infrastructures — applications
developed on Kubernetes could run on Mesos, or Quilt, or any other supported
backend.  In contrast, applications written to the Kubernetes API are locked
in, running them on other platforms implies rewriting their deployment scripts.

#### Static Analysis
Administrators, for security reasons or simple sanity checking, want to
prove things about their distributed applications.  Certain nodes should or
shouldn't be able to communicate, or perhaps they should only be allowed to
communicate through an IDS.  The customer Database should never have direct
access to the public internet.  The system should be able to survive three
independent hardware failures.  All of these assertions could be answered by
analyzing Stitch specifications without needing to interpret low level details
of a particular instantiation.

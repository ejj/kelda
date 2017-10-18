# Developing Kelda

## Setup

### Install Go

The project is written in Go and supports Go version 1.8 or later. Install
Go using your package manager (Go is commonly referred to as "golang" in
package managers and elsewhere) or via the
[Go website](https://golang.org/doc/install).

If you've never used Go before, we recommend reading the overview to Go
workspaces [here](https://golang.org/doc/code.html#Workspaces). In short,
you'll need to configure the `GOPATH` environment variable to be the location
where you'll keep all Go code. For example, if you'd like your Go workspace
to be `$HOME/gowork`:

```console
export GOPATH="$HOME/gowork"
export PATH="$GOPATH/bin:$PATH"
```

Add these commands to your `.bashrc` so that they'll be run automatically each
time you open a new shell.

### Download Kelda

Clone the Kelda repository into your Go workspace using `go get`:

```console
$ go get github.com/kelda/kelda
```

This will install Kelda in your Go workspace at
`$GOPATH/src/github.com/kelda/kelda`, and compile Kelda. After running
installing Kelda, the `kelda` command should execute successfully in your shell.

<aside class="notice">If you use git to clone the Kelda repository, make sure
that you clone it to the directory
<code class="prettyprint">$GOPATH/src/github.com/kelda/kelda</code>.
The Go language is opinionated about the directory structure of code, and if
you don't put Kelda in the expected location, you'll run into errors when you
use Go to compile Kelda.</aside>

### Protobufs
If you change any of the proto files, you'll need to regenerate the protobuf
code. We currently use protoc v3. On a Mac with homebrew, you can install protoc v3
using:

```console
$ brew install protobuf
```

On other operating systems you can directly download the protoc binary
[here](https://github.com/google/protobuf/releases), and then add it to your `$PATH`.

To generate the protobufs simply call:

```console
$ make generate
```

### Dependencies
We use [govendor](https://github.com/kardianos/govendor) for dependency
management. If you are using Go 1.5 make sure `GO15VENDOREXPERIMENT` is set to 1.

To add a new dependency:

1. Run `go get foo/bar`
2. Edit your code to import `foo/bar`
3. Run `govendor add +external`

To update a dependency:

```console
$ govendor update +vendor
```

## Building and Testing

### Building and Testing the Go Code

To build Kelda, run `go install` in the Kelda directory.  To do things beyond
basic build and install, several additional build tools are required.  These
can be installed with the `make go-get` target.

Note that if you've previously installed Kelda with npm, there will be
another Kelda binary installed on your machine (that was downloaded during the
npm installation).  If you want to develop Kelda,
you probably want to make sure that when you run `kelda`, the version you're
developing (that was compiled from the Go code) gets run, and not the Kelda
release that was downloaded from npm. Check that this is the case:

```console
$ which kelda
/Users/kay/gowork/bin/kelda
```

If running `which kelda` results in a path that includes your `$GOPATH$`, like
the one above, you're all set.  If it instead returns someplace else, e.g.,
`/usr/local/bin/kelda`, you'll need to fix your `$PATH` variable so that
`$GOPATH/bin` comes first.

To run the `go` tests, use the `gocheck` Make target in the root directory:

```console
$ make gocheck
```

If you'd like to run the tests in just one package, e.g., the tests in the
`engine` package, use `go test` with the package name:

```console
$ go test github.com/kelda/kelda/engine
```

### Building and Testing the JavaScript Code

To run the JavaScript code, you'll need to use `npm` to install Kelda's
dependencies:

```console
$ npm install .
```

If you're developing the kelda package, you must also tell Node.js
to use your local development copy of the Kelda JavaScript bindings (when you
use Kelda to run blueprints) by running:

```console
$ npm link .
```

in the directory that contains your local Kelda source files. For each blueprint
that uses the Kelda JavaScript bindings, you must also run:

```console
$ npm link kelda
```

in the directory that contains the blueprint JavaScript files.

To run the JavaScript tests for `bindings.js`, use the `jscheck` build target:

```console
$ make jscheck
```

## Contributing Code

We highly encourage contributions to Kelda from the Open Source community!
Everything from fixing spelling errors to major contributions to the
architecture is welcome.  If you'd like to contribute but don't know
where to get started, feel free to reach out to
[us](http://kelda.io/#contact) for some guidance.

The project is organized using a hybrid of the Github and Linux Kernel
development workflows.   Changes are submitted using the Github Pull Request
System and, after appropriate review, fast-forwarded into master.
See [Submitting Patches](#submitting-patches) for details.

### Go Coding Style
The coding style is as defined by the `gofmt` tool: whatever transformations it
makes on a piece of code are considered, by definition, the correct style.
Unlike official go style, in Kelda lines should be wrapped to 89 characters. To
make sure that your code is properly formatted, run:

```console
$ make golint
```

Running `make format` will fix many (but not all) formatting errors.

### JavaScript Coding Style

Kelda uses the AirBnb JavaScript style guide. To make sure that your JavaScript
code is properly formatted, run:

```console
$ make jslint
```

### Git Commits

The fundamental unit of work in the Kelda project is the git commit.  Each
commit should be a coherent whole that implements one idea completely and
correctly. No commits should break the code, even if they "fix it" later.
Commit messages should be wrapped to 80 characters and begin with a title of
the form `<Area>: <Title>`.  The title should be capitalized, but not end
with a period.  For example, `provider: Move the provider interfaces into the
cloud directory` is a good title. When possible, the title should fit in
50 characters.

All but the most trivial of commits should have a brief paragraph below the
title (separated by an empty line), explaining the _context_ of the commit.
Why the patch was written, what problem it solves, why the approach was taken,
what the future implications of the patch are, etc.

Commits should have proper author attribution, with the full name of the commit
author, capitalized properly, with their email at the time of authorship.
Commits authored by more than one person should have a `Co-Authored-By:` tag at
the end of the commit message.

### Submitting Patches
Patches are submitted for inclusion in Kelda using a Github Pull Request.

A pull request is a collection of well formed commits that tie together
in some theme, usually the larger goal they're trying to achieve.  Completely
unrelated patches should be included in separate pull requests.

Pull requests are reviewed by one person: either by a committer, if the code was
submitted by a non-committer, or by a non-committer otherwise. You do not
need to choose a reviewer yourself; [kelda-bot](https://github.com/kelda/bot)
will randomly select a reviewer from the appropriate group. Once the reviewer
has approved the pull request, a committer will merge it. If the reviewer
requests changes, leave a comment in the PR once you've implemented the changes,
so that the reviewer knows that the PR is ready for another look.

It should be noted that the code
review assignment is just a suggestion. If a another contributor, or member of
the public for that matter, happens to do a detailed review and provide a `+1`
then the assigned reviewer is relieved of their responsibility.  If you're not
the assigned reviewer, but would like to do the code review, please comment in
the PR to that effect so the assigned reviewer knows they need not review the
patch.

We expect patches to go through multiple rounds of code review, each involving
multiple changes to the code.  After each round of review, the original author
is expected to update the pull request with appropriate changes.  These changes
should be incorporated into the patches in their most logical places.  I.E.
they should be folded into the original patches or, if appropriate inserted as
a new patch in the series.  Changes _should not_ be simply tacked on to the end
of the series as tweaks to be squashed in later -- at all stages the PRs should
be ready to merge without reorganizing commits.

## Code Structure
Kelda is structured around a central database (`db`) that stores information about
the current state of the system. This information is used both by the global
controller (Kelda Global) that runs locally on your machine, and by the `minion`
containers on the remote machines.

### Database
Kelda uses the basic `db` database implemented in `db.go`. This database supports
insertions, deletions, transactions, triggers and querying.

The `db` holds the tables defined in `table.go`, and each table is simply a
collection of `row`s. Each `row` is in turn an instance of one of the types
defined in the `db` directory - e.g. `Cluster` or `Machine`. Note that a
`table` holds instances of exactly one type. For instance, in `ClusterTable`,
each `row` is an instance of `Cluster`; in `ConnectionTable`, each `row` is an
instance of `Connection`, and so on. Because of this structure, a given row can
only appear in exactly one table, and the developer therefore performs
insertions, deletions and transactions on the `db`, rather than on specific
tables. Because there is only one possible `table` for any given `row`, this is
safe.

The canonical way to query the database is by calling a `SelectFromX` function
on the `db`. There is a `SelectFromX` function for each type `X` that is stored
in the database. For instance, to query for `Connection`s in the
`ConnectionTable`, one should use `SelectFromConnection`.

### Kelda Global

The first thing that happens when Kelda starts is that your blueprint is parsed
by Kelda's JavaScript library, `kelda.js`. `kelda.js` then puts the connection
and container specifications into a sensible format and forwards them to the
`engine`.

The `engine` is responsible for keeping the `db` updated so it always reflects
the desired state of the system. It does so by computing a diff of the config
 and the current state stored in the database. After identifying the
differences, `engine` determines the least disruptive way to update the
database to the correct state, and then performs these updates. Notice that the
`engine` only updates the database, not the actual remote system - `cloud`
takes care of that.

The `cloud` takes care of making the state of your system equal to the state
of the database. `cloud` continuously checks for updates to the database, and
whenever the state changes, `cloud` boots or terminates VMs in you system to
reflect the changes in the `db`.

Now that VMs are running, the `minion` container will take care of starting the
necessary system containers on its host VM. The `foreman` acts like the middle
man between your locally run Kelda Global, and the `minion` on the VMs. Namely,
the `foreman` configures the `minion`, notifies it of its (the `minion`'s)
role, and passes it the policies from Kelda Global.

All of these steps are done continuously so the blueprint, database and
remote system always agree on the state of the system.

### Kelda Remote

As described above, `cloud` is responsible for booting VMs. On boot, each VM
runs docker and a `minion`. The VM is furthermore assigned a role - either
`worker` or `master` - which determines what tasks it will carry out. The
`master` minion is responsible for control related tasks, whereas the `worker`
VMs do "the actual work" - that is, they run containers. When the user
specifies a new container the config file, the scheduler will choose a worker
VM to boot this container on. The `minion` on the chosen VM is then notified,
and will boot the new container on its host. The `minion` is similarly
responsible for tearing down containers on its host VM.

While it is possible to boot multiple `master` VMs, there is only one effective
`master` at any given time. The remaining `master` VMs simply perform as
backups in case the leading `master` fails.

## Developing the Minion

If you're developing code in `minion`, you'll need to do some extra setup to
test your new code.  To make Kelda run your local version of the minion image,
and not the default Kelda minion image, follow these steps:

1. Create a new empty repository on your favorite registry -
[docker hub](https://hub.docker.com/) for example.
2. Modify `keldaImage` in [cfg.go](../cloud/cfg/cfg.go) to
point to your repo.
3. Modify `Version` in [version.go](../version/version.go) to be "latest".
This ensures that you will be using the most recent version of the minion
image that you are pushing up to your registry.
4. Create a `.mk` file (for example: `local.mk`) to override variables
defined in [Makefile](../Makefile). Set `REPO` to your own repository
(for example: `REPO = sample_repo`) inside the `.mk` file you created.
5. Create the docker image: `make docker-build-kelda`
   * Docker for Mac and Windows is in beta. See the
   [docs](https://docs.docker.com/) for install instructions.
6. Sign in to your image registry using `docker login`.
7. Push your image: `make docker-push-kelda`.

After the above setup, you're good to go - just remember to build and push your
image first, whenever you want to run the `minion` with your latest changes.

# Contributing

Everything from fixing spelling errors to major contributions to the
architecture is welcome.  If you'd like to contribute but don't know
where to get started, feel free to reach out to
[Ethan](http://people.eecs.berkeley.edu/~ejj/contact.html) for some guidance.

The project is organized using a hybrid of the Github and Linux Kernel
development workflows.   Changes are submitted using the Github Pull Request
System and, after appropriate review, fast-forwarded into master.
See [Submitting Patches](#submitting-patches) for details.

## Coding Style
The coding style is as defined by the `gofmt` tool: whatever transformations it
makes on a piece of code are considered, by definition, the correct style.  In
addition, `golint`, `go vet`, and `go test` should pass without warning on all
changes.  An easy way to check these requirements is to run `make lint check`
on each patch before submitting a pull request.

Unlike official go style, in Quilt lines should be wrapped to 89 characters.

The fundamental unit of work in the Quilt project is the git commit.  Each
commit should be a coherent whole that implements one idea completely and
correctly. No commits should break the code, even if they "fix it" later.
Commit messages should be wrapped to 80 characters and begin with a title of
the form `<Area>: <Title>`.  The title should be capitalized, but not end
with a period.  For example, `provider: Move the provider interfaces into the
cluster directory` is a good title. When possible, the title should fit in
50 characters.

All but the most trivial of commits should have a brief paragraph below the
title (separated by an empty line), explaining the _context_ of the commit.
Why the patch was written, what problem it solves, why the approach was taken,
what the future implications of the patch are, etc.

Commits should have proper author attribution, with the full name of the commit
author, capitalized properly, with their email at the time of authorship.
Commits authored by more than one person should have a `Co-Authored-By:` tag at
the end of the commit message.

## Submitting Patches
Patches are submitted for inclusion in Quilt using a Github Pull Request or
(though not preferred), they may be sent directly to
[Ethan](http://people.eecs.berkeley.edu/~ejj/contact.html) using
`git-format-patch` and `git-send-email`.

A pull request is a collection of well formed commits that tie together
in some theme, usually the larger goal they're trying to achieve.  Completely
unrelated patches should be included in separate pull requests.

Pull requests go through a two stage review process.  In the first stage,
[quilt-bot](https://github.com/quilt-bot) will randomly assign a contributor
to review the patch.  The first stage reviewer should provide feedback, wait
for fixes, and ultimately approve the patch using Github's code review tool.
By doing this, the reviewer is taking responsibility for the quality of the
patch.  In effect they are asserting that the patch is perfect and ready to be
merged.

Once the patch has been approved by the first reviewer, quilt-bot will assign a
committer to do a second (sometimes cursory) review. The committer will
either merge the patch, provide feedback, or if a great deal of work is
still needed, punt the patch back to the original reviewer.

It should be noted that the code
review assignment is just a suggestion. If a another contributor, or member of
the public for that matter, happens to do a detailed review and provide a `+1`
then the assigned reviewer is relieved of their responsibility.  If you're not
the assigned reviewer, but would like to do the code review, it may be polite
to comment in the PR to that effect so the assigned reviewer knows they need
not review the patch.

We expect patches to go through multiple rounds of code review, each involving
multiple changes to the code.  After each round of review, the original author
is expected to update the pull request with appropriate changes.  These changes
should be incorporated into the patches in their most logical places.  I.E.
they should be folded into the original patches or, if appropriate inserted as
a new patch in the series.  Changes _should not_ be simply tacked on to the end
of the series as tweaks to be squashed in later -- at all stages the PRs should
be ready to merge without reorganizing commits.

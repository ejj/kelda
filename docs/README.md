# Kelda Documentation

The Kelda documentation is located at [docs.kelda.io](http://docs.kelda.io).
This directory contains the Markdown files that are used to generate those
docs. We do not recommend directly browing the Markdown files, because the
links are designed to work properly for the HTML version of the page, and
will not work correctly in the Markdown version.

If you're a developer and you'd like to build the docs, run `make` in this
directory:

```console
$ make
```

This command will clone two other repositories into the `build` folder:
one with our JSDoc template, which helps to compile the JSDoc in the Kelda
JavaScript into HTML; and a second with Slate, which combines the Markdown
files here and the JSDoc HTML into a single HTML page.  Slate requires
Ruby and bundler, so you will need to install those if you don't already
have them; e.g., using Homebrew:

```console
$ brew install ruby
$ gem install bundler # gem is Ruby's equivalent of make.
```

For more information about Slate and its dependencies, refer to the
[our Slate fork](https://github.com/kelda/slate).

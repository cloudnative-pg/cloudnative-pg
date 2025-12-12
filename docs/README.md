# CloudNativePG documentation

CloudNativePG documentation is written in Markdown from the `docs` folder. The
`src` folder contains the sources with the `.md` extension.

We have adopted [Docusaurus](https://docusaurus.io/) as an open
source solution to build the documentation starting from Markdown
format.

Before you submit a pull request for the documentation, you must have
gone through the following steps:

1. local test of the documentation
2. run through the spell checker

## How to locally test the documentation

You can locally test the documentation using Docker executing the following
command and point your browser to `http://127.0.0.1:3000/docs/`:

``` bash
docker run --rm -ti -p 3000:3000 \
    -v ./src:/website/docs \
    ghcr.io/cloudnative-pg/docs:latest
```

The previous uses the infrastructure we use to build the
[CloudNativePG documentation
website](https://cloudnative-pg.github.io/docs) but using your local
Markdown files to compile the documentation of the development version
of the operator.

Make sure you review what you have written by putting yourself in the end
user's shoes. Once you are ready, proceed with the spell check and then with
the pull request.

## How to run the spell checker

Every time you work on the documentation, please run from the top directory:

``` bash
make spellcheck
```

This will run a spell checker and highlight all the words that need to be
either fixed or added to the `.wordlist-en-custom.txt` file.

## How to build the documentation in HTML

From the `docs` folder, run the following command to build the documentation
and place it in the `dist` directory:

``` bash
docker run --rm -v "$(pwd):$(pwd)" -w "$(pwd)" \
    minidocks/mkdocs \
    mkdocs build -v -d dist
```

## Reminders

If you added samples to `docs/src/samples` or modified existing samples, please
consider if they should be included in the curated [list of examples](src/samples.md)

And please help keeping the samples in the curated list, as well as any samples
named `cluster-example-*` in runnable condition.
These can be a big help for beginners.

## License

The CloudNativePG documentation and all the work under the `docs` folder is
licensed under a Creative Commons Attribution 4.0 International License.

<!-- SPDX-License-Identifier: CC-BY-4.0 -->

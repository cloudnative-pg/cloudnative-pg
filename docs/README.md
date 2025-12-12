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

To ensure your documentation changes look correct before creating a Pull
Request, you can build and view the documentation locally using Docker.

Execute the following command in your terminal. This command uses the same
infrastructure as the official CloudNativePG documentation website, mounting
your local files for preview:

```bash
docker run --rm -ti -p 3000:3000 \
    -v ./src:/website/docs \
    ghcr.io/cloudnative-pg/docs:latest
```

Once the server is running, open your browser and navigate to the local
documentation preview at: `http://127.0.0.1:3000/docs/`.

Thoroughly check your changes—put yourself in the end user's shoes to verify
clarity and accuracy. Complete a final spell check, and then proceed with
submitting your pull request.

## How to run the spell checker

Every time you work on the documentation, please run from the top directory:

``` bash
make spellcheck
```

This will run a spell checker and highlight all the words that need to be
either fixed or added to the `.wordlist-en-custom.txt` file.

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

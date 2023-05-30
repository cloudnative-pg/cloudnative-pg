# Setting up your development environment for CloudNativePG

> **IMPORTANT:** If you are looking for setup information of your local
> machine for local testing, please refer to the ["Quickstart" section](../../docs/src/quickstart.md)
> in the official documentation of CloudNativePG. In particular, we
> recommend `kind`.

In order to contribute to the source code of CloudNativePG, you need to have a
proper development environment. Normally, you configure the environment once, by
following the next three steps:

- [Installation of the requirements](#installation): one-off operation that installs the Go compiler, GNU/Make, Kind, and other components
- [Fork of the repository](#forking-the-repository): use Github to fork
  CloudNativePG in your account and `git` to clone it on your computer
- [Local build and deploy](#building-the-operator-and-deploying-it-locally):
  build the current *trunk* of CloudNativePG from the `main` branch; if this
  works, your environment is validated and you can start developing your own
  patches

## Installation

Currently, we provide installation instructions for [GNU/Linux](#gnulinux-systems),
[MacOS X](#mac-os-x), and [Windows Subsystem for Linux](microsoft-wsl2)
(feel free to submit a PR for any improvement you might think of).

Once you have followed the instructions for your system, run the following
command from the main directory to verify that GNU/Make is properly installed
and view the available tasks you can run:

```
make help
```

Normally, the next step after this is to [clone the CloudNativePG repository](#forking-the-repository)
on your local workstation.

<!-- TODO: We should add an easier way to check that requirements are met -->

### GNU/Linux systems

Make sure you have the following executables available in the `PATH`
environment variable:

- Go 1.17+ compiler
- GNU Make
- [Kind](https://kind.sigs.k8s.io/) v0.11.x or greater
- [golangci-lint](https://github.com/golangci/golangci-lint)
- [goreleaser](https://goreleaser.com/)

In addition, check that the following packages are installed in your system:

- `coreutils`,
- `diffutils`,
- `findutils`,
- `git`,
- `gpg`,
- `jq`,
- `make`,
- `pandoc`,
- `sed`,
- `tar`,
- `util-linux`,
- `zlib1g`.

The previous list assumes that you are using a Debian-based distribution. For
other distributions the name of the packages may vary.

## Microsoft WSL2

To setup a development environment you can use the same instructions discussed
for GNU/Linux.

Please ensure that the requirements for [kind using
WSL2](https://kind.sigs.k8s.io/docs/user/using-wsl2/) are also met.

### Mac OS X

We recommend to use [brew](https://brew.sh/) to install the following
components in your Mac OS X system:

``` bash
brew install go \
  kind \
  golangci/tap/golangci-lint \
  goreleaser
```

Please note that bash v5.0+ is required, this can be installed with:
``` bash
brew install bash
```

>**⚠️ Note:**
>If `kind` is already installed, make sure you have the latest version:
>
>``` bash
>brew upgrade kind
>```

You can now proceed with installing the remaining required packages:

``` bash
brew install jq \
  make \
  coreutils \
  diffutils \
  findutils \
  git \
  gpg \
  gnu-getopt \
  gnu-sed \
  gnu-tar \
  pandoc \
  zlib
```

You can then follow the provided instructions. As you'll see, you need to add
the following lines to the profile of your shell (eg `~/.bash_profile`):

``` bash
# Go settings
export GOPATH="${HOME}/go"
# Homebrew settings
export PATH="/usr/local/opt/gettext/bin:$PATH"
export PATH="/usr/local/opt/coreutils/libexec/gnubin:$PATH"
export PATH="/usr/local/opt/findutils/libexec/gnubin:$PATH"
export PATH="/usr/local/opt/gnu-getopt/bin:$PATH"
export PATH="/usr/local/opt/gnu-sed/libexec/gnubin:$PATH"
export PATH="/usr/local/opt/gnu-tar/libexec/gnubin:$PATH"
export MANPATH="/usr/local/opt/coreutils/libexec/gnuman:$MANPATH"
export MANPATH="/usr/local/opt/findutils/libexec/gnuman:$MANPATH"
export MANPATH="/usr/local/opt/gnu-getopt/share/man:$MANPATH"
export MANPATH="/usr/local/opt/gnu-sed/libexec/gnuman:$MANPATH"
export MANPATH="/usr/local/opt/gnu-tar/libexec/gnuman:$MANPATH"
export LDFLAGS="-L/usr/local/opt/zlib/lib $LDFLAGS"
export LDFLAGS="-L/usr/local/opt/gettext/lib $LDFLAGS"
export LDFLAGS="-L/usr/local/opt/readline/lib $LDFLAGS"
export CPPFLAGS="-I/usr/local/opt/zlib/include $CPPFLAGS"
export CPPFLAGS="-I/usr/local/opt/gettext/include $CPPFLAGS"
export CPPFLAGS="-I/usr/local/opt/readline/include $CPPFLAGS"
export PKG_CONFIG_PATH="/usr/local/opt/readline/lib/pkgconfig"
# GPGv2 backward compatibility
export GPG_AGENT_INFO=~/.gnupg/S.gpg-agent::1
export GPG_TTY=$(tty)
```

If you are using Docker Desktop, please make sure you follow the instructions in the
["Settings for Docker Desktop" section of the `kind` documentation](https://kind.sigs.k8s.io/docs/user/quick-start/#settings-for-docker-desktop).

MacOS Airplay uses port 5000 and can interfere with docker repository port. Make sure Airplay is off (System Preferences -> Share -> uncheck the Airplay checkbox).

## Forking the repository

Unless you are a developer with write permissions on the main repository of
[CloudNativePG](https://github.com/cloudnative-pg/cloudnative-pg), in order to
propose a pull request for CloudNativePG you need to fork the repo.

Please follow the [instructions provided by Github about forking a repository](https://docs.github.com/en/get-started/quickstart/fork-a-repo),
making sure you use the [cloudnative-pg/cloudnative-pg](https://github.com/cloudnative-pg/cloudnative-pg)
instead of `octocat/Spoon-Knife` - which is used in the example.

Then clone it on your local environment and proceed with the next step.

## Building the operator and deploying it locally

These instructions will guide you through building the operator from a local
cloned repository, normally a fork of CloudNativePG, and deploying it in
your local `kind` cluster for evaluation and testing.

Enter your local clone directory and select `main` as the working branch to
build and deploy:

```shell
cd cloudnative-pg
git checkout main
make deploy-locally
```

This will build the operator based on the `main` branch content, create a
`kind` cluster in your workstation with a container registry that provides the
operator image that you just built.

If everything went well, you are now able to use this version of the operator
in the local `kind` cluster. For example, you should be able to see the
CloudNativePG operator installed with:

```shell
kubectl get deploy -n cnpg-system cnpg-controller-manager
```

Now that your system has been validated, you can tear down the local cluster with:

```shell
make kind-cluster-destroy
```

Congratulations, you have a suitable development environment. You are now able
to contribute your patches to CloudNativePG!

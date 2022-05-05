# Contributing to CloudNativePG source code

If you want to contribute to the source code of CloudNativePG and innovate in
the database in Kubernetes space, this is the right place. Welcome!

We have a truly open source soul. That's why we welcome new contributors. Our
goal is to enable you to become the next committer of CloudNativePG, by having
a good set of docs that guide you through the development process. Having said this,
we know that everything can always be improved, so if you think our documentation
is not enough, let us know or provide a pull request based on your experience.

Feel free to ask in the ["dev" chat](https://cloudnativepg.slack.com/archives/C03D68KGG65)
if you have questions or are seeking guidance.

## About our development workflow

CloudNativePG follows [trunk-based development](https://cloud.google.com/architecture/devops/devops-tech-trunk-based-development),
with the `main` branch representing the trunk.

We adopt the ["Github Flow"](https://guides.github.com/introduction/flow/)
development workflow, with some customizations:

- the [Continuous Delivery](https://cloud.google.com/architecture/devops/devops-tech-continuous-delivery)
  branch is called `main` and is protected
- Github is configured for linear development (no merge commits)
- development happens in separate branches created from the `main` branch and
  called "*dev/ISSUE_ID*"
- once completed, developers must submit a pull request
- two reviews by different maintainers are required before a pull request can be merged

We adopt the [conventional commit](https://www.conventionalcommits.org/en/v1.0.0/)
format for commit messages.

The [roadmap](https://github.com/orgs/cloudnative-pg/projects/1) is defined as a [Github Project](https://docs.github.com/en/issues/trying-out-the-new-projects-experience/about-projects).

We have an [operational Kanban board](https://github.com/orgs/cloudnative-pg/projects/2)
we use to organize the flow of items.

---

<!--
TODO:

- Merge "hack/e2e/README.md" here
- Add architecture diagrams in the "contribute" folder
- Add https://github.com/istio/istio/wiki/Writing-Good-Pull-Requests
- Ideas from https://github.com/istio/istio/wiki/Reviewing-Pull-Requests
- ...

-->

## Your development environment for CloudNativePG

In order to write even the simplest patch for CloudNativePG you must have setup
your workstation to build and locally test the version of the operator you are
developing.  All you have to do is follow the instructions you find in
["Setting up your development environment for CloudNativePG"](development_environment/README.md).


---

## Sign your work

We use the Developer Certificate of Origin (DCO) as an additional safeguard for
the CloudNativePG project. This is a well established and widely used mechanism
to assure contributors have confirmed their right to license their contribution
under the project's license. Please read
[developer-certificate-of-origin](./developer-certificate-of-origin).

If you can certify it, then just add a line to every git commit message:

```
  Signed-off-by: Random J Developer <random@developer.example.org>
```

or use the command `git commit -s -m "commit message comes here"` to sign-off on your commits.

Use your real name (sorry, no pseudonyms or anonymous contributions).
If you set your `user.name` and `user.email` git configs, you can sign your
commit automatically with `git commit -s`.
You can also use git [aliases](https://git-scm.com/book/en/v2/Git-Basics-Git-Aliases)
like `git config --global alias.ci 'commit -s'`. Now you can commit with `git ci` and the
commit will be signed.

---

---

## To be classified (yet)

### How to upgrade the list of licenses

To generate or update the `licenses` folder run the following command:

```bash
make licenses
```

---

### Release procedure

#### Initial verification

- Make sure release notes for the release have been updated
  in `docs/src/release_notes.md` and have been approved by
  the maintainers
- Make sure that the operator capability levels page has been
  updated in `docs/src/operator_capability_levels.md` and approved
  by the maintainers

#### Release steps

The following steps assume version 1.15.0 as the one to be released. Alter the
instructions accordingly for your version.

1. Run `hack/release.sh 1.15.0`.
2. Approve the PR that is automatically generated.
3. Merge the PR.
4. Wait until all [Github Actions](https://github.com/cloudnative-pg/cloudnative-pg/actions) finish.

---


# Contributing to CloudNativePG source code

If you want to contribute to the source code of CloudNativePG and innovate in
the database in Kubernetes space, this is the right place. Welcome!

We have a truly open source soul. That's why we welcome new contributors. Our
goal is to enable you to become the next committer of CloudNativePG, by having
a good set of docs that guide you through the development process. Having said this,
we know that everything can always be improved, so if you think our documentation
is not enough, let us know or provide a pull request based on your experience.

If you have any questions or need guidance, feel free to reach out in the
[#cloudnativepg-dev](https://cloud-native.slack.com/archives/C08MW1HKF40) channel
on the [CNCF Slack workspace](https://communityinviter.com/apps/cloud-native/cncf).

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

**For AI-assisted development:** Review [AI_CONTRIBUTING.md](../AI_CONTRIBUTING.md) and [AI_CONTRIBUTORS.md](../AI_CONTRIBUTORS.md) for technical standards and social patterns that help AI agents produce review-ready PRs.

The [roadmap](https://github.com/orgs/cloudnative-pg/projects/1) is defined as a [Github Project](https://docs.github.com/en/issues/trying-out-the-new-projects-experience/about-projects).

We have an [operational Kanban board](https://github.com/orgs/cloudnative-pg/projects/2)
we use to organize the flow of items.

---

<!--
TODO:

- Add architecture diagrams in the "contribute" folder
- ...

-->

## Testing the latest development snapshot

If you want to test or evaluate the latest development snapshot of
CloudNativePG before the next official patch release, you can simply run:

```sh
kubectl apply --server-side -f \
  https://raw.githubusercontent.com/cloudnative-pg/artifacts/main/manifests/operator-manifest.yaml
```

---

## Your development environment for CloudNativePG

In order to write even the simplest patch for CloudNativePG you must have setup
your workstation to build and locally test the version of the operator you are
developing.  All you have to do is follow the instructions you find in
["Setting up your development environment for CloudNativePG"](development_environment/README.md).

---

## Your testing environment for CloudNativePG

Can you manually test your patch for all the supported PostgreSQL versions on
all the supported Kubernetes versions? You probably agree this is not feasible
(have you ever heard of the inverted pyramid of testing?).

This is the reason why we have invested since day 1 of CloudNativePG in
automated testing. Please refer to ["Running E2E tests on your environment"](./e2e_testing_environment/README.md)
for detailed information.

---

## Submit a pull request

> First and foremost: as a potential contributor, your changes and ideas are
> welcome at any hour of the day or night, weekdays, weekends, and holidays.
> Please do not ever hesitate to ask a question or send a PR.

**IMPORTANT:** before you submit a pull request, please read this document from
the Istio documentation which contains very good insights and best practices:
["Writing Good Pull Requests"](https://github.com/istio/istio/wiki/Writing-Good-Pull-Requests).

If you have written code for an improvement to CloudNativePG or a bug fix,
please follow this procedure to submit a pull request:

1. [Create a fork](development_environment/README.md#forking-the-repository) of CloudNativePG
2. **External contributors**: Comment on the issue with "I'd like to work on this" and wait for assignment. 
   **Maintainers**: Self-assign the ticket and move it to `Analysis` or `In Development` phase of
   [CloudNativePG operator development](https://github.com/orgs/cloudnative-pg/projects/2)
3. **External contributors**: Run local unit tests and basic e2e tests using `FEATURE_TYPE=smoke,basic make e2e-test-kind` or `TEST_DEPTH=0 make e2e-test-kind` for critical tests only. 
   **Maintainers**: [Run the comprehensive e2e tests in the forked repository](e2e_testing_environment/README.md#running-e2e-tests-on-a-fork-of-the-repository)
4. Once development is finished, create a pull request from your forked project
   to the CloudNativePG project. **Maintainers** will move the ticket to the `Waiting for First Review`
   phase. 
   
   > Please make sure the pull request title and message follow
   > [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/)
   
   > To facilitate collaboration, always [allow edits by maintainers](https://docs.github.com/en/pull-requests/collaborating-with-pull-requests/working-with-forks/allowing-changes-to-a-pull-request-branch-created-from-a-fork)

One of the maintainers will then proceed with the first review and approve the
CI workflow to run in the CloudNativePG project.  The second reviewer will run
end-to-end test against the changes in fork pull request. If testing passes,
the pull request will be labeled with `ok-to-merge` and will be ready for
merge.

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

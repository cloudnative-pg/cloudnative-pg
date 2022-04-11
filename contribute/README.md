# Contributing to CloudNativePG source code

This section is still Work-In-Progress.

<!--
TODO:

- Add Github Project (Kanban)
- Add Roadmap
- Merge "hack/e2e/README.md" here
- Add architecture diagrams in the "contribute" folder
- ...

-->

---

## Development workflow

CloudNativePG adopts the ["Github Flow"](https://guides.github.com/introduction/flow/)
development workflow, with some customizations:

- the Continuous Delivery branch is called "main" and is protected
- Github is configured for linear development (no merge commits)
- development happens in separate branches created from the "main" branch and
  called "*dev/ISSUE_ID*"
- once completed, developers must submit a pull request

## Commit messages

We adopt the [conventional commit](https://www.conventionalcommits.org/en/v1.0.0/)
format for commit messages.

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

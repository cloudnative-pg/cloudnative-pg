# Developing procedure


This section describes how to contribute to CloudNativePG project. For 
contributors come from outside `cloudnative-pg` organization, it is recommended
to fork [CloudNativePG project](https://github.com/cloudnative-pg/cloudnative-pg) 
and finish the developing and testing in the fork project. when the dev and testing work is done,
then create a pull request from fork project to CloudNativePG. All feature and bugs 
need pass unit test and end-to-end test before merging. 

## Contributing process 

### Pull request merge criteria

- Two approves for the pull request and at least one of them is maintainer
- All unit test and end-to-end test are green and `ok-to-merge` label is added in pull request

### Process for contributor from outside of `cloudnative-pg` organization

1. Create a [fork project](https://github.com/cloudnative-pg/cloudnative-pg/fork) from CloudNativePG.
2. Self-assign the ticket and working on it in the fork project. Move the ticket to `Analysis` or `In Development`
phase of [CloudNativePG operator development](https://github.com/orgs/cloudnative-pg/projects/2)
3. Run the unit test and end-to-end test in fork project. 
   - Unit test is in `continuous-integration` workflow, which is automatically run each time 
   when push happens to the developing branch, or you can also manually trigger it through `workflow_dispatch`
   - End-to-End test is in `continuous-delivery` workflow, which could be triggered through slash command
   `/test` in a pull request in the fork project, or it could also be invoked manually through 
   `workflow_dispatch`
4. Once everything is OK, create a fork pull request from fork project to CloudNativePG project and move the ticket
to `Waiting for First Review` phase. Please make sure the pull request title and message meet [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/)
5. The `maintainer` will first review the fork pull request and approve the ci workflow to run in CloudNativePG project.
6. The second reviewer will run end-to-end test against the changes in fork pull request, if testing is pass, pull request 
will be labeled with `ok-to-merge` and ready for merge.

### Process for contributor from `cloudnative-pg` organization

1. Create a dev branch for the development work, the format for the dev branch is `dev/<ticket number>`
2. Create a pull request from dev branch to main branch, and verify the unit test in `continuous-integration` workflow.
Please make sure the pull request title and message meet [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/)
3. Use slash command `/test` to trigger the end-to-end test and verify the status.
4. Once everything is OK, move the ticket to `Waiting for First Review` status.


## Slash command 

###  /test slash command

Slash command `/test` is used to trigger to run the end-to-end test in a pull request. Only user who has `write` permission
to cloudNativePG project could use this command to run end-to-end test. The format of the slash command is 

      `/test options`

Options supported in slash are:

- test_level (level or tl for short)  
 The priority of test to run, If test level is specific in slash command, all test with that priority and higher will 
be triggered. Default value for the test level is 4. Available values are:
  - 0: highest
  - 1: high
  - 2: medium 
  - 3: low
  - 4: lowest 

- depth (d for short)   
  The metrics (kubernetes and postgresql version) for the test to be run. Default value is `main`, available values 
are:
  - push
  - main
  - pull_request
  - schedule
- feature_type (type or ft for short)  
  The label for the end-to-end test to be run, empty value means all labels. Default value is empty, available 
type are: `disruptive, performance, upgrade, smoke, basic, service-connectivity, self-healing,
  backup-restore, operator, observability, replication, plugin, postgres-configuration, pod-scheduling,
  cluster-metadata, recovery, importing-databases, storage, security, maintenance`. For more information about
please see doc [using feature type test selection/filter](https://github.com/cloudnative-pg/cloudnative-pg/tree/main/contribute/e2e_testing_environment#using-feature-type-test-selectionfilter)
- log_level (ll for short)    
  Define the log value for cloudNativePG operator, which will be specified as the value for `--log-value` for operator.
Default value is info and available values are: `error, warning, info, debug, trace`
- build_plugin (plugin or bp for shore)  
  Whether to run the build cnpg plugin job in end-to-end test. Available values are true and false and default value 
is false.


  Example:
  1. Trigger an e2e test to run all test cases with `highest` test level, we want to cover most kubernetes and postgres metrics

  ```
     /test -tl=0 d=schedule
  ```
  2. Run smoking test for the pull request
  ```
     /test type=smoke
  ```

###  /ok-to-merge slash command

User with write permission could use `/ok-to-merge` slash command to add `ok-to-merge` label to a pull request. 

## Back-porting 

When pull request is created in cloudNativePG project, following labels is added to the pull request for  back-porting purpose
 - backport requested
 - release-x.y
 - release-x1.y1
 - ...

Label `backport requested` indicates that this pull request will be auto back-ported to release branch when it is merged, 
label `release-x.y` is target branch to back-port. 

To disable the back-port for current pull request, you can 

- Add label `do not backport` or remove label `backport requested` in pull request will disable the auto back-port to all
release branches. 
- Remove label `release-x.y` in pull request will disable the auto back-port to specific release branch `release-x.y`.

Once the back-port is failed, ticket named `Backport failure for pull request xxx` will be opened to track.


## Commit Specification in cloudNativePG project

1. Commits MUST be prefixed with a type, which consists of a noun, `feat`, `fix`, etc., followed by a colon and space.
2. An optional scope MAY be provided after a type. A scope is a phrase describing a section of the codebase enclosed in parenthesis, e.g., `fix(parser): `
3. A description MUST immediately follow the type/scope prefix. The description is a short description of the code changes, e.g.,

`fix: array parsing issue when multiple spaces were contained in the string.`

```
<type>[optional scope]: <description>

[optional body]

[optional footer]

[optional BREAKING CHANGE: at the beginning of its optional body or footer section]
```


| Type | Name | Description | 
| -------|------|-------------|
| feat | Features | A new feature |
| fix | Bug Fixes | A bug fix |
| docs | Documentation | Documentation only changes |
| style | Styles | Changes that do not affect the meaning of the code (white-space, formatting, missing semi-colons, etc) |
| refactor | Code Refactoring |  A code change that neither fixes a bug nor adds a feature |
| perf | Performance Improvements | A code change that improves performance |
| test | Tests | Adding missing tests or correcting existing tests |
| build | Builds | Changes that affect the build system or external dependencies |
| ci | Continuous Integrations |Changes to our CI configuration files and scripts |
| chore | Chores | Other changes that don't modify src or test files | 
| revert | Reverts | Reverts a previous commit | 


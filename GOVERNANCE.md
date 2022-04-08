# CloudNativePG Governance

This document defines governance policies for the CloudNativePG project.

## Our Mission

*"Run PostgreSQL, the Kubernetes way."*

PostgreSQL is one of the most loved databases in the world, especially in
traditional VM and bare metal installations.

CloudNativePG was originally conceived by PostgreSQL experts and
Kubernetes administrators within [2ndQuadrant](https://www.2ndquadrant.com/) -
later acquired by [EDB](https://www.enterprisedb.com/) - with the goal to
increase the adoption of Postgres within Kubernetes environments.

## Values

Developing a PostgreSQL operator for Kubernetes requires the highest level of
technical quality for both PostgreSQL and Kubernetes.
The goal of CloudNativePG is to innovate in the data in Kubernetes space,
making it easier for organizations to build microservice applications that rely
on a PostgreSQL database directly inside Kubernetes.

We believe that an open source community is the most effective way to address
the complexity of the domain through teamwork, trust, merit, openness,
constructive dissent, diversity, commitment, and accountability.

CloudNativePG and its leadership embrace the following values:

* Technical excellence: Our mindset is to provide the best experience of
  PostgreSQL in Kubernetes, and this requires the highest skills in both
  technologies.

* Built-in Quality and Security: Automated testing is a way to improve
  the quality directly in the product, avoiding manual inspection (citing
  Dr. Deming). Similarly, security must be part of the development process.

* Openness: Communication and decision-making happen in the open and are
  discoverable for future reference. As much as possible, all discussions
  and work take place in public forums and open repositories. Dissent, if
  constructive and expressed with respect and manners, is encouraged as it
  is seen as an innovation enabler.

* Fairness: All stakeholders have the opportunity to provide feedback and submit
  contributions, which will be considered on their merits.

* Community over Product or Company: Sustaining and growing our community takes
  priority over shipping code or sponsors' organizational goals. Each
  contributor participates in the project as an individual.

* Inclusivity: We innovate through different perspectives and skill sets, which
  can only be accomplished in a welcoming and respectful environment.

* Participation: Responsibilities within the project are earned through
  participation, and there is a clear path up the contributor ladder into leadership
  positions.


## Maintainers

CloudNativePG is made up of a few repositories. The primary repository is the
[CloudNativePG operator](https://github.com/cloudnative-pg/cloudnative-pg) one,
which acts as umbrella project for the CloudNativePG community, and lists the
guidelines that every satellite project adheres to.

The maintainers of each project are kept up to date in the
[CODEOWNERS](CODEOWNERS) file, which is located in the root folder of the
project. Maintainers have write access to the repository and can merge their
own patches or patches from others.

This privilege is granted with some expectation of responsibility: maintainers
are people who care about the CloudNativePG project and want to help it grow
and improve. A maintainer is not just someone who can make changes, but someone
who has demonstrated their ability to collaborate with the team, get the most
knowledgeable people to review code, contribute high-quality code, and follow
through to fix issues (in code or tests).

A maintainer is a contributor to the CloudNativePG project's success and a
citizen helping the project succeed.

## Becoming a Maintainer

To become a Maintainer, you need to demonstrate the following:

  * commitment to the project:
    * participate in discussions, contributions, code, and documentation reviews
      for 6 months or more,
    * perform reviews for 10 non-trivial pull requests,
    * contribute 10 non-trivial pull requests and have them merged,
  * ability to write quality code and/or documentation,
  * ability to collaborate with the team,
  * understanding of how the team works (policies, processes for testing and code review, etc.),
  * understanding of the project's code base and coding and documentation style.

A new Maintainer must be proposed by an existing maintainer by starting a new
[Github discussion under the "Maintainers room" category](https://github.com/cloudnative-pg/cloudnative-pg/discussions/categories/maintainers-room).

Two more maintainers need to second the nomination. If no one objects in 5
working days (Italy's timezone), the nomination is accepted. If anyone objects
or wants more information, the maintainers discuss and usually come to a
consensus (within the 5 working days). If issues can't be resolved, there's a
simple majority vote among current maintainers.

Maintainers who are selected will be granted the necessary GitHub rights,
and invited to the private maintainer mailing list.

## Meetings

Time zones permitting, Maintainers are expected to participate in the public
developer meeting, which occurs bi-weekly on the first and third Tuesday of
each month, via Zoom:

- Meeting ID: [973 0110 7092](https://enterprisedb.zoom.us/j/97301107092?pwd=ckJtV2ZoSDdKZW9EWlR4ckpOWlNWQT09)
- Passcode: 504519

[Agenda and minutes are publicly available](https://docs.google.com/document/d/1Bmf2AZG5WLKAyESJbYk7MbsfiuD3jgdIDQrDkNuKT9w/edit?usp=sharing).

Maintainers will also have closed meetings to discuss security reports
or Code of Conduct violations. Such meetings should be scheduled by any
Maintainer on receipt of a security issue or CoC report. All current Maintainers
must be invited to such closed meetings, except for any Maintainer accused of a CoC violation.

## CNCF Resources

Any Maintainer may suggest a request for CNCF resources by creating a new
[Github discussion under the "Maintainers room" category](https://github.com/cloudnative-pg/cloudnative-pg/discussions/categories/maintainers-room),
or during a meeting.  A simple majority of Maintainers approves the request.
The Maintainers may also choose to delegate working with the CNCF to
non-Maintainer community members.

## Code of Conduct

[Code of Conduct](./code-of-conduct.md)
violations by community members will be discussed and resolved
on the [private Maintainer mailing list](mailto:conduct@cloudnative-pg.io).
If the reported CoC violator is a Maintainer, the Maintainers will instead
designate two Maintainers to work with CNCF staff in resolving the report.

## Voting

While most business in CloudNativePG is conducted by "lazy consensus",
periodically, the Maintainers may need to vote on specific actions or changes.
A vote can be taken in [a new Github discussion under the "Maintainers room" category](https://github.com/cloudnative-pg/cloudnative-pg/discussions/categories/maintainers-room),
or on [the private Maintainer mailing list](mailto:security@cloudnative-pg.io) for
security or conduct matters. Votes may also be taken during developer meetings.
Any Maintainer may demand a vote be taken.

Most votes require a simple majority of all Maintainers to succeed. Maintainers
can be removed by a 2/3 majority vote of all Maintainers, and changes to this
Governance require a 2/3 vote of all Maintainers.

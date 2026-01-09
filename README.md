[![CNCF Landscape](https://img.shields.io/badge/CNCF%20Landscape-5699C6)][cncf-landscape]
[![Latest Release](https://img.shields.io/github/v/release/cloudnative-pg/cloudnative-pg.svg)][latest-release]
[![GitHub License](https://img.shields.io/github/license/cloudnative-pg/cloudnative-pg)][license]
[![OpenSSF Best Practices](https://www.bestpractices.dev/projects/9933/badge)][openssf]
[![OpenSSF Scorecard Badge][openssf-scorecard-badge]][openssf-socrecard-view]
[![Documentation][documentation-badge]][documentation]
[![Stack Overflow](https://img.shields.io/badge/stackoverflow-cloudnative--pg-blue?logo=stackoverflow&logoColor=%23F48024&link=https%3A%2F%2Fstackoverflow.com%2Fquestions%2Ftagged%2Fcloudnative-pg)][stackoverflow]
[![FOSSA Status][fossa-badge]][fossa]
[![CLOMonitor](https://img.shields.io/endpoint?url=https://clomonitor.io/api/projects/cncf/cloudnative-pg/badge)](https://clomonitor.io/projects/cncf/cloudnative-pg)
[![Artifact Hub](https://img.shields.io/endpoint?url=https://artifacthub.io/badge/repository/cloudnative-pg)](https://artifacthub.io/packages/search?repo=cloudnative-pg)

# Welcome to the CloudNativePG project!

**CloudNativePG** is a comprehensive open source platform designed to
seamlessly manage [PostgreSQL](https://www.postgresql.org/) databases within
Kubernetes environments, covering the entire operational lifecycle from initial
deployment to ongoing maintenance. The main component is the CloudNativePG
operator.

CloudNativePG was originally built and sponsored by [EDB](https://www.enterprisedb.com).

## Table of content

- [Code of conduct](CODE_OF_CONDUCT.md)
- [Governance policies](https://github.com/cloudnative-pg/governance/blob/main/GOVERNANCE.md)
- [Contributing](CONTRIBUTING.md)
- [Adopters](ADOPTERS.md)
- [Commercial Support](https://cloudnative-pg.io/support/)
- [License](LICENSE)

## Getting Started

The best way to get started is the [Quickstart Guide](https://cloudnative-pg.io/docs/devel/quickstart/).

## Scope

The goal of CloudNativePG is to increase the adoption of PostgreSQL, one of the
most loved DBMS in traditional VM and bare metal environments, inside
Kubernetes, thus making the database an integral part of the development
process and GitOps CI/CD automated pipelines.

### In scope

CloudNativePG has been designed by Postgres experts with Kubernetes
administrators in mind. Put simply, it leverages Kubernetes by extending its
controller and by defining, in a programmatic way, all the actions that a good
DBA would normally do when managing a highly available PostgreSQL database
cluster.

Since the inception, our philosophy has been to adopt a Kubernetes native
approach to PostgreSQL cluster management, making incremental decisions that
would answer the fundamental question: "What would a Kubernetes user expect
from a Postgres operator?".

The most important decision we made is to have the status of a PostgreSQL
cluster directly available in the `Cluster` resource, so to inspect it through
the Kubernetes API. We've fully embraced the operator pattern and eventual
consistency, two of the core principles upon which Kubernetes is built for
managing complex applications.

As a result, the operator is responsible for managing the status of the
`Cluster` resource, keeping it up to date with the information that each
PostgreSQL instance manager regularly reports back through the API server.
Changes to the cluster status might trigger, for example, actions like:

* a PostgreSQL failover where, after an unexpected failure of a cluster's
  primary instance, the operator itself elects the new primary, updates the
  status, and directly coordinates the operation through the reconciliation
  loop, by relying on the instance managers

* scaling up or down the number of read-only replicas, based on a positive or
  negative variation in the number of desired instances in the cluster, so that
  the operator creates or removes the required resources to run PostgreSQL,
  such as persistent volumes, persistent volume claims, pods, secrets, config
  maps, and then coordinates cloning and streaming replication tasks

* updates of the endpoints of the PostgreSQL services that applications rely on
  to interact with the database, as Kubernetes represents the single source of
  truth and authority

* updates of container images in a rolling fashion, following a change in the
  image name, by first updating the pods where replicas are running, and then
  the primary, issuing a switchover first

The latter example is based on another pillar of CloudNativePG:
immutable application containers - as explained in the
[blog article "Why EDB Chose Immutable Application Containers"](https://www.enterprisedb.com/blog/why-edb-chose-immutable-application-containers).

The above list can be extended. However, the gist is that CloudNativePG
exclusively relies on the Kubernetes API server and the instance manager to
coordinate the complex operations that need to take place in a business
continuity PostgreSQL cluster, without requiring any assistance from an
intermediate management tool responsible for high availability and failover
management like similar open source operators.

CloudNativePG also manages additional resources to help the `Cluster` resource
manage PostgreSQL - currently `Backup`, `ClusterImageCatalog`, `ImageCatalog`,
`Pooler`, and `ScheduledBackup`.

Fully embracing Kubernetes means adopting a hands-off approach during temporary
failures of the Kubernetes API server. In such instances, the operator refrains
from taking action, deferring decisions until the API server is operational
again. Meanwhile, Postgres instances persist, maintaining operations based on
the latest known state of the cluster.

### Out of scope

CloudNativePG is exclusively focused on the PostgreSQL database management
system maintained by the PostgreSQL Global Development Group (PGDG). We are not
currently considering adding to CloudNativePG extensions or capabilities that
are included in forks of the PostgreSQL database management system, unless in
the form of extensible or pluggable frameworks. [The operator itself can be extended
via a plugin interface called CNPG-I](https://github.com/cloudnative-pg/cnpg-i).

CloudNativePG doesn't intend to pursue database independence (e.g. control a
MariaDB cluster).

## Communications

- [Github Discussions](https://github.com/cloudnative-pg/cloudnative-pg/discussions)
- [Slack](https://cloud-native.slack.com/archives/C08MAUJ7NPM)
  (join the [CNCF Slack Workspace](https://communityinviter.com/apps/cloud-native/cncf)).
- [Twitter](https://twitter.com/CloudNativePg)
- [Mastodon](https://mastodon.social/@CloudNativePG)
- [Bluesky](https://bsky.app/profile/cloudnativepg.bsky.social)

## Resources

- [Roadmap](ROADMAP.md)
- [Website](https://cloudnative-pg.io)
- [FAQ](docs/src/faq.md)
- [Blog](https://cloudnative-pg.io/blog/)
- [CloudNativePG plugin Interface (CNPG-I)](https://github.com/cloudnative-pg/cnpg-i).

## Adopters

A list of publicly known users of the CloudNativePG operator is in [ADOPTERS.md](ADOPTERS.md).
Help us grow our community and CloudNativePG by adding yourself and your
organization to this list!

### CloudNativePG at KubeCon

- November 10, 2025, KubeCon North America 2025 in Atlanta: ["Project Lightning Talk: CloudNativePG: Running Postgres The Kubernetes Way"](https://www.youtube.com/watch?v=pYwYwehQX3U&t=4s) - Gabriele Bartolini, EDB
- November 11, 2025, KubeCon North America 2025 in Atlanta: ["Modern PostgreSQL Authorization With Keycloak: Cloud Native Identity Meets Database Security"](https://www.youtube.com/watch?v=TYgPemq06fg) - Yoshiyuki Tabata, Hitachi, Ltd. & Gabriele Bartolini, EDB
- November 13, 2025, KubeCon North America 2025 in Atlanta: ["Quorum-Based Consistency for Cluster Changes With CloudNativePG Operator"](https://www.youtube.com/watch?v=iQUOO3-JRK4&list=PLj6h78yzYM2MLSW4tUDO2gs2pR5UpiD0C&index=67) - Jeremy Schneider, GEICO Tech & Gabriele Bartolini, EDB
- April 4, 2025, KubeCon Europe in London: ["Consistent Volume Group Snapshots, Unraveling the Magic"](https://sched.co/1tx8g) - Leonardo Cecchi (EDB) and Xing Yang (VMware)
- November 11, 2024, Cloud Native Rejekts NA 2024: ["Maximising Microservice Databases with Kubernetes, Postgres, and CloudNativePG"](https://www.youtube.com/watch?v=uBzl_stoxoc&ab_channel=CloudNativeRejekts) - Gabriele Bartolini (EDB) and Leonardo Cecchi (EDB)
- March 21, 2024, KubeCon Europe 2024 in Paris: ["Scaling Heights: Mastering Postgres Database Vertical Scalability with Kubernetes Storage Magic"](https://kccnceu2024.sched.com/event/1YeM4/scaling-heights-mastering-postgres-database-vertical-scalability-with-kubernetes-storage-magic-gabriele-bartolini-edb-gari-singh-google) - Gari Singh, Google & Gabriele Bartolini, EDB
- March 19, 2024, Data on Kubernetes Day at KubeCon Europe 2024 in Paris: ["From Zero to Hero: Scaling Postgres in Kubernetes Using the Power of CloudNativePG"](https://colocatedeventseu2024.sched.com/event/1YFha/from-zero-to-hero-scaling-postgres-in-kubernetes-using-the-power-of-cloudnativepg-gabriele-bartolini-edb) - Gabriele Bartolini, EDB
- November 7, 2023, KubeCon North America 2023 in Chicago: ["Disaster Recovery with Very Large Postgres Databases (in Kubernetes)"](https://kccncna2023.sched.com/event/1R2ml/disaster-recovery-with-very-large-postgres-databases-gabriele-bartolini-edb-michelle-au-google) - Michelle Au, Google & Gabriele Bartolini, EDB
- October 27, 2022, KubeCon North America 2022 in Detroit: ["Data On Kubernetes, Deploying And Running PostgreSQL And Patterns For Databases In a Kubernetes Cluster"](https://kccncna2022.sched.com/event/182GB/data-on-kubernetes-deploying-and-running-postgresql-and-patterns-for-databases-in-a-kubernetes-cluster-chris-milsted-ondat-gabriele-bartolini-edb) - Chris Milsted, Ondat & Gabriele Bartolini, EDB

### Useful links

- ["Quorum-Based Consistency for Cluster Changes With CloudNativePG Operator"](https://www.youtube.com/watch?v=sRF09UMAlsI) (webinar) - Jeremy Schneider, GEICO Tech & Leonardo Cecchi, EDB
- [Data on Kubernetes (DoK) Community](https://dok.community/)
- ["How to migrate your PostgreSQL database in Kubernetes with ~0 downtime from anywhere" by Gabriele Bartolini](https://gabrielebartolini.it/articles/2024/03/cloudnativepg-recipe-5-how-to-migrate-your-postgresql-database-in-kubernetes-with-~0-downtime-from-anywhere/) (March 2024)
- ["Maximizing Microservice Databases with Kubernetes, Postgres, and CloudNativePG" by Gabriele Bartolini](https://gabrielebartolini.it/articles/2024/02/maximizing-microservice-databases-with-kubernetes-postgres-and-cloudnativepg/) (February 2024)
- ["Recommended Architectures for PostgreSQL in Kubernetes" by Gabriele Bartolini](https://www.cncf.io/blog/2023/09/29/recommended-architectures-for-postgresql-in-kubernetes/) (September 2023)
- ["The Current State of Major PostgreSQL Upgrades with CloudNativePG" by Gabriele Bartolini](https://www.enterprisedb.com/blog/current-state-major-postgresql-upgrades-cloudnativepg-kubernetes) (August 2023)
- ["The Rise of the Kubernetes Native Database" by Jeff Carpenter](https://thenewstack.io/the-rise-of-the-kubernetes-native-database/) (December 2022)
- ["Why Run Postgres in Kubernetes?" by Gabriele Bartolini](https://cloudnativenow.com/kubecon-cnc-eu-2022/why-run-postgres-in-kubernetes/) (May 2022)
- ["Shift-Left Security: The Path To PostgreSQL On Kubernetes" by Gabriele Bartolini](https://www.tfir.io/shift-left-security-the-path-to-postgresql-on-kubernetes/) (April 2021)
- ["Local Persistent Volumes and PostgreSQL usage in Kubernetes" by Gabriele Bartolini](https://www.2ndquadrant.com/en/blog/local-persistent-volumes-and-postgresql-usage-in-kubernetes/) (June 2020)

## Star History

[![Star History Chart](https://api.star-history.com/svg?repos=cloudnative-pg/cloudnative-pg&type=Date)](https://star-history.com/#cloudnative-pg/cloudnative-pg&Date)

## Trademarks

*[Postgres, PostgreSQL and the Slonik Logo](https://www.postgresql.org/about/policies/trademarks/)
are trademarks or registered trademarks of the PostgreSQL Community Association
of Canada, and used with their permission.*

[cncf-landscape]: https://landscape.cncf.io/?item=app-definition-and-development--database--cloudnativepg
[stackoverflow]: https://stackoverflow.com/questions/tagged/cloudnative-pg
[latest-release]: https://github.com/cloudnative-pg/cloudnative-pg/releases/latest
[documentation]: https://cloudnative-pg.io/docs
[license]: https://github.com/cloudnative-pg/cloudnative-pg?tab=Apache-2.0-1-ov-file#readme
[openssf]: https://www.bestpractices.dev/projects/9933
[openssf-scorecard-badge]: https://api.scorecard.dev/projects/github.com/cloudnative-pg/cloudnative-pg/badge
[openssf-socrecard-view]: https://scorecard.dev/viewer/?uri=github.com/cloudnative-pg/cloudnative-pg
[documentation-badge]: https://img.shields.io/badge/Documentation-white?logo=data%3Aimage%2Fpng%3Bbase64%2CiVBORw0KGgoAAAANSUhEUgAAACAAAAAgCAYAAABzenr0AAAGN0lEQVR4nJRXXWwcVxU%2B8%2F%2BzP%2BPZtR2v7dqy07jUJUALNaiK6lZyUVVKWgGKaIv8QCMekBAVQlQICcEzVZFQVYFKQhASEBHlISJPCRJEshTFChgrIYHEiYMh69jetffHM7Mzc%2B9Bs7vjnTs7yZpZWbt37s%2F5zne%2Bc861CD0eXRkbHc3NfjeffvxNAGEAgULD2756v35%2B3qe1Nc4fnQVEXlA2LnOcXlCF8S%2B6vvVgq%2FL3M65X3e51PvfQCU4WJgZe%2B8GQ8fS7AKgjBB8KEHwjDXZSjkf0CREAaXM2eI9c65siqWxWl360Xl74ANHz%2Fy8AitxnTBfmz%2BhyYS4wGhwObQCIHSA0AigOMBzvOsXzd4pnjyL6NMmWEH8hi2b28Og3%2FqRJA0ewfQy0v1vGO2NovwPo%2FEU%2FwVgSU1PI%2BSu79v3lJAB8HM%2BTI%2FO%2FUUXzM4xHIe0xI4DdRqOAwnF%2F38ePPyzaDIDh%2FMxcWh462m08aojuGY97C0nrAEHg9BlF0fmeAPr0J15vbaKsp0BZQzEDEAlP9B209UIIVXUta%2FQEQHwxgxFjTc%2BRskAwrgVWmHtg22vMPJwLDqGUNJIAMHVAkGu3WdpZz6NAkgSXpINSycluV28er1a3rJ4M3F2%2F9AtCvXKycRrTQttrjINjxxxIL9jevxdaDHU%2FTBr6pL5ruzuLZubgUQBOY2hPij3GBUe7tBCMBRE2KrXVSz0BBI%2FtPVgtV%2F%2FxkZ5WSjI%2F%2BFIXC3sHJwgT4yFqrZFFTSlVrp3sGYLwcfxSmXCbS00j2Ms4K7qkOsFx6qdTuiHtG4AimfmM8NyvOvR2G48qXtZ2fsfrN7%2BqpcRyUp0glKiimDm4TwAcHBp%2B9WeA4ki0GMWNR9OVF8BZvn7xtI%2FF09H8jzLEgz6yLwCDuelnFXHkTZZOytCOEdqDOtGwsm%2BNj00fXt%2B6%2Bj4vcA7bwNrZwENmXwAKuZnvsNRThs5ozMPfPiHyoDF7xiduHcXb70A8dRFheHjiySQATBZk0nl9MHPkBEWUoEtYjyrPFNwGzfdlD37Zdu98KCv%2BMmD2BYpUCvcST39e0%2BS1Wr249FAAg7mPzWrS5NstEbE0xrsiA6QN1PfRFLnhr%2BspxVJTlY8Mw1DqNXeyCQFREEXz9cHB0QOev73QaNhOF4B%2B45PHFHFgDhJTqjuubJFqX1KQco7NTTuW8kq95k2G4eLEGzM7lfItnjNeTKcOfV%2FT8hOuV77A9IK0XjgMpCO0ZiuV3L%2F6njCFAOmucGB3OII5XgCXEJTDdZLElVbu3Vz0fWexvL30k0B6ggBACOmIUBAEUKX0dDTvW7RCYcdZPq6n%2FSsQnUO2RuyBRgQ9Rc5mMvJ6CNIj1nXfd9qWAsCkaZzJAk1L8UjVqY737dSjfCGrPHWqXL32Q0mB%2F2BXnke00WaEYv2aTzAbnuV5pcWkDGAAGJmhSafh6hjr%2BW2SVYHrP7bb%2BOdPW%2FUgflGlTM2gaK%2Ft7tp6%2BN6yixdN89DcIwGktIFPABfNbwoQqQWEUnDJzg1g0jDeK5p7Kp7nensXFI7uyAr%2FLyM7fYLnpa6LYScE8vDnot5hrKlslm%2BfE3nVxJgO4o3KcYu%2FF8XM8yFQ27n%2F65Te%2FzKl3Jhpjj6TCIDneRD5%2FItxr1vdkALw7p1qfeWPpjHxMtsXaPxu6FLc%2BrnbSB1r7fcrlr36nqwMzQfnplJDryQCGOh%2FbLjhcM%2FEvQ4Pdund9xRV5m1LfTXaF%2BK9gsLGB9nsgddcz8thM%2FarPzYM8%2FFazf9sMFaU%2Fi%2FwvNANwEhPvUGR8ozn7d%2BiDKXixtKpbHp81nV9E7puRy31ixKUbOe%2Fv3Ud891ghhDrL5Z975eaOvV%2BCNRp0Gfz%2BcJjDABdTwlpdfKbId0t5XYAcHz5D5ZVtWUp9%2Flog2L7PgVJqZx0HOE5Cqghemv1%2Bt%2FeGBmZ%2BdB2yNN72UEpnzXG32YADA186i3bIpPxMhuKrFK%2Fd77JUnbkKbYvRJlC8DzKSZK76Lq1he2dKy%2BZuSfesSz5a2xHDbLJ%2BJaqdv5H4EUY%2BzbG2m9HgN7mg81bfw4W1uu7AjvHaqDhqF%2FZ3Fq5XFy%2FcESSDsx5fvZ7wLEsNfXk%2BjlVHfpSCOB%2FAQAA%2F%2F8zd8orZc2N9AAAAABJRU5ErkJggg%3D%3D
[fossa-badge]: https://app.fossa.com/api/projects/git%2Bgithub.com%2Fcloudnative-pg%2Fcloudnative-pg.svg?type=small
[fossa]: https://app.fossa.com/projects/git%2Bgithub.com%2Fcloudnative-pg%2Fcloudnative-pg?ref=badge_small

# Adopters

Below is a list of organizations and users who have publicly shared that
they’re using PostgreSQL in Kubernetes with the CloudNativePG operator in a
production environment.

The purpose of this list is to inspire others to join the movement and help
grow our open-source community and project.

Adding your organization takes just 5 minutes of your time, but it means a lot
to us!

## How to Add Your Organization

You can add your organization to this list in two ways:

- [Open a pull request](https://github.com/cloudnative-pg/cloudnative-pg/pulls)
  to directly update this file.
- [Edit the file](https://github.com/cloudnative-pg/cloudnative-pg/blob/main/ADOPTERS.md)
  directly on GitHub.

Use the commit title: **"docs: add <ORGANIZATION_NAME> to `ADOPTERS.md`"** and
be sure to [sign off your work](contribute/README.md#sign-your-work).

If you need any assistance, feel free to ask in our Slack chat—we’re here to
help!

## CloudNativePG Adopters

This list is sorted in chronological order, based on the submission date.

| Organization | Contact | Date | Description of Use |
| ------------ | ------- | ---- | ------------------ |
| [EDB](https://enterprisedb.com) | @gbartolini | 2023-02-21 | EDB's DataBase as a Service solution, [BigAnimal](https://www.enterprisedb.com/products/biganimal-cloud-postgresql), relies on CloudNativePG to run PostgreSQL and Postgres Distributed workloads. EDB is one of the primary contributors to the open source PostgreSQL project and the founder of CloudNativePG. |
| [Clustermarket](https://clustermarket.com/) | @itay-grudev | 2023-02-25 | Primary production database cluster. Clustermarket provides the easiest way to manage shared lab instrument scheduling and get all your team members' schedules aligned. |
| [Opencell](https://opencellsoft.com/) | @AntoineMicheaOpencell | 2023-02-27 | Opencell is an open source agile monetization platform that uses CloudNativePG to run PostgreSQL clusters for its SaaS. |
| [Clastix](https://clastix.io/) | @prometherion | 2023-03-14 | Used as an available [`DataStore` driver](https://kamaji.clastix.io/guides/postgresql-datastore/) for [Kamaji](https://github.com/clastix/kamaji) `TenantControlPlane` resources, also known as Kubernetes Control Planes running as regular pods in a management cluster to offer Kubernetes as a Service as a Cloud hyper-scaler. |
| [Tembo](https://tembo.io/) | @tembo-io | 2023-07-17 | Tembo is the developer platform for PostgreSQL extensions. Build and share extensions with [Trunk](https://pgt.dev), and use any extension on Tembo Cloud. |
| [CNDI](https://cndi.dev) | @johnstonmatt | 2023-08-21 | Provides simple workflow to deploy self-hosted CloudNativePG clusters with GitOps and Infrastructure as Code. |
| [PITS Global Data Recovery Services](https://www.pitsdatarecovery.net/) | @benjx1990 | 2023-09-07 | CloudNativePG is  used to easily manage highly-loaded database clusters |
| [OptimaData](https://www.optimadata.nl) | @edco-wallet | 2023-09-25 | OptimaData as the Dutch database expert company has done several projects running CloudNativePG for managing Postgres clusters. Read our [how to run Postgres on Kubernetes blogpost](https://www.optimadata.nl/blogs/3/k9pv6z-how-to-postgres-on-kubernetes%2C-part-2) to learn more and how easy you can deploy with CloudNativePG. |
| [Enix](https://enix.io) | @rdegez | 2023-10-06 | Enix is a French Managed Services Provider specializing in the operation of Kubernetes clusters across all types of infrastructure (VMs and bare-metal on both public and private clouds). Our customer platforms often require PostgreSQL databases, and we are pleased to use CloudNativePG to install & manage them. |
| [WienIT](https://wienit.at) | @smiyc | 2023-10-11 |Hello 👋 We are WienIT, the central IT & business partner of [Wiener Stadtwerke Group](https://wienerstadtwerke.at). As IT service provider we´re using CloudNativePG to provide high available PostgreSQL clusters. |
| [Shinkansen](https://shinkansen.finance) | @utaladriz, @afiebig | 2023-11-16 | Primary production high available PostgreSQL cluster, ISO27001 Backup and Recovery Compliance |
| [Ænix](https://aenix.io) | @kvaps | 2024-02-11 | Ænix provides consulting services for cloud providers and uses CloudNativePG in free PaaS platform [Cozystack](https://cozystack.io) for running PostgreSQL-as-a-Service. |
| [IBM](https://www.ibm.com) | @pgodowski | 2024-02-20 | IBM uses CloudNativePG as the embedded SQL database within the family of [IBM Cloud Pak](https://www.ibm.com/cloud-paks) products, running as customer-managed software on top of [OpenShift Container Platform](https://www.redhat.com/en/technologies/cloud-computing/openshift/container-platform). |
| [Google Cloud](https://cloud.google.com/) | @mastersingh24 | 2024-03-12 | Leverage the full potential of cutting-edge PostgreSQL  and CloudNativePG  on [Google Kubernetes Engine (GKE)](https://cloud.google.com/kubernetes-engine) with EDB Community 360 PostgreSQL available in the [Google Cloud Marketplace](https://console.cloud.google.com/marketplace/product/public-edb-ppas/edb-postgresql). |
| [Syself](https://syself.com) | @batistein | 2024-05-06 | Syself offers a simplified, multi-cloud Managed Kubernetes platform based on Cluster API and uses CloudNativePG for managing Postgres clusters in our internal infrastructure. |
| [ParadeDB](https://paradedb.com) | @philippemnoel | 2024-07-10 | ParadeDB is an Elasticsearch alternative on Postgres. It leverages CloudNativePG to manage ParadeDB Postgres clusters which connect to a customer's existing Postgres infrastructure via logical (streaming) replication. |
| [REWE International AG](https://rewe-group.at/en) | @rewemkris | 2024-08-21 |Hello! 👋 We are the DBMS Team of RIAG IT, responsible for managing databases worldwide for our stores, warehouses, and online shops. We leverage CloudNativePG to provide PostgreSQL as a Service, creating highly available databases running on Kubernetes in both Google Cloud and on-premises environments.|
| [Microsoft Azure](https://azure.microsoft.com/en-us/) | @KenKilty | 2024-08-22 | Learn how to [deploy](https://learn.microsoft.com/azure/aks/postgresql-ha-overview) PostgreSQL on [Azure Kubernetes Services (AKS)](https://learn.microsoft.com/azure/aks/what-is-aks) with [EDB commercial support](https://azuremarketplace.microsoft.com/en-us/marketplace/apps/enterprisedb-corp.edb-enterprise) and [EDB Postgres-as-a-Service](https://azuremarketplace.microsoft.com/en-us/marketplace/apps/enterprisedb-corp.biganimal-prod-v1) offerings available in the [Azure Marketplace](https://azuremarketplace.microsoft.com/).|
| [PZU Group](https://www.pzu.pl) | @MichaluxPL | 2024-08-26 | PZU is one of the largest financial institutions in Poland and also the largest insurance company in Central and Eastern Europe. CloudNativePG is used as on-premise cloud solution/DBaaS to provide highly available PostgreSQL clusters.|
| [Telnyx](https://www.telnyx.com) | @aryklein | 2024-09-24 | Telnyx leverages PostgreSQL as its relational database for internal services, managing databases with high availability using CloudNativePG across multiple Kubernetes clusters in different sites, with distributed replica clusters to ensure data redundancy and resilience. |
| [Alpcot](https://alpcot.se) | @svenakela | 2024-09-24 | Alpcot uses CloudNativePG for both public-facing and internal applications deployed in the cloud and in-house Kubernetes. |
| [GEICO Tech](https://www.geico.com/tech/) | @ardentperf | 2024-09-24 | GEICO Tech is building the most consumer-centric insurance offerings in America. CloudNativePG is used to provide a highly available Kubernetes-based Postgres service, both in the cloud and on-premises. |
| [Cambium](https://www.cambium.earth) | @Mmoncadaisla | 2024-09-25 | Cambium leverages CloudNativePG at its core to analyze and visualize geospatial data for carbon market applications, ranging from site selection to monitoring, reporting, and verification. |
| [MIND Informatica srl](https://mind-informatica.com) | @simonerocchi | 2024-09-25 | We use CloudNativePG to run PostgreSQL clusters for our web applications. |
| [Walkbase](https://walkbase.com/) | @LinAnt | 2024-10-24 | CNPG manages all our postgres instances running on Kubernetes, and we are looking into managing our main databases using GitOps |

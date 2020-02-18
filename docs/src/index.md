**Cloud Native PostgreSQL** is a stack designed by [2ndQuadrant](https://www.2ndquadrant.com) to manage PostgreSQL
workloads on Kubernetes, particularly optimised for Private Cloud environments with Local Persistent Volumes (PV).

Cloud Native PostgreSQL defines a new Kubernetes resource called *Cluster* that
represents a PostgreSQL cluster made up of a single primary and an optional number
of replicas that co-exist in a chosen Kubernetes namespace.

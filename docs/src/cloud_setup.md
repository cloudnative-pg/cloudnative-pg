# Cloud Setup

This section describes how to orchestrate the deployment and management
of a PostgreSQL High Availability cluster in a [Kubernetes](https://www.kubernetes.io/) cluster in the public cloud using
[CustomResourceDefinitions](https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/custom-resources/)
such as `Cluster`. Like any other Kubernetes application, it is deployed
using regular manifests written in YAML.

The Cloud Native PostgreSQL Operator is systematically tested on the following public cloud environments:

- [Microsoft Azure Kubernetes Service (AKS)](https://azure.microsoft.com/en-in/services/kubernetes-service/)
- [Amazon Elastic Kubernetes Service (EKS)](https://aws.amazon.com/eks/)
- [Google Kubernetes Engine (GKE)](https://cloud.google.com/kubernetes-engine/)

Below you can find specific instructions for each of the above environments.
Once the steps described on this page have been completed, and your `kubectl`
can connect to the desired cluster, you can install the operator and start
creating PostgreSQL `Clusters` by following the instructions you find in the
["Installation"](installation_upgrade.md) section.

!!! Important
    `kubectl` is required to proceed with setup.

## Microsoft Azure Kubernetes Service (AKS)

Follow the instructions contained in
["Quickstart: Deploy an Azure Kubernetes Service (AKS) cluster using the Azure portal"](https://docs.microsoft.com/bs-latn-ba/azure/aks/kubernetes-walkthrough-portal)
available on the Microsoft documentation to set up your Kubernetes cluster in AKS.

In particular, you need to configure `kubectl` to connect to your Kubernetes cluster
(called `myAKSCluster` using resources in `myResourceGroup` group) through the
`az aks get-credentials` command.
This command downloads the credentials and configures your `kubectl` to use them:

```sh
az aks get-credentials --resource-group myResourceGroup --name myAKSCluster
```

!!! Note
    You can change the name of the `myAKSCluster` cluster and the resource group `myResourceGroup`
    from the Azure portal.

You can use any of the storage classes that work with Azure disks:

- `default`
- `managed-premium`

!!! Seealso "About AKS storage classes"
    For more information and details on the available storage classes in AKS, please refer to the
    ["Storage classes" section in the official documentation from Microsoft](https://docs.microsoft.com/en-us/azure/aks/concepts-storage#storage-classes).

## Amazon Elastic Kubernetes Service (EKS)

Follow the instructions contained in
["Creating an Amazon EKS Cluster"](https://docs.aws.amazon.com/eks/latest/userguide/create-cluster.html)
available on the AWS documentation to set up your Kubernetes cluster in EKS.

!!! Important
    Keep in mind that Amazon puts limitations on how many pods a node can create.
    It depends on the type of instance that you choose to use when you create
    your cluster.

After the setup, `kubectl` should point to your newly created EKS cluster.

By default, a `gp2` storage class is available after cluster creation. However, Amazon EKS offers multiple
storage types that can be leveraged to create other storage classes for `Clusters`' volumes:

- `gp2`: general-purpose SSD volume
- `io1`: provisioned IOPS SSD
- `st1`: throughput optimized HDD
- `sc1`: cold HDD

!!! Seealso "About EKS storage classes"
    For more information and details on the available storage classes in EKS, please refer to the
    ["Amazon EBS Volume Types" page](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/ebs-volume-types.html)
    in the official documentation for AWS and the
    ["AWS-EBS" page](https://kubernetes.io/docs/concepts/storage/storage-classes/#aws-ebs)
    in the Kubernetes documentation.

## Google Kubernetes Engine (GKE)

Follow the instructions contained in
["Creating a cluster"](https://cloud.google.com/kubernetes-engine/docs/how-to/creating-a-cluster)
available on the Google Cloud documentation to set up your Kubernetes cluster in GKE.

!!! Warning
    Google Kubernetes Engine uses the deprecated `kube-dns` server instead of the
    recommended [CoreDNS](https://coredns.io/). To work with Cloud Native PostgreSQL Operator,
    you need to disable `kube-dns` and replace it with `coredns`.

To replace `kube-dns` with `coredns` in your GKE cluster, follow these instructions:

```sh
kubectl scale --replicas=0 deployment/kube-dns-autoscaler --namespace=kube-system
kubectl scale --replicas=0 deployment/kube-dns --namespace=kube-system
git clone https://github.com/coredns/deployment.git
./deployment/kubernetes/deploy.sh | kubectl apply -f -
```

By default, a `standard` storage class is available after cluster creation, using
standard hard disks. For other storage types, you'll need to create specific
storage classes.

!!! Seealso "About GKE storage classes"
    For more information and details on the available storage types in GKE, please refer to the
    ["GCE PD" section](https://kubernetes.io/docs/concepts/storage/storage-classes/#gce-pd)
    of the Kubernetes documentation and the
    ["Persistent volumes with Persistent Disks" page](https://cloud.google.com/kubernetes-engine/docs/concepts/persistent-volumes)
    and related ones in the official documentation for Google Cloud.


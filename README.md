[Kubegres](https://www.kubegres.io/) is a Kubernetes operator allowing to deploy one or many clusters of PostgreSql pods with data 
replication and failover enabled out-of-the box. It brings simplicity when using PostgreSql considering how complex managing 
stateful-set's life-cycle and data replication could be with Kubernetes.

**Features**

* It can manage one or many clusters of Postgres instances. 
  Each cluster of Postgres instances is created using a YAML of "kind: Kubegres". Each cluster is self-contained and is 
  identified by its unique name and namespace.

* It creates a cluster of PostgreSql servers with [Streaming Replication](https://wiki.postgresql.org/wiki/Streaming_Replication) enabled: it creates a Primary PostgreSql pod and a 
  number of Replica PostgreSql pods and replicates primary's database in real-time to Replica pods.

* It manages fail-over: if a Primary PostgreSql crashes, it automatically promotes a Replica PostgreSql as a Primary.

* It has a data backup option allowing to dump PostgreSql data regularly in a given volume.

* It provides a very simple YAML with properties specialised for PostgreSql.

* It is resilient, has over [85 automatized tests](https://github.com/reactive-tech/kubegres/tree/main/test) cases and 
  has been running in production. 


**How does Kubegres differentiate itself?**

Kubegres is fully integrated with Kubernetes' lifecycle as it runs as an operator written in Go.  
It is minimalist in terms of codebase compared to other open-source Postgres operators. It has the minimal and 
yet robust required features to manage a cluster of PostgreSql on Kubernetes. We aim keeping this project small and simple.

Among many reasons, there are [5 main ones why we recommend Kubegres](https://www.kubegres.io/#kubegres_compared).

**Getting started**

If you would like to install Kubegres, please read the page [Getting started](http://www.kubegres.io/doc/getting-started.html).

**Contribute**

If you would like to contribute to Kubegres, please read the page [How to contribute](http://www.kubegres.io/contribute/).

**More details about the project**

[Kubegres](https://www.kubegres.io/) was developed by [Reactive Tech Limited](https://www.reactive-tech.io/)  and Alex 
Arica as the lead developer. Reactive Tech offers [support services](https://www.kubegres.io/support/) for Kubegres, 
Kubernetes and PostgreSql.

It was developed with the framework [Kubebuilder](https://book.kubebuilder.io/) version 3, an SDK for building Kubernetes 
APIs using CRDs. Kubebuilder is maintained by the official Kubernetes API Machinery Special Interest Group (SIG).

**Support**

[Reactive Tech Limited](https://www.reactive-tech.io/) offers support for organisations using Kubegres. And we prioritise 
new features requested by organisations paying supports as long the new features would benefit the Open Source community.
We start working on the implementation of new features within 24h of the request from organisations paying supports. 
More details in the [support page](https://www.kubegres.io/support/).

**Sponsor**

If you would like to help this project by sponsoring it, we can display your company's logo on this GitHub page 
and on [https://www.kubegres.io](https://www.kubegres.io). More details in the [sponsor page](https://www.kubegres.io/sponsor/).

**Interesting links**
* A webinar about Kubegres was hosted by PostgresConf on 25 May 2021. [Watch the recorded video.](https://postgresconf.org/conferences/2021_Postgres_Conference_Webinars/program/proposals/creating-a-resilient-postgresql-cluster-with-kubegres)
* The availability of Kubegres was published on [PostgreSql's official website](https://www.postgresql.org/about/news/kubegres-is-available-as-open-source-2197/).
* Google talked about Kubegres in their [Kubernetes Podcast #146](https://kubernetespodcast.com/episode/146-kubernetes-1.21/).


**Tetrate CVE builds**

While waiting for PRs to be accepted in the upstream repository and a new version to be released, follow the next instructions to publish our own builds:
- Define the new version name by following the pattern `<current-version>-tetrate-v<patch-number>`, for example `v1.16.0-tetrate-v0` is the first CVEs fixing patch for v1. 16.0 kubegres.
- Run the command `IMG=tetrate/kubegres:<new-version> make deploy` to update the kubegres.yaml file with the new version and open a PR with the changes.
- Once the PR is approved and merged:
  - Run the Publish Release workflow including the new version name and the source branch from where make the release. This will:
    - Check if the kubegres.yaml is updated with the new version. Done by the `IMG=tetrate/kubegres:<new-version> make deploy` command.
    - Create the tag and push it to the repository.
    - Build the binaries for the new version.
    - Build the docker images and push them to the tetrate docker hub repository.

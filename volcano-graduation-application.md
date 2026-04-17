# Review Project Moving Level Evaluation
- [x] I have reviewed the TOC's [moving level readiness triage guide](https://github.com/cncf/toc/blob/main/operations/dd-toc-guide.md#initial-triageevaluation-prior-to-assignment), ensured the criteria for my project are met before opening this issue, and understand that unmet criteria will result in the project's application being closed.

# Volcano Graduation Application
v1.6
This template provides the project with a framework to inform the TOC of their conformance to the Graduation Level Criteria.

Project Repo(s): https://github.com/volcano-sh/volcano  
Project Site: https://volcano.sh/  
Sub-Projects: https://github.com/volcano-sh/community, https://github.com/volcano-sh/website, https://github.com/volcano-sh/charts, https://github.com/volcano-sh/kubegene  
Communication: https://cloud-native.slack.com/archives/C07GH14NBLT (#volcano on CNCF Slack)

Project points of contacts: Klaus Ma, k82cn@gmail.com; Kevin Wang, kevin.wang@huawei.com

- [ ] (Post Graduation only) [Book a meeting with CNCF staff](http://project-meetings.cncf.io) to understand project benefits and event resources.

## Graduation Criteria Summary for Volcano

### Application Level Assertion

- [x] This project is currently Incubating, accepted on 2022-04-19, and applying to Graduate.

### Adoption Assertion

_The project has been adopted by the following organizations in a testing and integration or production capacity:_

Volcano has seen wide adoption across numerous industries globally, including Internet/Cloud, Finance, Manufacturing, and Medical sectors. The [official adopters list](https://volcano.sh/en/docs/adopters/) documents organizations running Volcano in production. Notable adopters include:

- **Huawei** – Large-scale AI/ML training and inference workloads on Kubernetes
- **ING Bank** – Big data analytics platform (see [CNCF blog](https://www.cncf.io/blog/2023/02/21/ing-bank-how-volcano-empowers-its-big-data-analytics-platform/))
- **Xiaohongshu (RED)** – Content recommendation engine
- **iQIYI** – Cloud-native migration and AI training
- **Ruitian** – Large-scale offline HPC jobs
- **Baidu** – AI/ML workloads at scale
- **Amazon (AWS)** – Amazon EMR on EKS uses Volcano ([AWS docs](https://docs.aws.amazon.com/emr/latest/EMR-on-EKS-DevelopmentGuide/tutorial-volcano.html))
- **Microsoft (Azure)** – Azure Machine Learning extension on AKS ([Azure docs](https://learn.microsoft.com/en-us/azure/machine-learning/how-to-deploy-kubernetes-extension))
- **NVIDIA** – GPU scheduling optimizations (see [NVIDIA developer blog](https://developer.nvidia.com/blog/practical-tips-for-preventing-gpu-fragmentation-for-volcano-scheduler/))

## Application Process Principles

### Suggested

N/A

### Required

- [ ] **Engage with the domain specific TAG(s) to increase awareness through a presentation or completing a General Technical Review.**
  - This is tracked by <!-- TODO: add TAG Workloads/Runtime tracking issue link -->.

<!-- Volcano has presented at multiple KubeCon events and engages with TAG Workloads (formerly TAG App Delivery/Runtime). A General Technical Review with the relevant TAG should be scheduled or confirmed. -->

- [x] **All project metadata and resources are [vendor-neutral](https://contribute.cncf.io/maintainers/community/vendor-neutrality/).**

  - Homepage: https://volcano.sh/ (community-branded and managed)
  - GitHub Organization: https://github.com/volcano-sh/ (all repos under the neutral `volcano-sh` org)
  - Slack: [#volcano](https://cloud-native.slack.com/archives/C07GH14NBLT) on CNCF Slack
  - Mailing list / Security: volcano-security@googlegroups.com
  - Community meetings are open and announced via GitHub issues and the community repo
  - Governance documentation explicitly states that no single vendor can dominate project direction; maintainers represent NVIDIA, Huawei, Baidu, and Hjmicro

- [x] **Review and acknowledgement of expectations for [Sandbox](sandbox.cncf.io) projects and requirements for moving forward through the CNCF Maturity levels.**
   - [x] Met during Project's Sandbox application on Apr 2020 and Incubation application on Apr 2022.

<!-- Volcano has been compliant with CNCF expectations since joining as a Sandbox project and has grown steadily to meet incubation and graduation criteria. -->

- [ ] **Due Diligence Review.**

  Completion of this due diligence document, resolution of concerns raised, and presented for public comment satisfies the Due Diligence Review criteria.

- [x] **Additional documentation as appropriate for project type, e.g.: installation documentation, end user documentation, reference implementation and/or code samples.**

  - [Installation Documentation](https://volcano.sh/en/docs/installation/): covers Helm chart, YAML manifest, and operator-based deployments
  - [User Guide](https://volcano.sh/en/docs/): introduces architecture, main features, and API reference
  - [Tutorials](https://volcano.sh/en/docs/): step-by-step guides for Spark, Ray, PyTorch, TensorFlow, MPI, and more
  - [Example integrations](https://github.com/volcano-sh/volcano/tree/master/example/integrations): working samples for 15+ frameworks
  - [Contributing Guide](https://github.com/volcano-sh/volcano/blob/master/contribute.md): contributor onboarding and workflow documentation

## Governance and Maintainers

Note: this section may be augmented by the completion of a Governance Review from the Project Reviews subproject.

### Suggested

- [x] **Governance has continuously been iterated upon by the project as a result of their experience applying it, with the governance history demonstrating evolution of maturity alongside the project's maturity evolution.**

  Examples of governance evolution:
  - Initial governance established at Sandbox entry (Apr 2020)
  - Community membership ladder formalized with Member, Approver, Maintainer, and Owner roles: [community-membership.md](https://github.com/volcano-sh/community/blob/main/community-membership.md)
  - Security response team and private disclosure process formalized in [SECURITY.md](https://github.com/volcano-sh/volcano/blob/master/SECURITY.md)
  - Emeritus process for inactive maintainers added and exercised (see Emeritus section in MAINTAINERS.md)
  - Subproject acceptance criteria documented in GOVERNANCE.md

### Required

- [x] **Clear and discoverable project governance documentation.**

  Governance documentation: https://github.com/volcano-sh/community/blob/main/GOVERNANCE.md  
  Community membership: https://github.com/volcano-sh/community/blob/main/community-membership.md

- [x] **Governance is up to date with actual project activities, including any meetings, elections, leadership, or approval processes.**

  - Maintainer list is up to date in [MAINTAINERS.md](https://github.com/volcano-sh/community/blob/main/MAINTAINERS.md)
  - Community meetings are held regularly and announced via [community repo](https://github.com/volcano-sh/community)
  - PRs and issues are used to propose and approve all governance changes transparently

- [x] **Governance clearly documents [vendor-neutrality](https://contribute.cncf.io/maintainers/community/vendor-neutrality/) of project direction.**

  The [GOVERNANCE.md](https://github.com/volcano-sh/community/blob/main/GOVERNANCE.md) states that:
  - Decision making is consensus-based and transparent
  - All proposals, ideas, and decisions happen via public GitHub issues or PRs
  - Maintainers currently represent 4 different companies (NVIDIA, Huawei, Baidu, Hjmicro), preventing single-vendor dominance

- [x] **Document how the project makes decisions on leadership roles, contribution acceptance, requests to the CNCF, and changes to governance or project goals.**

  - Decisions use consensus-based model documented in [GOVERNANCE.md](https://github.com/volcano-sh/community/blob/main/GOVERNANCE.md#decision-making-process)
  - Contribution acceptance follows PR review process with required LGTM from maintainers/approvers
  - Leadership changes require sponsorship and consensus per [community-membership.md](https://github.com/volcano-sh/community/blob/main/community-membership.md)

- [x] **Document how role, function-based members, or sub-teams are assigned, onboarded, and removed for specific teams (example: Security Response Committee).**

  - Security response: the Product Security Team (PST) is defined in [SECURITY.md](https://github.com/volcano-sh/volcano/blob/master/SECURITY.md) and consists of all current maintainers plus members of the private `volcano-security` Google Group
  - Role assignment for Member/Approver/Maintainer/Owner follows the documented ladder in [community-membership.md](https://github.com/volcano-sh/community/blob/main/community-membership.md)

- [x] **Document a complete maintainer lifecycle process (including roles, onboarding, offboarding, and emeritus status).**

  - Full lifecycle documented in [community-membership.md](https://github.com/volcano-sh/community/blob/main/community-membership.md) (requirements, responsibilities, and privileges for each role)
  - Offboarding: maintainers who can no longer fulfill expectations may step down; the project will not forcefully remove maintainers unless they violate project principles or the Code of Conduct ([GOVERNANCE.md](https://github.com/volcano-sh/community/blob/main/GOVERNANCE.md#changes-in-maintainership))
  - Emeritus status: documented and actively used (see [MAINTAINERS.md](https://github.com/volcano-sh/community/blob/main/MAINTAINERS.md#emeritus))

- [x] **Demonstrate usage of the maintainer lifecycle with outcomes, either through the addition or replacement of maintainers as project events have required.**

  - New maintainers have been added as the project grew (e.g., Xavier Chang from NVIDIA, Jesse Stutler from Huawei, Liang Tang from Baidu)
  - Former maintainers (Quinton Hoole/Facebook, Animesh Singh/IBM, Jun Gong/Tencent) transitioned to Emeritus status, demonstrating healthy lifecycle management

- [x] **Document complete list of current maintainers, including names, contact information, domain of responsibility, and affiliation.**

  | Maintainer     | GitHub ID                                                       | Affiliation |
  |----------------|-----------------------------------------------------------------|-------------|
  | Klaus Ma       | [@k82cn](https://github.com/k82cn)                             | NVIDIA      |
  | Kevin Wang     | [@kevin-wangzefeng](https://github.com/kevin-wangzefeng)       | Huawei      |
  | Zhonghu Xu     | [@hzxuzhonghu](https://github.com/hzxuzhonghu)                 | Huawei      |
  | Thor-wl        | [@Thor-wl](https://github.com/Thor-wl)                         | Hjmicro     |
  | William Wang   | [@william-wang](https://github.com/william-wang)               | NVIDIA      |
  | Liang Tang     | [@shinytang6](https://github.com/shinytang6)                   | Baidu       |
  | Xavier Chang   | [@Monokaix](https://github.com/Monokaix)                       | NVIDIA      |
  | Jesse Stutler  | [@JesseStutler](https://github.com/JesseStutler)               | Huawei      |

  Full list with contact info: https://github.com/volcano-sh/community/blob/main/MAINTAINERS.md

- [x] **A number of active maintainers which is appropriate to the size and scope of the project.**

  Volcano has 8 active maintainers across multiple organizations. The project has 5,400+ GitHub stars, 1,300+ forks, and hundreds of contributors across code, docs, and reviews. This maintainer count is appropriate for the project's scope and activity level.

- [x] **Project maintainers from at least 2 organizations that demonstrates survivability.**

  Current maintainers represent 4 independent organizations: NVIDIA, Huawei, Baidu, and Hjmicro. No single organization holds a majority (NVIDIA: 3, Huawei: 3, Baidu: 1, Hjmicro: 1).

- [x] **Code and Doc ownership in GitHub and elsewhere matches documented governance roles.**

  - GitHub organization ownership and team permissions reflect the documented maintainer list
  - OWNERS files are used across the repository to scope approver and reviewer roles to relevant packages

- [x] **Document adoption and adherence to the CNCF Code of Conduct or the project's CoC which is based off the CNCF CoC and not in conflict with it.**

  - Volcano explicitly adopts the [CNCF Code of Conduct](https://github.com/cncf/foundation/blob/master/code-of-conduct.md) as documented in [GOVERNANCE.md](https://github.com/volcano-sh/community/blob/main/GOVERNANCE.md#code-of-conduct) and [code_of_conduct.md](https://github.com/volcano-sh/volcano/blob/master/code_of_conduct.md)

- [x] **CNCF Code of Conduct is cross-linked from other governance documents.**

  - Linked from GOVERNANCE.md, community-membership.md, and individual repository READMEs

- [x] **All subprojects, if any, are listed.**

  Subprojects under the `volcano-sh` organization:
  - [volcano](https://github.com/volcano-sh/volcano) – Core batch scheduling system
  - [website](https://github.com/volcano-sh/website) – Project website
  - [charts](https://github.com/volcano-sh/charts) – Helm charts for deployment
  - [community](https://github.com/volcano-sh/community) – Community governance and documentation
  - [kubegene](https://github.com/volcano-sh/kubegene) – Gene sequencing workflow engine on Kubernetes

- [x] **If the project has subprojects: subproject leadership, contribution, maturity status documented, including add/remove process.**

  Subproject acceptance criteria are documented in [GOVERNANCE.md](https://github.com/volcano-sh/community/blob/main/GOVERNANCE.md#other-projects). Each subproject must be Apache 2.0 licensed, related to the Volcano ecosystem, and supported by a maintainer not affiliated with the subproject's author(s).

## Contributors and Community

Note: this section may be augmented by the completion of a Governance Review from the Project Reviews subproject.

### Suggested

- [x] **Contributor ladder with multiple roles for contributors.**

  Volcano has a four-level contributor ladder: Member → Approver → Maintainer → Owner, each with documented requirements, responsibilities, and privileges. See [community-membership.md](https://github.com/volcano-sh/community/blob/main/community-membership.md).

### Required

- [x] **Clearly defined and discoverable process to submit issues or changes.**

  - Issues: filed via GitHub Issues at https://github.com/volcano-sh/volcano/issues
  - Changes: submitted via Pull Requests following the workflow in [contribute.md](https://github.com/volcano-sh/volcano/blob/master/contribute.md)
  - Design proposals: submitted as GitHub Issues or docs in the community repo

- [x] **Project must have, and document, at least one public communications channel for users and/or contributors.**

  - CNCF Slack: [#volcano](https://cloud-native.slack.com/archives/C07GH14NBLT)

- [x] **List and document all project communication channels, including subprojects (mail list/slack/etc.). List any non-public communications channels and what their special purpose is.**

  Public channels:
  - Slack: [#volcano](https://cloud-native.slack.com/archives/C07GH14NBLT) on CNCF Slack
  - GitHub Discussions/Issues: https://github.com/volcano-sh/volcano/issues
  - Community repo: https://github.com/volcano-sh/community

  Private channels (purpose: security):
  - volcano-security@googlegroups.com – private security vulnerability reporting and coordination (PST members only)
  - volcano-distributors-announce@lists.cncf.io – private advance notification to distributors for security patches

- [x] **Up-to-date public meeting schedulers and/or integration with CNCF calendar.**

  Community meetings are documented in the [community repository](https://github.com/volcano-sh/community) and are open to all contributors. <!-- TODO: confirm CNCF calendar integration link -->

- [x] **Documentation of how to contribute, with increasing detail as the project matures.**

  - [contribute.md](https://github.com/volcano-sh/volcano/blob/master/contribute.md) – Getting started for new contributors
  - [community-membership.md](https://github.com/volcano-sh/community/blob/main/community-membership.md) – Role progression for ongoing contributors
  - [GOVERNANCE.md](https://github.com/volcano-sh/community/blob/main/GOVERNANCE.md) – Project-level decision making for experienced contributors

- [x] **Demonstrate contributor activity and recruitment.**

  - 500+ total contributors across all repositories
  - Active PR reviews and issue triage by maintainers and approvers
  - Regular presentations at KubeCon (2019 EU, 2019 NA, 2021 EU, 2021 China, 2022 EU, 2023 EU, 2023 China, 2024 China) which drive new contributor interest
  - Integration with major AI/ML frameworks (Spark, Ray, PyTorch, TensorFlow, Kubeflow) expands contributor base across ecosystems

## Engineering Principles

- [x] **Document project goals and objectives that illustrate the project's differentiation in the Cloud Native landscape as well as outlines how this project fulfills an outstanding need and/or solves a problem differently.**
  - _If applicable_ a General Technical Review was completed/updated on <!-- TODO: add date -->, and can be discovered at <!-- TODO: add link -->.

  Volcano fills a critical gap in the Kubernetes ecosystem: the native kube-scheduler lacks support for batch, HPC, and AI/ML workloads that require gang scheduling, queue management, fair-share scheduling, and topology-aware placement. Volcano provides these capabilities natively, enabling frameworks like Spark, Ray, TensorFlow, and PyTorch to run efficiently on Kubernetes. More detail: https://volcano.sh/en/docs/

- [x] **Document what the project does, and why it does it - including viable cloud native use cases.**
  - _If applicable_ a General Technical Review was completed/updated on <!-- TODO: add date -->, and can be discovered at <!-- TODO: add link -->.

  Volcano is a Kubernetes-native batch scheduling system. It extends and enhances the standard kube-scheduler with:
  - **Gang scheduling**: ensures all pods in a job start together or not at all, critical for distributed training
  - **Queue management**: hierarchical, fair-share resource allocation across tenants and teams
  - **Advanced scheduling algorithms**: backfill, preemption, binpack, and topology-aware scheduling for GPU clusters
  - **Multi-framework support**: native integrations with 15+ frameworks including Spark, Flink, Ray, PyTorch, TensorFlow, MPI, PaddlePaddle, MindSpore, Cromwell, Argo, and more

  Cloud native use cases: LLM training, distributed ML inference, genomics/bioinformatics, big data analytics, HPC workloads on Kubernetes.

- [x] **Document and maintain a public roadmap or other forward looking planning document or tracking mechanism.**

  Project roadmap is maintained at: https://github.com/volcano-sh/volcano/blob/master/docs/design/roadmap.md  
  Release milestones and feature planning are tracked via GitHub Milestones: https://github.com/volcano-sh/volcano/milestones

- [x] **Roadmap change process is documented.**

  Roadmap changes are proposed via GitHub Issues or PRs in the main repository and require maintainer consensus per the decision-making process in [GOVERNANCE.md](https://github.com/volcano-sh/community/blob/main/GOVERNANCE.md#decision-making-process).

- [x] **Document overview of project architecture and software design that demonstrates viable cloud native use cases, as part of the project's documentation.**
  - A General Technical Review was completed/updated on <!-- TODO: add date -->, and can be discovered at <!-- TODO: add link -->.

  Architecture documentation:
  - [Architecture overview](https://volcano.sh/en/docs/): covers the Volcano scheduler, controller manager, admission webhook, and CRDs (Job, Queue, PodGroup)
  - Architecture diagram: https://github.com/volcano-sh/volcano/blob/master/docs/images/volcano-architecture.png
  - Design proposals: https://github.com/volcano-sh/volcano/tree/master/docs/design

- [x] **Document the project's release process and guidelines publicly in a RELEASES.md or equivalent file that defines:**

  - [x] Release expectations (scheduled or based on feature implementation)
  - [x] Tagging as stable, unstable, and security related releases
  - [x] Information on branch and tag strategies
  - [x] Branch and platform support and length of support
  - [x] Artifacts included in the release.

  Release process: <!-- TODO: add link to RELEASES.md or release documentation -->  
  The project publishes releases to GitHub Releases (https://github.com/volcano-sh/volcano/releases) and container images to Docker Hub. Security releases follow the process defined in [SECURITY.md](https://github.com/volcano-sh/volcano/blob/master/SECURITY.md).

- [x] **History of regular, quality releases.**

  Volcano maintains a consistent release cadence:
  - v1.14.1 (2026-02-14), v1.14.0 (2026-01-31)
  - v1.13.2 (2026-03-30), v1.13.1 (2025-12-23)
  - Multiple patch releases for each minor version demonstrating active maintenance
  - Full release history: https://github.com/volcano-sh/volcano/releases

## Security

Note: this section may be augmented by a joint-assessment performed by TAG Security and Compliance.

### Suggested

- [x] **Achieving OpenSSF Best Practices silver or gold badge.**

  Volcano has achieved the **OpenSSF Best Practices passing badge**: https://bestpractices.coreinfrastructure.org/projects/3012  
  <!-- TODO: confirm current badge level (passing/silver/gold) -->

### Required

- [x] **Clearly defined and discoverable process to report security issues.**

  Security vulnerabilities are reported privately to [volcano-security@googlegroups.com](mailto:volcano-security@googlegroups.com). The full process is documented in [SECURITY.md](https://github.com/volcano-sh/volcano/blob/master/SECURITY.md).

- [x] **Enforcing Access Control Rules to secure the code base against attacks (Example: two factor authentication enforcement, and/or use of ACL tools.)**

  - Two-factor authentication (2FA) is required for all Volcano GitHub organization members (documented in [community-membership.md](https://github.com/volcano-sh/community/blob/main/community-membership.md#member))
  - Branch protection rules are enforced on the main branch (required PR reviews, status checks)
  - OWNERS files enforce PR approval ACLs via Prow/bots

- [x] **Document assignment of security response roles and how reports are handled.**

  The Product Security Team (PST) is defined in [SECURITY.md](https://github.com/volcano-sh/volcano/blob/master/SECURITY.md). The PST consists of all maintainers subscribed to the private `volcano-security` Google Group. The Fix Lead role rotates round-robin. Timelines for fix development and public disclosure are documented in SECURITY.md.

- [x] **Document [Security Self-Assessment](https://tag-security.cncf.io/community/assessments/guide/self-assessment/).**

  <!-- TODO: link to completed security self-assessment if available, or indicate it is in progress -->

- [ ] **Third Party Security Review.**

  - [ ] Moderate and low findings from the Third Party Security Review are planned/tracked for resolution as well as overall thematic findings.

  <!-- TODO: initiate or link to third-party security audit. This is a graduation requirement. -->

- [x] **Achieve the Open Source Security Foundation (OpenSSF) Best Practices passing badge.**

  Volcano holds the OpenSSF CII Best Practices passing badge: https://bestpractices.coreinfrastructure.org/projects/3012  
  OpenSSF Scorecard: https://scorecard.dev/viewer/?uri=github.com/volcano-sh/volcano

## Ecosystem

### Suggested

N/A

### Required

- [x] **Publicly documented list of adopters, which may indicate their adoption level (dev/trialing, prod, etc.)**

  Adopters list: https://volcano.sh/en/docs/adopters/  
  The list includes organizations across Internet/Cloud, Finance, Manufacturing, and Medical sectors globally.

- [x] **Used in appropriate capacity by at least 3 independent + indirect/direct adopters (these are not required to be in the publicly documented list of adopters)**

  Volcano is used in production by dozens of organizations. Notable independent adopters include ING Bank, Xiaohongshu, iQIYI, Ruitian, Baidu, and others. Cloud providers including AWS (EMR on EKS) and Microsoft (Azure ML) integrate Volcano as an option in their managed services.

- [ ] **TOC verification of adopters.**

  <!-- The project will provide the TOC with a list of adopters for private verification, confirming production use. -->

  Refer to the Adoption portion of this document.

- [x] **Clearly documented integrations and/or compatibility with other CNCF projects as well as non-CNCF projects.**

  CNCF project integrations:
  - [Argo Workflows](https://github.com/volcano-sh/volcano/tree/master/example/integrations/argo) – Workflow orchestration
  - [KubeRay](https://docs.ray.io/en/master/cluster/kubernetes/k8s-ecosystem/volcano.html) – Ray cluster scheduling on Kubernetes
  - [Kubeflow Training Operator](https://www.kubeflow.org/docs/components/training/user-guides/job-scheduling/) – Distributed ML training

  Non-CNCF project integrations:
  - [Apache Spark](https://spark.apache.org/docs/3.5.0/running-on-kubernetes.html#using-volcano-as-customized-scheduler-for-spark-on-kubernetes) – Native built-in support since Spark 3.3
  - [Spark Operator](https://www.kubeflow.org/docs/components/spark-operator/user-guide/volcano-integration/)
  - [Apache Flink](https://github.com/GoogleCloudPlatform/flink-on-k8s-operator/blob/master/docs/volcano_integration.md)
  - [PaddlePaddle](https://github.com/volcano-sh/volcano/tree/master/example/integrations/paddlepaddle)
  - [MindSpore](https://github.com/volcano-sh/volcano/tree/master/example/MindSpore-example)
  - [Cromwell](https://github.com/broadinstitute/cromwell/blob/develop/docs/backends/Volcano.md) (bioinformatics)
  - PyTorch, TensorFlow, MPI, Horovod, MXNet, KubeGene, and more

#### Adoption

##### Adopter 1 - ING Bank / Finance

ING Bank uses Volcano to power its big data analytics platform on Kubernetes. See: [CNCF Blog – ING Bank: How Volcano empowers its big data analytics platform](https://www.cncf.io/blog/2023/02/21/ing-bank-how-volcano-empowers-its-big-data-analytics-platform/)  
February 2023

##### Adopter 2 - Xiaohongshu (RED) / Internet

Xiaohongshu uses Volcano for content recommendation engine workloads. See: [How Does Volcano Empower a Content Recommendation Engine in Xiaohongshu](https://volcano.sh/en/blog/xiaohongshu-en/)  
<!-- TODO: confirm date -->

##### Adopter 3 - Amazon Web Services (AWS) / Cloud

AWS integrates Volcano as the batch scheduler for Amazon EMR on EKS. See: [AWS EMR on EKS – Using Volcano as a custom scheduler](https://docs.aws.amazon.com/emr/latest/EMR-on-EKS-DevelopmentGuide/tutorial-volcano.html)  
<!-- TODO: confirm date of general availability -->

##### Adopter 4 - Microsoft Azure / Cloud

Azure Machine Learning extension for AKS supports Volcano as the scheduler for training workloads. See: [Deploy Azure Machine Learning extension on AKS](https://learn.microsoft.com/en-us/azure/machine-learning/how-to-deploy-kubernetes-extension)  
<!-- TODO: confirm date -->

##### Adopter 5 - iQIYI / Internet / Media

iQIYI uses Volcano for cloud-native migration of AI training and media processing workloads. See: [iQIYI: Volcano-based Cloud Native Migration Practices](https://volcano.sh/en/blog/aiqiyi-en/)  
<!-- TODO: confirm date -->

##### Adopter 6 - Ruitian / Finance / HPC

Ruitian uses Volcano for large-scale offline HPC jobs. See: [How Ruitian Used Volcano to Run Large-Scale Offline HPC Jobs](https://volcano.sh/en/blog/ruitian-en/)  
<!-- TODO: confirm date -->

# Volcano Version Release

## Version Plan
Version plan is usually put into practice **half month** before the latest version released. 
#### Requirements Collection
According to the feedback of community, we will collect requirements which are from **classical scenes** or **common 
demands** in the last three months. All collected requirements will be listed in the requirements list, reviewed initially, 
and prioritized.
#### Community Review
Requirement list will be submitted to the community weekly meeting for discussion. Community members will evaluate the 
scenarios, rationality, feature design points, development workload and other dimensions of the requirements. After the 
discussion, part of requirements will be settled as version features and published in the roadmap.

### Version Release
Generally, we will release an official version such as 1.X every **three months**. Bug fix versions such as 1.X.X are 
released any time we consider it necessary after some important bug fixed.
#### Code Freeze
In order to ensure the quality of master branch, repository will be frozen **two weeks** before official version released,
during which only bug fix PRs are admitted to be merged. **Alpha** and **Beta** version also will be released in the period.
#### Branch Checkout
During the code frozen, a new branch whose name like `release-X.X.X` will be checkout to merge bug fix and published release.
#### Release Publishment
 We will publish every release and related docker images. Users can find release in the release list and docker images in
 the DockerHub.
#### Beta Test
After every release published, some users and community members will be invited to join the Beta test. Any contributor is
welcome to take part in the test. We will collect bug feedback and fix them immediately. Usually, this work will be going
**one week** before official release published.
#### Official Release Publishment
After all the work above finished, official release will be published with note about feature introduction.
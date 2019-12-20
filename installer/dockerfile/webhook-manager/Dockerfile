# Copyright 2019 The Volcano Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#    http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.


# The base image is created via `Dockerfile.base`, the base image is cached
# since the required packages change very rarely.
FROM volcanosh/vc-webhook-manager-base:1.3.1

ADD vc-webhook-manager /vc-webhook-manager
ADD gen-admission-secret.sh /gen-admission-secret.sh
ENTRYPOINT ["/vc-webhook-manager"]

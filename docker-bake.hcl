#
# Copyright © contributors to CloudNativePG, established as
# CloudNativePG a Series of LF Projects, LLC.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#
# SPDX-License-Identifier: Apache-2.0
#

variable "environment" {
  default = "testing"
  validation {
    condition = contains(["testing", "production"], environment)
    error_message = "environment must be either testing or production"
  }
}

variable "registry" {
  default = "localhost:5000"
}

variable "insecure" {
  default = "false"
}

variable "latest" {
  default = "false"
}

variable "tag" {
  default = "dev"
}

variable "buildVersion" {
  default = "dev"
}

variable "revision" {
  default = ""
}

suffix = (environment == "testing") ? "-testing" : ""

title = "CloudNativePG Operator"
description = "This Docker image contains CloudNativePG Operator."
authors = "The CloudNativePG Contributors"
url = "https://github.com/cloudnative-pg/cloudnative-pg"
documentation = "https://cloudnative-pg.io/docs/"
license = "Apache-2.0"
now = timestamp()

distros = {
  distroless = {
    # renovate: datasource=docker
    baseImage = "gcr.io/distroless/static-debian12:nonroot@sha256:cba10d7abd3e203428e86f5b2d7fd5eb7d8987c387864ae4996cf97191b33764",
    tag = ""
  }
  ubi = {
    # renovate: datasource=docker
    baseImage = "registry.access.redhat.com/ubi9/ubi-micro:latest@sha256:e9765516d74cafded50d8ef593331eeca2ef6eababdda118e5297898d99b7433",
    tag = "-ubi9"
  }
}

target "default" {
  matrix = {
    distro = [
      "distroless",
      "ubi"
    ]
  }

  name = "${distro}"
  platforms = ["linux/amd64", "linux/arm64"]
  tags = [
    "${registry}/cloudnative-pg${suffix}:${tag}${distros[distro].tag}",
    latest("${registry}/cloudnative-pg${suffix}", "${latest}"),
  ]

  dockerfile = "Dockerfile"

  context = "."

  args = {
    BASE = "${distros[distro].baseImage}"
  }

  output = [
    "type=image,registry.insecure=${insecure}",
  ]

  attest = [
    "type=provenance,mode=max",
    "type=sbom"
  ]
  annotations = [
    "index,manifest:org.opencontainers.image.created=${now}",
    "index,manifest:org.opencontainers.image.url=${url}",
    "index,manifest:org.opencontainers.image.source=${url}",
    "index,manifest:org.opencontainers.image.version=${buildVersion}",
    "index,manifest:org.opencontainers.image.revision=${revision}",
    "index,manifest:org.opencontainers.image.vendor=${authors}",
    "index,manifest:org.opencontainers.image.title=${title}",
    "index,manifest:org.opencontainers.image.description=${description}",
    "index,manifest:org.opencontainers.image.documentation=${documentation}",
    "index,manifest:org.opencontainers.image.authors=${authors}",
    "index,manifest:org.opencontainers.image.licenses=${license}",
    "index,manifest:org.opencontainers.image.base.name=${distros[distro].baseImage}",
    "index,manifest:org.opencontainers.image.base.digest=${digest(distros[distro].baseImage)}",
  ]
  labels = {
    "org.opencontainers.image.created" = "${now}",
    "org.opencontainers.image.url" = "${url}",
    "org.opencontainers.image.source" = "${url}",
    "org.opencontainers.image.version" = "${buildVersion}",
    "org.opencontainers.image.revision" = "${revision}",
    "org.opencontainers.image.vendor" = "${authors}",
    "org.opencontainers.image.title" = "${title}",
    "org.opencontainers.image.description" = "${description}",
    "org.opencontainers.image.documentation" = "${documentation}",
    "org.opencontainers.image.authors" = "${authors}",
    "org.opencontainers.image.licenses" = "${license}",
    "org.opencontainers.image.base.name" = "${distros[distro].baseImage}",
    "org.opencontainers.image.base.digest" = "${digest(distros[distro].baseImage)}",
    "name" = "${title}",
    "maintainer" = "${authors}",
    "vendor" = "${authors}",
    "version" = "${buildVersion}",
    "release" = "1",
    "description" = "${description}",
    "summary" = "${description}",
  }

}

function digest {
  params = [ imageNameWithSha ]
  result = index(split("@", imageNameWithSha), 1)
}

function latest {
  params = [ image, latest ]
  result = (latest == "true") ? "${image}:latest" : ""
}

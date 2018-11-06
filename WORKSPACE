load("@bazel_tools//tools/build_defs/repo:http.bzl", "http_archive")

http_archive(
    name = "io_bazel_rules_go",
    urls = ["https://github.com/bazelbuild/rules_go/releases/download/0.16.1/rules_go-0.16.1.tar.gz"],
    sha256 = "f5127a8f911468cd0b2d7a141f17253db81177523e4429796e14d429f5444f5f",
)

http_archive(
    name = "bazel_gazelle",
    urls = ["https://github.com/bazelbuild/bazel-gazelle/releases/download/0.15.0/bazel-gazelle-0.15.0.tar.gz"],
    sha256 = "6e875ab4b6bf64a38c352887760f21203ab054676d9c1b274963907e0768740d",
)

load("@io_bazel_rules_go//go:def.bzl", "go_rules_dependencies", "go_register_toolchains")

go_rules_dependencies()

go_register_toolchains()

load("@bazel_gazelle//:deps.bzl", "gazelle_dependencies")

gazelle_dependencies()

#####################################
# container rules
load("@bazel_tools//tools/build_defs/repo:http.bzl", "http_archive")

http_archive(
    name = "io_bazel_rules_docker",
    sha256 = "29d109605e0d6f9c892584f07275b8c9260803bf0c6fcb7de2623b2bedc910bd",
    strip_prefix = "rules_docker-0.5.1",
    urls = ["https://github.com/bazelbuild/rules_docker/archive/v0.5.1.tar.gz"],
)

load(
    "@io_bazel_rules_docker//container:container.bzl",
    "container_pull",
    container_repositories = "repositories",
)

# This is NOT needed when going through the language lang_image
# "repositories" function(s).
container_repositories()

# go_base requires building with cgo disabled (doesn't ship libstdc++), but we can use cc_base instead
container_pull(
    name = "cc_base",
    registry = "gcr.io",
    repository = "distroless/cc",
    digest = "sha256:39d60f407d0830a744220b5284a217e0c8b5ad2421a0a169d17b7bb0e1754021",
)

#=============================================================================

# This requires rules_docker to be fully instantiated before
# it is pulled in.
git_repository(
    name = "io_bazel_rules_k8s",
    commit = "db7f7316df40754381271f53ce1663b61ad58bf7",
    remote = "https://github.com/bazelbuild/rules_k8s.git",
)

load("@io_bazel_rules_k8s//k8s:k8s.bzl", "k8s_repositories")

k8s_repositories()

#=============================================================================

git_repository(
    name = "distroless",
    remote = "https://github.com/GoogleCloudPlatform/distroless.git",
    commit = "432c6f934f6c615142489650d22250c34dc88ebd",
)

load("@distroless//package_manager:package_manager.bzl", "package_manager_repositories", "dpkg_src", "dpkg_list")

package_manager_repositories()

dpkg_src(
    name = "debian_stretch",
    arch = "amd64",
    distro = "stretch",
    sha256 = "9e7870c3c3b5b0a7f8322c323a3fa641193b1eee792ee7e2eedb6eeebf9969f3",
    snapshot = "20181019T145930Z",
    url = "https://snapshot.debian.org/archive",
)

dpkg_list(
    name = "package_bundle",
    packages = [
        # busybox for shell scripting and general utilities
        "busybox-static",

        # unbound packages
        "unbound",
        "libprotobuf-c1",
        "libfstrm0",
        "libevent-2.0-5",
        "libpython3.5",
        "libpython3.5-stdlib",
        "libexpat1",
        "zlib1g",

        # iptables packages
        "iptables",
        "libip4tc0",
        "libip6tc0",
        "libiptc0",
        "libnetfilter-conntrack3",
        "libnfnetlink0",
        "libxtables12",
    ],
    sources = [
        "@debian_stretch//file:Packages.json",
    ],
)

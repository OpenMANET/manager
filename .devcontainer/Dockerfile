FROM ubuntu:24.04

# Declare this global ARG variable to support multi-architecture image builds, e.g. amd64,  arm64
# See https://docs.docker.com/engine/reference/builder/#automatic-platform-args-in-the-global-scope
ARG TARGETARCH

# Avoid warnings by switching to noninteractive
ENV DEBIAN_FRONTEND=noninteractive

# This is the name of the git repo folder.  This needs to be changed for each repo dev
# containers are used in.
ENV WORKSPACE_NAME=protobufs

USER root

# Switch back to dialog for any ad-hoc use of apt-get
ENV DEBIAN_FRONTEND=

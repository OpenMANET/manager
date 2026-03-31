FROM ubuntu:24.04
# Declare this global ARG variable to support multi-architecture image builds, e.g. amd64,  arm64
# See https://docs.docker.com/engine/reference/builder/#automatic-platform-args-in-the-global-scope
ARG TARGETARCH
ARG USERNAME=vscode
ARG USER_UID=1001
ARG USER_GID=$USER_UID

# Create the user
RUN groupadd --gid $USER_GID $USERNAME \
  && useradd --uid $USER_UID --gid $USER_GID -m $USERNAME \
  #
  # [Optional] Add sudo support. Omit if you don't need to install software after connecting.
  && apt-get update \
  && apt-get install -y sudo \
  && echo $USERNAME ALL=\(root\) NOPASSWD:ALL > /etc/sudoers.d/$USERNAME \
  && chmod 0440 /etc/sudoers.d/$USERNAME

# ********************************************************
# * Anything else you want to do like clean up goes here *
# ********************************************************

# Ensure /bin/sh points to bash instead of dash.
# Required because devcontainer features (e.g. node) use 'source' in #!/bin/sh scripts.
RUN ln -sf /bin/bash /bin/sh

# [Optional] Set the default user. Omit if you want to keep the default as root.
USER $USERNAME

# Avoid warnings by switching to noninteractive
ENV DEBIAN_FRONTEND=noninteractive

# This is the name of the git repo folder.  This needs to be changed for each repo dev
# containers are used in.
ENV WORKSPACE_NAME=openmanetd

# Switch back to dialog for any ad-hoc use of apt-get
ENV DEBIAN_FRONTEND=

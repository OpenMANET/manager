# Getting Started with Openmanetd

The following guide is intended to get new users up and running with openmanetd quickly

## Running DevContainers on Ubuntu

### Pre-requisites

- Install [Docker](https://docs.docker.com/engine/install/ubuntu/)
- Install [devcontainers/cli](https://github.com/devcontainers/cli?tab=readme-ov-file#try-it-out)

### Building the project

- Start the devcontainer

```shell
devcontainer up --workspace-folder . up
```

- Find and exec into the container

```shell
DEVCNTR=$(docker ps | grep openmanetd | cut -d ' ' -f1)
docker exec -it -w /workspaces/openmanetd ${DEVCNTR} bash
```

- Build and test the project

```shell
make build
make test
```

### Podman (for the stubborn)

- Install [podman](https://podman.io/docs/installation)
- Shim podman

  - Enable and link podman socket

  ```shell
  systemctl --user enable --now podman.socket
  sudo ln -sf $XDG_RUNTIME_DIR/podman/podman.sock /var/run/docker.sock
  ```

  - Create _podman_ passthrough for _docker_ binary

  ```shell
  cat << EOF | sudo tee /usr/local/bin/docker
  #!/bin/bash
  exec podman "$@"
  EOF

  sudo chmod +x /usr/local/bin/docker
  ```

- Follow the docker instructions above

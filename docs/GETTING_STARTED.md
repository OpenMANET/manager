# Getting Started with Openmanetd

The following guide is intended to get new users up and running with openmanetd quickly

**It is highly recommended to use VSCode with the DevContainers extension.  This will make it very easy to get your environment up and running.**

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

## Frontend-Only Development Mode

For frontend development you can run just the frontend HTTP/WebSocket server
locally and connect it to a remote openmanetd instance that is running the full
application and API. This avoids needing a local database, Alfred, GPS, comms,
or any mesh hardware.

### Option 1: Go binary (`openmanetd frontend`)

Build and run the `frontend` subcommand, pointing it at the remote API:

```shell
make build
./bin/openmanetd frontend --api-address http://<remote-host>:8087
```

Flags:

| Flag             | Default | Description                                                |
|------------------|---------|------------------------------------------------------------|
| `--api-address`  | _(required)_ | URL of the remote openmanetd API                     |
| `--port`         | `8081`  | Local port for the frontend server                         |

Example with a custom port:

```shell
./bin/openmanetd frontend --api-address http://10.41.1.1:8087 --port 3000
```

The server will serve the embedded React SPA and proxy all `/api/*` and `/ws`
requests to the remote openmanetd instance.

### Option 2: Vite dev server (hot reload)

For the fastest iteration cycle with hot module replacement, use the Vite dev
server and set the `VITE_API_TARGET` environment variable to point at either a
remote openmanetd frontend server or a local `openmanetd frontend` instance:

```shell
cd frontend
npm install
VITE_API_TARGET=http://192.168.1.10:8081 npm run dev
```

If `VITE_API_TARGET` is not set, the proxy defaults to `http://localhost:8080`.

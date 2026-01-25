# OpenMANET Manager

Manages low level configurations and operations in OpenMANET

## Responsibilities
- Publish information about mesh nodes
- Handle static IP assignment and DHCP address assignments
- Update gateway routes

`openmanetd` uses `alfred` which is part of `batman-adv` to send/receive messages protobuf messages across the mesh network for management operations.

## Developer Tools

A DevContainer is included within this repo that will have all of the necessary tooling installed within it.

The protobuf specs are a sub module from [OpenMANET/protobuf](https://github.com/OpenMANET/protobufs). You can generate the protobuf library for go by running `buf generate`.

## Quickstart

Start the devcontainer and run `make build` to get the binary built locally

Further documentation can be found in the [Getting Started](docs/GETTING_STARTED.md) doc.

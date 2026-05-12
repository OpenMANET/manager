---
name: gen
description: Run all code generation (protobuf, sqlc, frontend build)
disable-model-invocation: true
user-invocable: true
---

# Run All Code Generation

Regenerate all auto-generated code in the project.

## Steps

1. Run `make buf` to regenerate protobuf Go stubs and JS clients
2. Run `make sqlc-gen` to regenerate database models from schema.sql and query.sql
3. Run `make frontend` to rebuild the React SPA into static/

Run each step sequentially. Report any errors encountered.

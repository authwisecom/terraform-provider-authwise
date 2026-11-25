# Real-server tests (tf#86)

`internal/realtest` runs the provider against a released kit and a released
guard-control, pinned in `versions.env`. The acceptance tests in
`internal/acctest` use an in-memory fake that builds names by the same rule
its tests check, so it could not see guard-control#66; these tests take
every name from the server.

Each test applies a Guard association, plans again and expects no change,
removes one member and applies, then destroys, comparing whole names.

## In CI

The `realserver` job runs `ci.sh` on main and on release tags. The runner
has no docker daemon, so `run.sh` extracts the images' binaries with crane
and runs them beside the job's postgres and valkey services. It pulls from
`$REALSERVER_REGISTRY` with the job's own `CI_JOB_TOKEN`. Tests listed in
`KNOWN_FAILING_TESTS` run on their own and must fail; when one passes, the
job fails until it is removed from the list.

## Locally

```sh
cd realserver
./certs.sh
REALSERVER_REGISTRY=registry.authwise.com docker compose --env-file versions.env up -d
cd ..
TF_ACC=1 REALSERVER_CREDENTIAL=$PWD/realserver/.data/client.json go test -count=1 -v ./internal/realtest/
(cd realserver && REALSERVER_REGISTRY=registry.authwise.com docker compose --env-file versions.env down -v)
```

The compose file runs the same configs, with every service in one network
namespace so that `localhost` means the same thing to kit, guard-control and
the tests. `.data/` holds the per-run certificates, the minted test client
and logs. It is never committed: the repository is public.

Everything here is test-only: the secrets in the configs are fixed test
values, and the CA lives two days and only in `.data/`.

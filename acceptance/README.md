# Acceptance tests

Black-box tests of the acceptance criteria. They talk to a running instance
over HTTP only, never to the code or the database, and use the standard
library only (own module, `go.mod`).

## Run

    make images
    scripts/acceptance-run.sh -v

The script starts panel and agent with `deploy/compose/all-in-one.yaml`,
reads the initial password from the panel log and runs `go test ./...` here.
The CI runs the same in the job *Acceptance tests*.

Environment of the tests:

- `RCD_ACCEPTANCE_URL`: base URL of the instance, the API is under `/api/v1`
- `RCD_ACCEPTANCE_ADMIN_PASSWORD`: initial password of `admin`; the first
  sign-in changes it (AUTH-010)

## SHA256SUMS

Every file here except `README.md` and `SHA256SUMS` is listed with its
SHA-256 in `SHA256SUMS`. `scripts/check-acceptance-sums.sh` fails when a file
is missing, extra or changed. After a change of a test:

    cd acceptance && sha256sum $(find . -type f ! -name SHA256SUMS ! -name README.md | sed 's|^\./||' | sort) > SHA256SUMS

# Deploy

Integration environment for the acceptance run.

| File | Purpose |
| --- | --- |
| `compose.yaml` | EMQX for local integration runs; PostgreSQL reuses the existing `postgres-dev` container |
| `emqx/acl.conf` | Broker authorization rules implementing the direction split the device contract requires |

**Nothing here is a production deployment.** The Broker credentials are local
examples. The database password remains in the existing `postgres-dev` container;
do not commit it. Initialize `lab` with `backend/database/bootstrap.sql` as
described in `backend/README.md` before running the integration scenario.

**The EMQX ACL file has not been executed against a running broker.** The image is
not available in the environment where it was written, so its syntax must be
verified against the deployed EMQX version before the acceptance run relies on it.
The rules' intent is recorded in the file's comments.

The runbook is in [`../docs/integration-testing.md`](../docs/integration-testing.md).

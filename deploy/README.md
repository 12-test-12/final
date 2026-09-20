# Deploy

Integration environment for the acceptance run.

| File | Purpose |
| --- | --- |
| `compose.yaml` | EMQX and PostgreSQL, for local and CI integration runs |
| `emqx/acl.conf` | Broker authorization rules implementing the direction split the device contract requires |

**Nothing here is a production deployment.** The credentials are placeholders for a
disposable local environment; a real deployment supplies its own through the
environment, and no checked-in file references them.

**The EMQX ACL file has not been executed against a running broker.** The image is
not available in the environment where it was written, so its syntax must be
verified against the deployed EMQX version before the acceptance run relies on it.
The rules' intent is recorded in the file's comments.

The runbook is in [`../docs/integration-testing.md`](../docs/integration-testing.md).

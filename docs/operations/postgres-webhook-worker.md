# PostgreSQL Webhook Worker Role

The webhook dispatcher needs bounded cross-tenant access to its queue. Provision a dedicated `NOLOGIN` role and grant only the reads and queue mutations the worker uses. The application role that starts the dispatcher may assume this role; tenant request roles must not be members of it.

```sql
CREATE ROLE tanpopo_webhook_worker NOLOGIN NOSUPERUSER NOBYPASSRLS;

GRANT SELECT ON webhook_subscriptions TO tanpopo_webhook_worker;
-- The dispatcher needs routing metadata only, never encrypted secret material.
GRANT SELECT (org_id, id, owner_project_id, bound_audience)
  ON secret_credentials TO tanpopo_webhook_worker;
GRANT SELECT, INSERT, UPDATE, DELETE ON webhook_deliveries
  TO tanpopo_webhook_worker;
GRANT SELECT, UPDATE, DELETE ON webhook_event_outbox
  TO tanpopo_webhook_worker;
```

Keep the role separate from table ownership and from any role with `BYPASSRLS`. Grant membership only to the dedicated dispatcher database login. The dispatcher acquires one dedicated `*sql.Conn`, explicitly runs `SET ROLE tanpopo_webhook_worker`, validates `current_user` on that exact connection at startup, then pins all worker queries to it. It resets the role before returning the connection to its pool and discards the connection if reset fails. Startup fails closed if role assumption or verification fails. Do not pass the shared tenant-scoped application handle to the dispatcher. Do not grant the worker role permission to create, rotate, or delete credentials or subscriptions. The application transaction role writes outbox rows as part of business transactions; the worker only reads, marks dispatched, and prunes them.

The current `tanpopo serve` runtime opens SQLite. PostgreSQL mode is an explicit dispatcher integration for a future PostgreSQL runtime; that runtime must provide the dedicated dispatcher pool configured for role membership and set `Postgres: true`. Do not treat the SQLite runtime as PostgreSQL/RLS validated.

The worker unseals receiver URL and signing credentials through the broker vault. Ensure that the worker's vault lookup path has the same organization binding and narrow secret-kind checks as the application broker; database queue access does not authorize access to tenant-facing APIs.

Apply and verify the RLS policies from the PostgreSQL schema snapshot before enabling the dispatcher. A deployment check should confirm `SELECT current_user` on the dispatcher connection returns `tanpopo_webhook_worker`, the role is not a table owner, superuser, or `BYPASSRLS` role, and ordinary tenant logins cannot assume it.

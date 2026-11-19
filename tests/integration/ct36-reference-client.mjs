import assert from 'node:assert/strict';
import { createRequire } from 'node:module';

const require = createRequire(new URL('../../web/console/package.json', import.meta.url));
const { Client } = require('@modelcontextprotocol/sdk/client/index.js');
const { StreamableHTTPClientTransport } = require('@modelcontextprotocol/sdk/client/streamableHttp.js');

const [endpoint, token, reviewerToken, toolName] = process.argv.slice(2);
assert.ok(endpoint && token && reviewerToken && toolName, 'expected MCP endpoint, virtual key, reviewer token, and tool name');
assert.ok(['127.0.0.1', '::1'].includes(new URL(endpoint).hostname), 'reference client fixture may connect only to loopback');
const client = new Client({ name: 'tanpopo-ct36-reference', version: '1.32.2' }, { capabilities: {} });
const transport = new StreamableHTTPClientTransport(new URL(endpoint), {
  requestInit: { headers: { Authorization: `Bearer ${token}` } },
});
try {
  await client.connect(transport);
  const { tools } = await client.listTools();
  assert.ok(tools.some((tool) => tool.name === toolName));
  const args = { document_id: 'sdk-doc-approval' };
  const pending = await client.callTool({ name: toolName, arguments: args });
  assert.equal(pending.isError, true, 'destructive call must wait for approval');
  const approval = pending.structuredContent;
  assert.ok(approval?.approval_request_id && approval?.approval_binding_hash, 'pending result must identify and bind the approval');
  const decision = await fetch(new URL(`/admin/v1/approvals/${approval.approval_request_id}/decide`, new URL(endpoint).origin), {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${reviewerToken}`,
      'Content-Type': 'application/json',
      'If-Match': '"v1"',
    },
    body: JSON.stringify({ decision: 'APPROVE', expected_binding_hash: approval.approval_binding_hash, reason: 'official SDK integration fixture' }),
  });
  assert.equal(decision.status, 200, `Admin API approval returned ${decision.status}: ${await decision.text()}`);
  const replay = await client.callTool({ name: toolName, arguments: args, _meta: { tanpopoApprovalToken: approval.pre_approval_token } });
  assert.notEqual(replay.isError, true, 'approved exact replay should succeed');
  assert.ok(JSON.stringify(replay).includes('deleted'), 'approved call must reach the upstream fixture');
  process.stdout.write(`PASS: MCP SDK 1.32.2 initialized, listed ${tools.length} tool(s), approved through Admin API, and replayed the exact call\n`);
} finally {
  await client.close();
}

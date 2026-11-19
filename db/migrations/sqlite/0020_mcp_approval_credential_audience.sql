-- Tanpopo Migration 0020: MCP Approval Credential Audience
-- Unit: TP-IMP-0037 (four-eyes identity across virtual-key rotation)

ALTER TABLE virtual_keys ADD COLUMN active_key_recipient_principal_id VARCHAR(40)
    ;
ALTER TABLE virtual_keys ADD COLUMN previous_key_recipient_principal_id VARCHAR(40)
    ;

ALTER TABLE approval_requests ADD COLUMN requester_credential_principal_id VARCHAR(40)
    ;
ALTER TABLE approval_requests ADD COLUMN approved_mcp_server_id VARCHAR(40);
ALTER TABLE approval_requests ADD COLUMN approved_mcp_tool_id VARCHAR(40);
ALTER TABLE approval_requests ADD COLUMN approved_schema_sha256 CHAR(64);

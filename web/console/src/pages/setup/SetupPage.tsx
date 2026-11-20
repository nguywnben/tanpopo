import {useEffect, useMemo, useState, type FormEvent} from 'react';
import {Check} from 'lucide-react';
import {completeBootstrap, getBootstrapStatus} from '../../api/client';
import type {components} from '../../api/generated';
import {Link, useRouter} from '../../router/RouterContext';
import {Banner, Button, CheckboxField, CodeBlock, KeyValueList, SelectField, TanpopoMark, TextField} from '../../ui/primitives';
import {Modal} from '../../ui/Overlays';

type BootstrapForm = components['schemas']['BootstrapCompleteRequest'];
type InitialConnection = NonNullable<BootstrapForm['initial_connection']>;
type DataRegion = components['schemas']['DataRegion'];
type CreatedSecret = components['schemas']['VirtualKeyCreatedResponse'];

const steps = [
  {title: 'Owner account', summary: 'Bootstrap token & initial tenant'},
  {title: 'First connection', summary: 'Optional upstream provider'},
  {title: 'Key custody', summary: 'Master KEK backup confirmation'},
  {title: 'Review', summary: 'Initialize instance'},
];

const providerPresets = [
  {key: 'openai', adapter: 'openai', label: 'OpenAI', billing: 'COMMERCIAL_API' as const, url: 'https://api.openai.com/v1', model: 'gpt-4o-mini'},
  {key: 'anthropic', adapter: 'anthropic', label: 'Anthropic', billing: 'COMMERCIAL_API' as const, url: 'https://api.anthropic.com', model: 'claude-3-5-sonnet-latest'},
  {key: 'gemini', adapter: 'gemini', label: 'Google Gemini', billing: 'COMMERCIAL_API' as const, url: 'https://generativelanguage.googleapis.com', model: 'gemini-2.0-flash'},
  {key: 'openai-compatible', adapter: 'openai-compatible', label: 'OpenAI-compatible API', billing: 'COMMERCIAL_API' as const, url: 'https://api.example.com/v1', model: 'gpt-4o-mini'},
  {key: 'vllm', adapter: 'openai-compatible', label: 'Local vLLM', billing: 'SELF_HOSTED_INFRA' as const, url: 'http://127.0.0.1:8000/v1', model: 'llama3.2'},
  {key: 'ollama', adapter: 'ollama', label: 'Local Ollama', billing: 'SELF_HOSTED_INFRA' as const, url: 'http://127.0.0.1:11434', model: 'llama3.2'},
];

export function SetupPage() {
  const {navigate} = useRouter();
  const [step, setStep] = useState(0);
  const [initialized, setInitialized] = useState<boolean | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const [bootstrapToken, setBootstrapToken] = useState('');
  const [ownerEmail, setOwnerEmail] = useState('');
  const [ownerPassword, setOwnerPassword] = useState('');
  const [passwordConfirm, setPasswordConfirm] = useState('');
  const [orgName, setOrgName] = useState('Default Organization');
  const [projectName, setProjectName] = useState('default');

  const [addConnection, setAddConnection] = useState(true);
  const [providerKey, setProviderKey] = useState('ollama');
  const [connectionName, setConnectionName] = useState('Primary connection');
  const [endpoint, setEndpoint] = useState('http://127.0.0.1:11434');
  const [modelId, setModelId] = useState('llama3.2');
  const [region, setRegion] = useState<DataRegion>('ON_PREM');
  const [secretValue, setSecretValue] = useState('');
  const [allowPrivateIP, setAllowPrivateIP] = useState(false);
  const [allowedCIDRs, setAllowedCIDRs] = useState('127.0.0.1/32');

  const [acknowledgedCustody, setAcknowledgedCustody] = useState(false);
  const [result, setResult] = useState<components['schemas']['BootstrapCompleteResponse'] | null>(null);

  const preset = useMemo(() => providerPresets.find(item => item.key === providerKey) ?? providerPresets[0], [providerKey]);
  const isLocal = preset.billing === 'SELF_HOSTED_INFRA';

  useEffect(() => {
    let active = true;
    getBootstrapStatus()
      .then(status => {
        if (!active) return;
        setInitialized(status.initialized);
        if (status.initialized) navigate('/login', {replace: true});
      })
      .catch(cause => {
        if (active) setError(formatSetupError(cause));
      });
    return () => { active = false; };
  }, [navigate]);

  function choosePreset(key: string) {
    const selected = providerPresets.find(item => item.key === key);
    if (!selected) return;
    setProviderKey(key);
    setEndpoint(selected.url);
    setModelId(selected.model);
    setRegion(selected.billing === 'SELF_HOSTED_INFRA' ? 'ON_PREM' : 'US');
    setAllowPrivateIP(false);
    setSecretValue('');
  }

  function validate(targetStep: number): string {
    if (targetStep === 0) {
      if (!bootstrapToken.trim()) return 'Enter the one-time bootstrap token printed by the Tanpopo server.';
      if (!ownerEmail.includes('@')) return 'Enter a valid administrator email address.';
      if (ownerPassword.length < 12) return 'Use a password with at least 12 characters.';
      if (ownerPassword !== passwordConfirm) return 'Password confirmation does not match.';
      if (!orgName.trim() || !projectName.trim()) return 'Organization and project names are required.';
    }
    if (targetStep === 1 && addConnection) {
      try {
        const parsed = new URL(endpoint);
        if (!['http:', 'https:'].includes(parsed.protocol)) return 'Provider URL must use HTTP or HTTPS.';
        if (!isLocal && parsed.protocol !== 'https:') return 'Commercial provider URLs must use HTTPS.';
      } catch {
        return 'Enter a valid provider URL.';
      }
      if (!connectionName.trim() || !modelId.trim()) return 'Connection name and upstream model name are required.';
      const cidrs = allowedCIDRs.split(',').map(v => v.trim()).filter(Boolean);
      if (isLocal && (!allowPrivateIP || cidrs.length === 0)) {
        return 'Explicitly allow private network access and specify at least one non-wildcard CIDR.';
      }
      if (cidrs.some(v => v === '*' || v === '0.0.0.0/0' || v === '::/0')) {
        return 'Wildcard CIDR ranges (0.0.0.0/0, ::/0, *) are forbidden.';
      }
      if (!isLocal && !secretValue.trim()) {
        return 'Enter the provider API key. It is sealed write-only and never retained in the browser.';
      }
    }
    if (targetStep === 2 && !acknowledgedCustody) {
      return 'Confirm custody of the instance master key before initializing.';
    }
    return '';
  }

  function handleNext() {
    const issue = validate(step);
    if (issue) {
      setError(issue);
      return;
    }
    setError('');
    setStep(current => Math.min(current + 1, steps.length - 1));
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy || initialized !== false) return;
    for (let idx = 0; idx < 3; idx += 1) {
      const issue = validate(idx);
      if (issue) {
        setStep(idx);
        setError(issue);
        return;
      }
    }
    setBusy(true);
    setError('');
    let initialConnection: InitialConnection | undefined;
    if (addConnection) {
      initialConnection = {
        adapter_id: preset.adapter,
        name: connectionName.trim(),
        billing_mode: preset.billing,
        endpoint_url: endpoint.trim(),
        allow_private_ip: isLocal && allowPrivateIP,
        ...(isLocal ? {allowed_cidrs: allowedCIDRs.split(',').map(v => v.trim()).filter(Boolean)} : {}),
        ...(!isLocal ? {secret_value: secretValue} : {}),
        model_id: modelId.trim(),
        data_region: region,
      };
    }
    const payload: BootstrapForm = {
      bootstrap_token: bootstrapToken.trim(),
      owner_email: ownerEmail.trim(),
      owner_password: ownerPassword,
      org_name: orgName.trim(),
      project_name: projectName.trim(),
      master_kek_backup_acknowledged: acknowledgedCustody,
      ...(initialConnection ? {initial_connection: initialConnection} : {}),
    };
    try {
      const response = await completeBootstrap(payload);
      setBootstrapToken('');
      setOwnerPassword('');
      setPasswordConfirm('');
      setResult(response);
      if (!response.initial_virtual_key) navigate('/overview', {replace: true});
    } catch (cause) {
      setError(formatSetupError(cause));
    } finally {
      setSecretValue('');
      setBusy(false);
    }
  }

  if (result?.initial_virtual_key) {
    return (
      <FirstKeyDialog
        secret={result.initial_virtual_key}
        model={modelId.trim()}
        onContinue={() => {
          setResult(null);
          navigate('/overview', {replace: true});
        }}
      />
    );
  }

  if (initialized === null) {
    return (
      <main className="tp-auth-page">
        <div className="tp-auth-panel">
          {error ? <Banner tone="danger" title="Setup status unavailable">{error}</Banner> : <p className="tp-muted" role="status">Checking instance initialization…</p>}
        </div>
      </main>
    );
  }
  if (initialized) return null;

  return (
    <main className="tp-auth-page">
      <div className="tp-setup-shell">
        <div className="tp-stack">
          <div className="tp-row">
            <TanpopoMark size={24} />
            <span style={{fontWeight: 700, fontSize: '1rem'}}>Tanpopo</span>
          </div>
          <div className="tp-stack-sm">
            <h1>Instance setup</h1>
            <p className="tp-muted">Claim this self-hosted gateway and provision its initial owner and route.</p>
          </div>
          <ol className="tp-stepper" aria-label="Setup steps">
            {steps.map((item, index) => {
              const state = index === step ? 'active' : index < step ? 'done' : 'upcoming';
              return (
                <li key={item.title}>
                  <button
                    type="button"
                    className="tp-step-btn"
                    data-state={state}
                    disabled={index > step}
                    onClick={() => {
                      if (index < step) {
                        setStep(index);
                        setError('');
                      }
                    }}
                  >
                    <span className="tp-step-num" aria-hidden="true">
                      {state === 'done' ? <Check size={13} /> : index + 1}
                    </span>
                    <span className="tp-checkbox-text">
                      <span style={{fontWeight: 600, fontSize: '0.875rem'}}>{item.title}</span>
                      <span className="tp-hint">{item.summary}</span>
                    </span>
                  </button>
                </li>
              );
            })}
          </ol>
          <p className="tp-muted">
            Already initialized? <Link href="/login">Sign in</Link>
          </p>
        </div>

        <form className="tp-surface tp-surface-pad tp-stack" onSubmit={handleSubmit}>
          {error ? <Banner tone="danger" title="Cannot continue">{error}</Banner> : null}

          {step === 0 ? (
            <>
              <h2>Owner account & initial tenant</h2>
              <TextField
                label="Bootstrap token"
                type="password"
                autoComplete="one-time-code"
                value={bootstrapToken}
                hint="Printed once on server startup."
                onChange={event => setBootstrapToken(event.target.value)}
              />
              <TextField
                label="Owner email"
                type="email"
                autoComplete="email"
                value={ownerEmail}
                onChange={event => setOwnerEmail(event.target.value)}
              />
              <div className="tp-grid-2">
                <TextField
                  label="Owner password"
                  type="password"
                  autoComplete="new-password"
                  value={ownerPassword}
                  hint="Minimum 12 characters."
                  onChange={event => setOwnerPassword(event.target.value)}
                />
                <TextField
                  label="Confirm password"
                  type="password"
                  autoComplete="new-password"
                  value={passwordConfirm}
                  onChange={event => setPasswordConfirm(event.target.value)}
                />
              </div>
              <div className="tp-grid-2">
                <TextField label="Organization name" value={orgName} onChange={event => setOrgName(event.target.value)} />
                <TextField label="First project name" value={projectName} onChange={event => setProjectName(event.target.value)} />
              </div>
            </>
          ) : null}

          {step === 1 ? (
            <>
              <h2>Initial provider connection</h2>
              <CheckboxField
                label="Provision a starter connection, deployment, route, and virtual key"
                description="Creates a working governed route in one atomic transaction."
                checked={addConnection}
                onChange={setAddConnection}
              />
              {addConnection ? (
                <>
                  <SelectField label="Provider adapter" value={providerKey} onChange={event => choosePreset(event.target.value)}>
                    {providerPresets.map(item => (
                      <option key={item.key} value={item.key}>{item.label}</option>
                    ))}
                  </SelectField>
                  <div className="tp-grid-2">
                    <TextField label="Connection name" value={connectionName} onChange={event => setConnectionName(event.target.value)} />
                    <SelectField label="Data region" value={region} onChange={event => setRegion(event.target.value as DataRegion)}>
                      <option value="US">US</option>
                      <option value="EU">EU</option>
                      <option value="APAC">APAC</option>
                      <option value="ON_PREM">ON_PREM</option>
                    </SelectField>
                  </div>
                  <TextField label="Provider URL" type="url" value={endpoint} onChange={event => setEndpoint(event.target.value)} />
                  <TextField label="Upstream model name" value={modelId} onChange={event => setModelId(event.target.value)} />
                  {!isLocal ? (
                    <TextField
                      label="Provider API key"
                      type="password"
                      autoComplete="new-password"
                      value={secretValue}
                      hint="Sealed write-only in the vault and never returned."
                      onChange={event => setSecretValue(event.target.value)}
                    />
                  ) : (
                    <>
                      <CheckboxField
                        label="Allow private or loopback upstream for this connection"
                        description="Cloud metadata endpoints and control-plane ports remain blocked."
                        checked={allowPrivateIP}
                        onChange={setAllowPrivateIP}
                      />
                      <TextField
                        label="Allowed private CIDRs"
                        value={allowedCIDRs}
                        hint="Comma-separated specific CIDRs (e.g. 127.0.0.1/32). Wildcards are rejected."
                        onChange={event => setAllowedCIDRs(event.target.value)}
                      />
                    </>
                  )}
                </>
              ) : (
                <p className="tp-muted">Connections can be added later from Providers.</p>
              )}
            </>
          ) : null}

          {step === 2 ? (
            <>
              <h2>Master key custody</h2>
              <p className="tp-muted">
                Tanpopo encrypts provider credentials and secrets with the configured instance master key (KEK). Back it up in your approved secret manager—losing it renders encrypted credentials unrecoverable.
              </p>
              <CheckboxField
                label="I have securely backed up the Tanpopo master key"
                checked={acknowledgedCustody}
                onChange={setAcknowledgedCustody}
              />
            </>
          ) : null}

          {step === 3 ? (
            <>
              <h2>Review and initialize</h2>
              <KeyValueList
                items={[
                  {label: 'Owner email', value: ownerEmail},
                  {label: 'Organization / Project', value: `${orgName} / ${projectName}`},
                  {
                    label: 'Initial connection',
                    value: addConnection ? `${preset.label} · ${endpoint} · ${modelId} (${region})` : 'Skipped',
                  },
                ]}
              />
            </>
          ) : null}

          <div className="tp-row" style={{justifyContent: 'space-between'}}>
            {step > 0 ? (
              <Button variant="secondary" disabled={busy} onClick={() => { setError(''); setStep(s => Math.max(0, s - 1)); }}>
                Back
              </Button>
            ) : <span />}
            {step < steps.length - 1 ? (
              <Button variant="primary" onClick={handleNext}>Continue</Button>
            ) : (
              <Button type="submit" variant="primary" loading={busy}>
                {busy ? 'Initializing…' : 'Complete setup'}
              </Button>
            )}
          </div>
        </form>
      </div>
    </main>
  );
}

function FirstKeyDialog({secret, model, onContinue}: {secret: CreatedSecret; model: string; onContinue: () => void}) {
  const [acknowledged, setAcknowledged] = useState(false);
  const [testing, setTesting] = useState(false);
  const [testError, setTestError] = useState('');
  const [testResult, setTestResult] = useState<{text: string; requestId: string; latencyMs: number; verified: boolean} | null>(null);

  async function runFirstRequest() {
    if (testing || !secret.secret_key) return;
    setTesting(true);
    setTestError('');
    setTestResult(null);
    const started = performance.now();
    try {
      const response = await fetch('/v1/chat/completions', {
        method: 'POST',
        credentials: 'omit',
        headers: {Authorization: `Bearer ${secret.secret_key}`, 'Content-Type': 'application/json'},
        body: JSON.stringify({model, messages: [{role: 'user', content: 'Reply with a short greeting.'}], stream: true, max_tokens: 32}),
      });
      if (!response.ok || !response.body) {
        const body = await response.text();
        throw new Error(extractStreamError(body, response.status));
      }
      const reader = response.body.getReader();
      const decoder = new TextDecoder();
      let buffer = '';
      let text = '';
      let doneSeen = false;
      const reqId = response.headers.get('x-tanpopo-request-id') ?? 'unreported';
      while (true) {
        const chunk = await reader.read();
        if (chunk.done) break;
        buffer += decoder.decode(chunk.value, {stream: true});
        const lines = buffer.split('\n');
        buffer = lines.pop() ?? '';
        for (const line of lines) {
          if (!line.startsWith('data:')) continue;
          const payload = line.slice(5).trim();
          if (!payload) continue;
          if (payload === '[DONE]') {
            doneSeen = true;
            continue;
          }
          const parsed = JSON.parse(payload) as {choices?: Array<{delta?: {content?: string}}>};
          const delta = parsed.choices?.[0]?.delta?.content;
          if (delta) text += delta;
          setTestResult({text, requestId: reqId, latencyMs: Math.round(performance.now() - started), verified: false});
        }
      }
      if (!doneSeen || !text.trim()) {
        throw new Error('The test stream ended without a complete response.');
      }
      setTestResult({text, requestId: reqId, latencyMs: Math.round(performance.now() - started), verified: true});
    } catch (cause) {
      setTestResult(null);
      setTestError(cause instanceof Error ? cause.message : 'First request failed.');
    } finally {
      setTesting(false);
    }
  }

  const origin = typeof window === 'undefined' ? 'http://localhost:8080' : window.location.origin;
  const curlSnippet = `curl ${origin}/v1/chat/completions \\\n  -H "Authorization: Bearer ${secret.secret_key}" \\\n  -H "Content-Type: application/json" \\\n  -d '{"model":"${model}","messages":[{"role":"user","content":"Hello"}],"stream":true}'`;

  return (
    <Modal
      open
      dismissible={false}
      onClose={() => undefined}
      size="lg"
      title="Store your virtual key & verify first request"
      subtitle="This plaintext secret is shown once and cannot be retrieved after closing."
      footer={
        <Button variant="primary" disabled={!acknowledged || !testResult?.verified || testing} onClick={onContinue}>
          Continue to Overview
        </Button>
      }
    >
      <CodeBlock title={`${secret.name} (${secret.key_prefix}…${secret.last_four})`} code={secret.secret_key} />
      <CodeBlock title="cURL quickstart" code={curlSnippet} />
      <div className="tp-row">
        <Button variant="primary" loading={testing} onClick={() => void runFirstRequest()}>
          {testing ? 'Sending streaming test…' : 'Send Test Request'}
        </Button>
      </div>
      {testError ? <Banner tone="danger" title="Test request failed">{testError}</Banner> : null}
      {testResult ? (
        <Banner tone={testResult.verified ? 'ok' : 'info'} title={testResult.verified ? `Verified in ${testResult.latencyMs} ms` : 'Streaming response…'}>
          <div className="tp-stack-sm">
            <span className="mono">Request ID: {testResult.requestId}</span>
            <span>{testResult.text}</span>
          </div>
        </Banner>
      ) : null}
      <CheckboxField
        label="I have securely stored this virtual key"
        checked={acknowledged}
        onChange={setAcknowledged}
      />
    </Modal>
  );
}

function extractStreamError(body: string, status: number): string {
  try {
    const parsed = JSON.parse(body) as {error?: {message?: string}};
    if (parsed.error?.message) return parsed.error.message;
  } catch {
    // Fallback to HTTP status text.
  }
  return `First request failed (${status}). Check the deployment and route configuration.`;
}

function formatSetupError(cause: unknown): string {
  if (cause instanceof Error) {
    const code = 'code' in cause && typeof cause.code === 'string' ? cause.code : '';
    if (code === 'bootstrap_already_completed') return 'This instance is already initialized. Sign in instead.';
    if (code === 'invalid_credentials') return 'The bootstrap token or owner credentials were rejected.';
    if (code === 'ssrf_blocked_private_ip' || code === 'egress_port_denied') return 'The provider URL is blocked by egress network policy.';
    return cause.message;
  }
  return 'Could not reach the Tanpopo server.';
}

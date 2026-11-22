import {useEffect, useMemo, useState, type FormEvent} from 'react';
import {Moon, Sun} from 'lucide-react';
import {Step, Stepper} from '@astryxdesign/core/Stepper';
import {completeBootstrap, getBootstrapStatus} from '../../api/client';
import type {components} from '../../api/generated';
import {useI18n} from '../../i18n/I18nProvider';
import {LanguageSelector} from '../../i18n/LanguageSelector';
import {Link, useRouter} from '../../router/RouterContext';
import {useTheme} from '../../styles/theme';
import {Banner, Button, CheckboxField, CodeBlock, IconButton, KeyValueList, SelectField, TanpopoMark, TextField} from '../../ui/primitives';
import {Modal} from '../../ui/Overlays';

type BootstrapForm = components['schemas']['BootstrapCompleteRequest'];
type InitialConnection = NonNullable<BootstrapForm['initial_connection']>;
type DataRegion = components['schemas']['DataRegion'];
type CreatedSecret = components['schemas']['VirtualKeyCreatedResponse'];

const stepKeys = [
  {titleKey: 'setup.step0.title', summaryKey: 'setup.step0.summary'},
  {titleKey: 'setup.step1.title', summaryKey: 'setup.step1.summary'},
  {titleKey: 'setup.step2.title', summaryKey: 'setup.step2.summary'},
  {titleKey: 'setup.step3.title', summaryKey: 'setup.step3.summary'},
];

const providerPresets = [
  {key: 'openai', adapter: 'openai', label: 'OpenAI', billing: 'COMMERCIAL_API' as const, url: 'https://api.openai.com/v1', model: 'gpt-4o-mini'},
  {key: 'anthropic', adapter: 'anthropic', label: 'Anthropic', billing: 'COMMERCIAL_API' as const, url: 'https://api.anthropic.com', model: 'claude-3-5-sonnet-latest'},
  {key: 'gemini', adapter: 'gemini', label: 'Google Gemini', billing: 'COMMERCIAL_API' as const, url: 'https://generativelanguage.googleapis.com', model: 'gemini-2.0-flash'},
  {key: 'openai-compatible', adapter: 'openai-compatible', labelKey: 'setup.preset.openaiCompatible', billing: 'COMMERCIAL_API' as const, url: 'https://api.example.com/v1', model: 'gpt-4o-mini'},
  {key: 'vllm', adapter: 'openai-compatible', labelKey: 'setup.preset.localVllm', billing: 'SELF_HOSTED_INFRA' as const, url: 'http://127.0.0.1:8000/v1', model: 'llama3.2'},
  {key: 'ollama', adapter: 'ollama', labelKey: 'setup.preset.localOllama', billing: 'SELF_HOSTED_INFRA' as const, url: 'http://127.0.0.1:11434', model: 'llama3.2'},
];

export function SetupPage() {
  const {navigate} = useRouter();
  const {theme, toggleTheme} = useTheme();
  const {t} = useI18n();
  const [step, setStep] = useState(0);
  const [initialized, setInitialized] = useState<boolean | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const [bootstrapToken, setBootstrapToken] = useState('');
  const [ownerEmail, setOwnerEmail] = useState('');
  const [ownerPassword, setOwnerPassword] = useState('');
  const [passwordConfirm, setPasswordConfirm] = useState('');
  const [orgName, setOrgName] = useState(() => t('setup.defaultOrgName'));
  const [projectName, setProjectName] = useState('default');

  const [addConnection, setAddConnection] = useState(true);
  const [providerKey, setProviderKey] = useState('ollama');
  const [connectionName, setConnectionName] = useState(() => t('setup.defaultConnName'));
  const [endpoint, setEndpoint] = useState('http://127.0.0.1:11434');
  const [modelId, setModelId] = useState('llama3.2');
  const [region, setRegion] = useState<DataRegion>('ON_PREM');
  const [secretValue, setSecretValue] = useState('');
  const [allowPrivateIP, setAllowPrivateIP] = useState(false);
  const [allowedCIDRs, setAllowedCIDRs] = useState('127.0.0.1/32');

  const [acknowledgedCustody, setAcknowledgedCustody] = useState(false);
  const [result, setResult] = useState<components['schemas']['BootstrapCompleteResponse'] | null>(null);

  const preset = useMemo(() => providerPresets.find(item => item.key === providerKey) ?? providerPresets[0], [providerKey]);
  const presetLabel = (item: (typeof providerPresets)[number]) => (item.labelKey ? t(item.labelKey) : item.label ?? item.key);
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
        if (active) setError(formatSetupError(cause, t));
      });
    return () => { active = false; };
  }, [navigate, t]);

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
      if (!bootstrapToken.trim()) return t('setup.val.bootstrapToken');
      if (!ownerEmail.includes('@')) return t('setup.val.ownerEmail');
      if (ownerPassword.length < 12) return t('setup.val.passwordLength');
      if (ownerPassword !== passwordConfirm) return t('setup.val.passwordMismatch');
      if (!orgName.trim() || !projectName.trim()) return t('setup.val.orgProject');
    }
    if (targetStep === 1 && addConnection) {
      try {
        const parsed = new URL(endpoint);
        if (!['http:', 'https:'].includes(parsed.protocol)) return t('setup.val.urlProtocol');
        if (!isLocal && parsed.protocol !== 'https:') return t('setup.val.httpsRequired');
      } catch {
        return t('setup.val.invalidUrl');
      }
      if (!connectionName.trim() || !modelId.trim()) return t('setup.val.connAndModel');
      const cidrs = allowedCIDRs.split(',').map(v => v.trim()).filter(Boolean);
      if (isLocal && (!allowPrivateIP || cidrs.length === 0)) {
        return t('setup.val.privateCidrRequired');
      }
      if (cidrs.some(v => v === '*' || v === '0.0.0.0/0' || v === '::/0')) {
        return t('setup.val.wildcardCidr');
      }
      if (!isLocal && !secretValue.trim()) {
        return t('setup.val.apiKeyRequired');
      }
    }
    if (targetStep === 2 && !acknowledgedCustody) {
      return t('setup.val.custodyRequired');
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
    setStep(current => Math.min(current + 1, stepKeys.length - 1));
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
      setError(formatSetupError(cause, t));
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
          {error ? <Banner tone="danger" title={t('setup.statusUnavailable')}>{error}</Banner> : <p className="tp-muted" role="status">{t('setup.checkingInit')}</p>}
        </div>
      </main>
    );
  }
  if (initialized) return null;

  return (
    <main className="tp-auth-page">
      <div className="tp-setup-shell">
        <div className="tp-setup-rail">
          <div className="tp-auth-head">
            <div className="tp-auth-brand">
              <TanpopoMark size={22} />
              <span style={{fontWeight: 700, fontSize: '0.9375rem', letterSpacing: '-0.02em'}}>Tanpopo</span>
            </div>
            <div className="tp-auth-controls">
              <LanguageSelector id="setup-language-selector" />
              <IconButton variant="secondary" label={theme === 'dark' ? t('common.switchToLightTheme') : t('common.switchToDarkTheme')} onClick={toggleTheme}>
                {theme === 'dark' ? <Sun size={15} aria-hidden="true" /> : <Moon size={15} aria-hidden="true" />}
              </IconButton>
            </div>
          </div>
          <div className="tp-stack-sm">
            <h1>{t('setup.title')}</h1>
            <p className="tp-muted">{t('setup.subtitle')}</p>
          </div>
          <Stepper
            activeStep={step}
            orientation="vertical"
            label={t('setup.stepsLabel')}
            density="compact"
            onStepClick={index => {
              if (index < step) {
                setStep(index);
                setError('');
              }
            }}
          >
            {stepKeys.map((item, index) => (
              <Step
                key={item.titleKey}
                step={index}
                label={t(item.titleKey)}
                isDisabled={index > step}
              />
            ))}
          </Stepper>
          <p className="tp-muted">
            {t('setup.alreadyInitialized')} <Link href="/login" className="tp-link">{t('login.submit')}</Link>
          </p>
        </div>

        <form className="tp-surface tp-setup-form tp-stack" onSubmit={handleSubmit}>
          {error ? <Banner tone="danger" title={t('setup.cannotContinue')}>{error}</Banner> : null}

          {step === 0 ? (
            <>
              <h2>{t('setup.ownerHeading')}</h2>
              <p className="tp-muted">{t('setup.step0.summary')}</p>
              <TextField
                label={t('setup.bootstrapToken')}
                type="password"
                autoComplete="one-time-code"
                value={bootstrapToken}
                hint={t('setup.bootstrapTokenHint')}
                onChange={event => setBootstrapToken(event.target.value)}
              />
              <TextField
                label={t('setup.ownerEmail')}
                type="email"
                autoComplete="email"
                value={ownerEmail}
                onChange={event => setOwnerEmail(event.target.value)}
              />
              <div className="tp-grid-2">
                <TextField
                  label={t('setup.ownerPassword')}
                  type="password"
                  autoComplete="new-password"
                  value={ownerPassword}
                  hint={t('setup.ownerPasswordHint')}
                  onChange={event => setOwnerPassword(event.target.value)}
                />
                <TextField
                  label={t('setup.confirmPassword')}
                  type="password"
                  autoComplete="new-password"
                  value={passwordConfirm}
                  hint={t('setup.confirmPasswordHint')}
                  onChange={event => setPasswordConfirm(event.target.value)}
                />
              </div>
              <div className="tp-grid-2">
                <TextField label={t('setup.orgName')} value={orgName} onChange={event => setOrgName(event.target.value)} />
                <TextField label={t('setup.projectName')} value={projectName} onChange={event => setProjectName(event.target.value)} />
              </div>
            </>
          ) : null}

          {step === 1 ? (
            <>
              <h2>{t('setup.connectionHeading')}</h2>
              <p className="tp-muted">{t('setup.step1.summary')}</p>
              <CheckboxField
                label={t('setup.provisionStarter')}
                description={t('setup.provisionStarterDesc')}
                checked={addConnection}
                onChange={setAddConnection}
              />
              {addConnection ? (
                <>
                  <SelectField
                    label={t('setup.providerAdapter')}
                    value={providerKey}
                    onChange={choosePreset}
                    options={providerPresets.map(item => ({value: item.key, label: presetLabel(item)}))}
                  />
                  <div className="tp-grid-2">
                    <TextField label={t('setup.connectionName')} value={connectionName} onChange={event => setConnectionName(event.target.value)} />
                    <SelectField
                      label={t('setup.dataRegion')}
                      value={region}
                      onChange={next => setRegion(next as DataRegion)}
                      options={[
                        {value: 'US', label: t('common.region.US')},
                        {value: 'EU', label: t('common.region.EU')},
                        {value: 'APAC', label: t('common.region.APAC')},
                        {value: 'ON_PREM', label: t('common.region.ON_PREM')},
                      ]}
                    />
                  </div>
                  <TextField label={t('setup.providerUrl')} type="url" value={endpoint} onChange={event => setEndpoint(event.target.value)} />
                  <TextField label={t('setup.upstreamModel')} value={modelId} onChange={event => setModelId(event.target.value)} />
                  {!isLocal ? (
                    <TextField
                      label={t('setup.providerApiKey')}
                      type="password"
                      autoComplete="new-password"
                      value={secretValue}
                      hint={t('setup.providerApiKeyHint')}
                      onChange={event => setSecretValue(event.target.value)}
                    />
                  ) : (
                    <>
                      <CheckboxField
                        label={t('setup.allowPrivateIp')}
                        description={t('setup.allowPrivateIpDesc')}
                        checked={allowPrivateIP}
                        onChange={setAllowPrivateIP}
                      />
                      <TextField
                        label={t('setup.allowedCidrs')}
                        value={allowedCIDRs}
                        hint={t('setup.allowedCidrsHint')}
                        onChange={event => setAllowedCIDRs(event.target.value)}
                      />
                    </>
                  )}
                </>
              ) : (
                <p className="tp-muted">{t('setup.connectionsLater')}</p>
              )}
            </>
          ) : null}

          {step === 2 ? (
            <>
              <h2>{t('setup.custodyHeading')}</h2>
              <p className="tp-muted">{t('setup.custodyDesc')}</p>
              <CheckboxField
                label={t('setup.custodyAck')}
                checked={acknowledgedCustody}
                onChange={setAcknowledgedCustody}
              />
            </>
          ) : null}

          {step === 3 ? (
            <>
              <h2>{t('setup.reviewHeading')}</h2>
              <p className="tp-muted">{t('setup.step3.summary')}</p>
              <KeyValueList
                items={[
                  {label: t('setup.ownerEmail'), value: ownerEmail},
                  {label: t('setup.reviewOrgProject'), value: `${orgName} / ${projectName}`},
                  {
                    label: t('setup.reviewInitialConn'),
                    value: addConnection ? `${presetLabel(preset)} · ${endpoint} · ${modelId} (${region})` : t('setup.skipped'),
                  },
                ]}
              />
            </>
          ) : null}

          <div className="tp-row" style={{justifyContent: 'space-between'}}>
            {step > 0 ? (
              <Button variant="secondary" disabled={busy} onClick={() => { setError(''); setStep(s => Math.max(0, s - 1)); }}>
                {t('common.back')}
              </Button>
            ) : <span />}
            {step < stepKeys.length - 1 ? (
              <Button key="continue-step" variant="primary" onClick={handleNext}>{t('common.continue')}</Button>
            ) : (
              <Button key="complete-setup" type="submit" variant="primary" loading={busy}>
                {busy ? t('setup.initializing') : t('setup.completeSetup')}
              </Button>
            )}
          </div>
        </form>
      </div>
    </main>
  );
}

function FirstKeyDialog({secret, model, onContinue}: {secret: CreatedSecret; model: string; onContinue: () => void}) {
  const {t, formatNumber} = useI18n();
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
        throw new Error(extractStreamError(body, response.status, t));
      }
      const reader = response.body.getReader();
      const decoder = new TextDecoder();
      let buffer = '';
      let text = '';
      let doneSeen = false;
      const reqId = response.headers.get('x-tanpopo-request-id') ?? t('setup.firstKey.unreported');
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
        throw new Error(t('setup.firstKey.incompleteStream'));
      }
      setTestResult({text, requestId: reqId, latencyMs: Math.round(performance.now() - started), verified: true});
    } catch (cause) {
      setTestResult(null);
      setTestError(cause instanceof Error ? cause.message : t('setup.firstKey.requestFailed'));
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
      title={t('setup.firstKey.title')}
      subtitle={t('setup.firstKey.subtitle')}
      footer={
        <Button variant="primary" disabled={!acknowledged || !testResult?.verified || testing} onClick={onContinue}>
          {t('setup.firstKey.continue')}
        </Button>
      }
    >
      <CodeBlock title={`${secret.name} (${secret.key_prefix}…${secret.last_four})`} code={secret.secret_key} />
      <CodeBlock title={t('setup.firstKey.curlTitle')} code={curlSnippet} />
      <div className="tp-row">
        <Button variant="primary" loading={testing} onClick={() => void runFirstRequest()}>
          {testing ? t('setup.firstKey.sending') : t('setup.firstKey.sendTest')}
        </Button>
      </div>
      {testError ? <Banner tone="danger" title={t('setup.firstKey.testFailedTitle')}>{testError}</Banner> : null}
      {testResult ? (
        <Banner
          tone={testResult.verified ? 'ok' : 'info'}
          title={testResult.verified ? t('setup.firstKey.verifiedIn', {ms: formatNumber(testResult.latencyMs)}) : t('setup.firstKey.streaming')}
        >
          <div className="tp-stack-sm">
            <span className="mono">{t('setup.firstKey.requestId', {id: testResult.requestId})}</span>
            <span>{testResult.text}</span>
          </div>
        </Banner>
      ) : null}
      <CheckboxField
        label={t('setup.firstKey.storedAck')}
        checked={acknowledged}
        onChange={setAcknowledged}
      />
    </Modal>
  );
}

function extractStreamError(body: string, status: number, t: (key: string, vars?: Record<string, string | number>) => string): string {
  try {
    const parsed = JSON.parse(body) as {error?: {message?: string}};
    if (parsed.error?.message) return parsed.error.message;
  } catch {
    // Fallback to HTTP status text.
  }
  return t('setup.firstKey.statusFailed', {status});
}

function formatSetupError(cause: unknown, t: (key: string) => string): string {
  if (cause instanceof Error) {
    const code = 'code' in cause && typeof cause.code === 'string' ? cause.code : '';
    if (code === 'bootstrap_already_completed') return t('setup.err.alreadyCompleted');
    if (code === 'invalid_credentials') return t('setup.err.invalidCredentials');
    if (code === 'ssrf_blocked_private_ip' || code === 'egress_port_denied') return t('setup.err.egressBlocked');
    return cause.message;
  }
  return t('setup.err.unreachable');
}

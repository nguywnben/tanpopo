import {useEffect, useMemo, useState, type FormEvent} from 'react';
import {Banner} from '@astryxdesign/core/Banner';
import {Button} from '@astryxdesign/core/Button';
import {Card} from '@astryxdesign/core/Card';
import {CheckboxInput} from '@astryxdesign/core/CheckboxInput';
import {FormLayout} from '@astryxdesign/core/FormLayout';
import {Heading, Text} from '@astryxdesign/core/Text';
import {RadioList, RadioListItem} from '@astryxdesign/core/RadioList';
import {HStack, VStack} from '@astryxdesign/core/Stack';
import {Step, Stepper} from '@astryxdesign/core/Stepper';
import {TextInput} from '@astryxdesign/core/TextInput';
import {completeBootstrap, getBootstrapStatus} from '../../api/client';
import type {components} from '../../api/generated';
import {ShowOnceSecretDialog} from '../../components/ShowOnceSecretDialog';

type BootstrapForm = components['schemas']['BootstrapCompleteRequest'];
type InitialConnection = NonNullable<BootstrapForm['initial_connection']>;
const stepLabels = ['Owner account', 'First connection', 'Security review', 'Complete setup'];
const connectionChoices = [
  {key: 'openai', adapter: 'openai', label: 'OpenAI', billing: 'COMMERCIAL_API' as const, url: 'https://api.openai.com/v1'},
  {key: 'anthropic', adapter: 'anthropic', label: 'Anthropic', billing: 'COMMERCIAL_API' as const, url: 'https://api.anthropic.com'},
  {key: 'gemini', adapter: 'gemini', label: 'Google Gemini', billing: 'COMMERCIAL_API' as const, url: 'https://generativelanguage.googleapis.com'},
  {key: 'openai-compatible', adapter: 'openai-compatible', label: 'OpenAI-compatible commercial API', billing: 'COMMERCIAL_API' as const, url: 'https://api.example.com/v1'},
  {key: 'vllm', adapter: 'openai-compatible', label: 'Local vLLM server', billing: 'SELF_HOSTED_INFRA' as const, url: 'http://127.0.0.1:8000/v1'},
  {key: 'ollama', adapter: 'ollama', label: 'Local Ollama server', billing: 'SELF_HOSTED_INFRA' as const, url: 'http://127.0.0.1:11434'},
];

export function SetupWizardPage() {
  const [step, setStep] = useState(0);
  const [initialized, setInitialized] = useState<boolean | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [bootstrapToken, setBootstrapToken] = useState('');
  const [ownerEmail, setOwnerEmail] = useState('');
  const [ownerPassword, setOwnerPassword] = useState('');
  const [passwordConfirm, setPasswordConfirm] = useState('');
  const [orgName, setOrgName] = useState('Default Organization');
  const [projectName, setProjectName] = useState('default');
  const [acknowledgedKeyCustody, setAcknowledgedKeyCustody] = useState(false);
  const [addConnection, setAddConnection] = useState(true);
  const [provider, setProvider] = useState('ollama');
  const [connectionName, setConnectionName] = useState('First connection');
  const [endpoint, setEndpoint] = useState('http://127.0.0.1:11434');
  const [modelId, setModelId] = useState('llama3.2');
  const [region, setRegion] = useState<components['schemas']['DataRegion']>('ON_PREM');
  const [secretValue, setSecretValue] = useState('');
  const [allowPrivateIP, setAllowPrivateIP] = useState(false);
  const [allowedCIDRs, setAllowedCIDRs] = useState('127.0.0.1/32');
  const [result, setResult] = useState<components['schemas']['BootstrapCompleteResponse'] | null>(null);

  const choice = useMemo(() => connectionChoices.find(item => item.key === provider) ?? connectionChoices[0], [provider]);
  const isLocalProvider = choice.billing === 'SELF_HOSTED_INFRA';

  useEffect(() => {
    let active = true;
    getBootstrapStatus().then(status => {
      if (!active) return;
      setInitialized(status.initialized);
      if (status.initialized) window.location.replace('/login');
    }).catch(cause => {
      if (active) {
        setError(errorMessage(cause));
      }
    });
    return () => { active = false; };
  }, []);

  function selectProvider(value: string) {
    const selected = connectionChoices.find(item => item.key === value);
    if (!selected) return;
    setProvider(value);
    setEndpoint(selected.url);
    setRegion(selected.billing === 'SELF_HOSTED_INFRA' ? 'ON_PREM' : 'US');
    setAllowPrivateIP(false);
    setSecretValue('');
  }

  function validateStep(): string {
    if (step === 0) {
      if (!bootstrapToken.trim()) return 'Enter the one-time bootstrap token printed by the Tanpopo server.';
      if (!ownerEmail.includes('@')) return 'Enter a valid owner email address.';
      if (ownerPassword.length < 12) return 'Use an owner password with at least 12 characters.';
      if (ownerPassword !== passwordConfirm) return 'The passwords do not match.';
      if (!orgName.trim() || !projectName.trim()) return 'Enter an organization and first project name.';
    }
    if (step === 1 && addConnection) {
      try {
        const parsed = new URL(endpoint);
        if (!['http:', 'https:'].includes(parsed.protocol)) return 'Use an HTTP or HTTPS provider URL.';
        if (!isLocalProvider && parsed.protocol !== 'https:') return 'Commercial provider URLs must use HTTPS.';
      } catch {
        return 'Enter a valid provider URL.';
      }
      if (!connectionName.trim() || !modelId.trim()) return 'Enter a connection name and upstream model name.';
      if (isLocalProvider && (!allowPrivateIP || !allowedCIDRs.trim())) return 'Explicitly allow the local upstream and enter a specific, non-wildcard CIDR.';
      if (!isLocalProvider && !secretValue.trim()) return 'Enter the provider API key. It is sent once and is not retained by the console.';
      if (allowedCIDRs.split(',').some(value => ['0.0.0.0/0', '::/0'].includes(value.trim()))) return 'Wildcard network ranges are not allowed.';
    }
    if (step === 2 && !acknowledgedKeyCustody) return 'Confirm that you can securely back up the master key before setup continues.';
    return '';
  }

  function nextStep() {
    const issue = validateStep();
    if (issue) {
      setError(issue);
      return;
    }
    setError('');
    setStep(value => Math.min(value + 1, stepLabels.length - 1));
  }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (loading || initialized !== false) return;
    const issue = validateStep();
    if (issue) {
      setError(issue);
      return;
    }
    setLoading(true);
    setError('');
    let initial_connection: InitialConnection | undefined;
    if (addConnection) {
      initial_connection = {
        adapter_id: choice.adapter,
        name: connectionName.trim(),
        billing_mode: choice.billing,
        endpoint_url: endpoint.trim(),
        allow_private_ip: isLocalProvider && allowPrivateIP,
        ...(isLocalProvider ? {allowed_cidrs: allowedCIDRs.split(',').map(value => value.trim()).filter(Boolean)} : {}),
        ...(!isLocalProvider ? {secret_value: secretValue} : {}),
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
      master_kek_backup_acknowledged: acknowledgedKeyCustody,
      ...(initial_connection ? {initial_connection} : {}),
    };
    try {
      const response = await completeBootstrap(payload);
      setBootstrapToken('');
      setOwnerPassword('');
      setPasswordConfirm('');
      setResult(response);
      if (!response.initial_virtual_key) window.location.assign('/overview');
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setSecretValue('');
      setLoading(false);
    }
  }

  if (result?.initial_virtual_key) {
    return <ShowOnceSecretDialog secret={result.initial_virtual_key} model={modelId.trim()} onAcknowledge={() => {
      setResult(null);
      window.location.assign('/overview');
    }} />;
  }
  if (initialized === null) return <VStack padding={6} gap={4}>{error ? <Banner status="error" title="Could not check setup status" description={error} collapsible={false} /> : <Text role="status">Checking first-run setup…</Text>}</VStack>;
  if (initialized) return null;

  return (
    <VStack padding={4} gap={5}>
      <VStack gap={1}>
        <Heading level={1}>Set up Tanpopo</Heading>
        <Text>Claim this instance, create its first owner, and prepare an initial governed model route.</Text>
      </VStack>
      <Stepper activeStep={step} orientation="vertical" label="First-run setup progress" onStepClick={index => {
        if (index < step) { setStep(index); setError(''); }
      }}>
        <Step step={0} label="Owner account" description="Redeem the one-time token and create the owner account." />
        <Step step={1} label="First connection" description="Choose a provider and explicitly allow local network access when needed." />
        <Step step={2} label="Security review" description="Confirm custody of the instance master key." />
        <Step step={3} label="Complete setup" description="Review the initial account and provider configuration." />
      </Stepper>
      {error ? <Banner status="error" title="Setup could not continue" description={error} collapsible={false} /> : null}
      <form onSubmit={submit}>
        <Card padding={5}>
          <VStack gap={4}>
            {step === 0 ? <>
              <Heading level={2}>Create the first administrator</Heading>
              <TextInput label="Bootstrap token" value={bootstrapToken} type="password" autoComplete="one-time-code" onChange={setBootstrapToken} />
              <TextInput label="Owner email" value={ownerEmail} type="email" autoComplete="email" onChange={setOwnerEmail} />
              <TextInput label="Owner password" value={ownerPassword} type="password" autoComplete="new-password" onChange={setOwnerPassword} />
              <TextInput label="Confirm password" value={passwordConfirm} type="password" autoComplete="new-password" onChange={setPasswordConfirm} />
              <TextInput label="Organization name" value={orgName} onChange={setOrgName} />
              <TextInput label="First project name" value={projectName} onChange={setProjectName} />
            </> : null}
            {step === 1 ? <>
              <Heading level={2}>Connect a first model (optional)</Heading>
              <CheckboxInput label="Create an initial provider connection" description="The server provisions the connection, healthy deployment, default route, and show-once virtual key in one transaction." value={addConnection} onChange={setAddConnection} />
              {addConnection ? <>
                <RadioList label="Provider adapter" value={provider} onChange={selectProvider}>
                  {connectionChoices.map(item => <RadioListItem key={item.key} value={item.key} label={item.label} />)}
                </RadioList>
                <FormLayout>
                  <TextInput label="Connection name" value={connectionName} onChange={setConnectionName} />
                  <TextInput label="Provider URL" type="url" value={endpoint} onChange={setEndpoint} />
                  <TextInput label="Upstream model name" value={modelId} onChange={setModelId} />
                  {!isLocalProvider ? <TextInput label="Provider API key" type="password" autoComplete="new-password" value={secretValue} onChange={setSecretValue} /> : null}
                  <TextInput label="Data region (EU, US, APAC, ON_PREM)" value={region} onChange={value => {
                    if (['EU', 'US', 'APAC', 'ON_PREM'].includes(value)) setRegion(value as components['schemas']['DataRegion']);
                  }} />
                </FormLayout>
                {isLocalProvider ? <>
                  <CheckboxInput label="Allow private or loopback upstream for this connection" description="This explicit opt-in is scoped to the provider endpoint. Cloud metadata addresses and Tanpopo control-plane ports remain blocked." value={allowPrivateIP} onChange={setAllowPrivateIP} />
                  <TextInput label="Allowed private network CIDR" value={allowedCIDRs} onChange={setAllowedCIDRs} />
                </> : null}
              </> : <Text>You can add connections later from the Providers section.</Text>}
            </> : null}
            {step === 2 ? <>
              <Heading level={2}>Protect the instance master key</Heading>
              <Text>Back up the configured master key using your approved secret-management process. Losing it makes protected credentials and data unavailable.</Text>
              <CheckboxInput label="I have securely backed up the Tanpopo master key" value={acknowledgedKeyCustody} onChange={setAcknowledgedKeyCustody} />
            </> : null}
            {step === 3 ? <>
              <Heading level={2}>Review and complete</Heading>
              <Text>Owner: {ownerEmail || 'Not entered'}</Text>
              <Text>Organization: {orgName || 'Not entered'} · Project: {projectName || 'Not entered'}</Text>
              <Text>{addConnection ? `${choice.label} · ${endpoint} · ${modelId}` : 'No provider connection will be created yet.'}</Text>
              {isLocalProvider && addConnection && !allowPrivateIP ? <Banner status="warning" title="Local network access is not enabled" description="Go back and explicitly allow the private or loopback upstream for this connection." collapsible={false} /> : null}
            </> : null}
            <HStack gap={3} hAlign="end" wrap="wrap">
              {step > 0 ? <Button label="Back" variant="secondary" type="button" onClick={() => { setError(''); setStep(value => Math.max(0, value - 1)); }} /> : null}
              {step < stepLabels.length - 1 ? <Button label="Continue" variant="primary" type="button" onClick={event => { event.preventDefault(); nextStep(); }} /> : <Button label="Complete setup" variant="primary" type="submit" isLoading={loading} />}
            </HStack>
            <Text>Already set up? <a href="/login">Sign in</a></Text>
          </VStack>
        </Card>
      </form>
    </VStack>
  );
}

function errorMessage(cause: unknown): string {
  if (cause instanceof Error) {
    const code = 'code' in cause && typeof cause.code === 'string' ? cause.code : '';
    if (code === 'bootstrap_already_completed') return 'This instance is already initialized. Sign in to continue.';
    if (code === 'invalid_credentials') return 'The bootstrap token or owner details could not be accepted. Check them and try again.';
    if (code === 'ssrf_blocked_private_ip' || code === 'egress_port_denied') return 'The provider URL is blocked by the server network security policy.';
    return cause.message;
  }
  return 'The request failed. Check the server connection and try again.';
}

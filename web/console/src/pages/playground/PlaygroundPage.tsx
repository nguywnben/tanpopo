import {useRef, useState} from 'react';
import {Button} from '@astryxdesign/core/Button';
import {Heading} from '@astryxdesign/core/Heading';
import {Section} from '@astryxdesign/core/Section';
import {Text} from '@astryxdesign/core/Text';
import {TextInput} from '@astryxdesign/core/TextInput';
import {HStack, VStack} from '@astryxdesign/core/Stack';
import {createPlaygroundSession, getTraceWaterfall} from '../../api/managementClient';
import type {components} from '../../api/generated';

type Role = components['schemas']['RbacRole'];
type Message = {role: 'user' | 'assistant'; content: string};
type Trace = components['schemas']['TraceSummary'];
const allowedRoles: Role[] = ['OPERATOR', 'ORG_OWNER', 'TEAM_ADMIN', 'DEVELOPER'];

export function PlaygroundPage({projectId, environmentId, roles}: {projectId: string; environmentId: string; roles: Role[]}) {
  const [model, setModel] = useState('');
  const [comparisonModel, setComparisonModel] = useState('');
  const [prompt, setPrompt] = useState('');
  const [budget, setBudget] = useState(500000);
  const [messages, setMessages] = useState<Message[]>([]);
  const [comparison, setComparison] = useState<{primary: string; secondary: string} | null>(null);
  const [primaryRequestId, setPrimaryRequestId] = useState('');
  const [comparisonRequestId, setComparisonRequestId] = useState('');
  const [primaryTrace, setPrimaryTrace] = useState<Trace | null>(null);
  const [comparisonTrace, setComparisonTrace] = useState<Trace | null>(null);
  const [session, setSession] = useState<components['schemas']['PlaygroundSessionResponse'] | null>(null);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');
  const abortRef = useRef<AbortController | null>(null);
  const authorized = roles.some(role => allowedRoles.includes(role));

  async function send() {
    if (!projectId || !environmentId || !model.trim() || !prompt.trim() || busy) return;
    setBusy(true); setMessage('');
    setComparison(null);
    setPrimaryRequestId(''); setComparisonRequestId(''); setPrimaryTrace(null); setComparisonTrace(null);
    const controller = new AbortController(); abortRef.current = controller;
    const userMessage: Message = {role: 'user', content: prompt.trim()};
    const next = [...messages, userMessage]; setMessages([...next, {role: 'assistant', content: ''}]); setPrompt('');
    try {
      const currentSession = session ?? await createPlaygroundSession({project_id: projectId, env_id: environmentId, ttl_seconds: 900, max_budget_micros: budget});
      setSession(currentSession);
      const response = await fetch('/v1/chat/completions', {
        method: 'POST', cache: 'no-store', credentials: 'omit', signal: controller.signal,
        headers: {'Authorization': `Bearer ${currentSession.ephemeral_virtual_key}`, 'Content-Type': 'application/json', 'Accept': 'text/event-stream'},
        body: JSON.stringify({model: model.trim(), messages: next, stream: true}),
      });
      if (!response.ok || !response.body) throw new Error(`Playground request failed (${response.status}).`);
      const requestId = response.headers.get('X-Tanpopo-Request-Id')?.trim() ?? '';
      setPrimaryRequestId(requestId);
      const reader = response.body.getReader(); const decoder = new TextDecoder(); let buffer = ''; let answer = '';
      while (true) {
        const chunk = await reader.read();
        if (chunk.done) break;
        buffer += decoder.decode(chunk.value, {stream: true});
        const lines = buffer.split('\n'); buffer = lines.pop() ?? '';
        for (const line of lines) {
          if (!line.startsWith('data:')) continue;
          const payload = line.slice(5).trim();
          if (!payload || payload === '[DONE]') continue;
          const event = JSON.parse(payload) as {choices?: {delta?: {content?: string}}[]};
          answer += event.choices?.[0]?.delta?.content ?? '';
          setMessages([...next, {role: 'assistant', content: answer}]);
        }
      }
      setMessages([...next, {role: 'assistant', content: answer}]);
      if (requestId) setPrimaryTrace(await loadTrace(requestId));
      if (comparisonModel.trim() && comparisonModel.trim() !== model.trim()) {
        const compareResponse = await fetch('/v1/chat/completions', {
          method: 'POST', cache: 'no-store', credentials: 'omit', signal: controller.signal,
          headers: {'Authorization': `Bearer ${currentSession.ephemeral_virtual_key}`, 'Content-Type': 'application/json', 'Accept': 'text/event-stream'},
          body: JSON.stringify({model: comparisonModel.trim(), messages: next, stream: true}),
        });
        if (!compareResponse.ok || !compareResponse.body) throw new Error(`Comparison request failed (${compareResponse.status}).`);
        const comparisonId = compareResponse.headers.get('X-Tanpopo-Request-Id')?.trim() ?? '';
        setComparisonRequestId(comparisonId);
        const compareReader = compareResponse.body.getReader(); const compareDecoder = new TextDecoder(); let compareBuffer = ''; let comparisonText = '';
        setComparison({primary: answer, secondary: ''});
        while (true) {
          const chunk = await compareReader.read();
          if (chunk.done) break;
          compareBuffer += compareDecoder.decode(chunk.value, {stream: true});
          const lines = compareBuffer.split('\n'); compareBuffer = lines.pop() ?? '';
          for (const line of lines) {
            if (!line.startsWith('data:')) continue;
            const payload = line.slice(5).trim();
            if (!payload || payload === '[DONE]') continue;
            const event = JSON.parse(payload) as {choices?: {delta?: {content?: string}}[]};
            comparisonText += event.choices?.[0]?.delta?.content ?? '';
            setComparison({primary: answer, secondary: comparisonText});
          }
        }
        setComparison({primary: answer, secondary: comparisonText});
        if (comparisonId) setComparisonTrace(await loadTrace(comparisonId));
      }
    } catch (error) {
      if (!(error instanceof DOMException && error.name === 'AbortError')) setMessage(error instanceof Error ? error.message : 'Playground request failed.');
    } finally { abortRef.current = null; setBusy(false); }
  }

  if (!authorized) return <Text role="alert">Your role cannot create Playground sessions.</Text>;
  return <VStack gap={4}>
    <Heading level={1}>Playground</Heading>
    <Text>Requests use a short-lived Virtual Key with a per-session hard spend ceiling. Prompts and responses remain in this browser tab only.</Text>
    {!environmentId ? <Text role="status">Select an environment to start a Playground session.</Text> : null}
    {session ? <Text role="status">Session expires {session.expires_at}; hard spend ceiling ${ (session.max_budget_micros / 1_000_000).toFixed(2) }.</Text> : null}
    {message ? <Text role="alert">{message}</Text> : null}
    <Section><VStack gap={3}><Heading level={2}>Model request</Heading>
      <TextInput label="Model or route slug" value={model} onChange={setModel} isDisabled={busy} />
      <TextInput label="Comparison model (optional)" value={comparisonModel} onChange={setComparisonModel} isDisabled={busy} />
      {!session ? <TextInput label="Maximum session budget (micro-USD)" value={String(budget)} onChange={value => setBudget(Number(value) || 0)} isDisabled={busy} /> : null}
      {!session && (budget < 1000 || budget > 5000000) ? <Text role="alert">Choose a budget between 1,000 and 5,000,000 micro-USD.</Text> : null}
      {messages.length ? <ol aria-label="Conversation transcript">{messages.map((item, index) => <li key={`${item.role}-${index}`}><Text><strong>{item.role === 'user' ? 'You' : 'Assistant'}:</strong> {item.content || (busy && item.role === 'assistant' ? 'Waiting for stream…' : '')}</Text></li>)}</ol> : <Text>No conversation turns are stored on the server. Enter a prompt to begin.</Text>}
      <label htmlFor="playground-prompt">Prompt</label>
      <textarea id="playground-prompt" value={prompt} onChange={event => setPrompt(event.target.value)} rows={5} disabled={busy} />
      {comparison ? <HStack gap={3} wrap="wrap"><Section><VStack gap={2}><Heading level={3}>{model}</Heading><Text>{comparison.primary}</Text></VStack></Section><Section><VStack gap={2}><Heading level={3}>{comparisonModel}</Heading><Text>{comparison.secondary || 'Waiting for comparison stream…'}</Text></VStack></Section></HStack> : null}
      {primaryRequestId || comparisonRequestId ? <Section><VStack gap={2}><Heading level={2}>Execution traces</Heading>
        {primaryRequestId ? <TraceSummary label="Primary" requestId={primaryRequestId} trace={primaryTrace} /> : null}
        {comparisonRequestId ? <TraceSummary label="Comparison" requestId={comparisonRequestId} trace={comparisonTrace} /> : null}
      </VStack></Section> : null}
      <HStack gap={2}><Button label={busy ? comparisonModel.trim() ? 'Streaming comparison…' : 'Streaming…' : 'Send'} variant="primary" isDisabled={busy || !environmentId || !model.trim() || !prompt.trim() || (!session && (budget < 1000 || budget > 5000000))} onClick={() => void send()} />
        {busy ? <Button label="Cancel stream" variant="secondary" onClick={() => abortRef.current?.abort()} /> : null}
        {session && !busy ? <Button label="End session" variant="ghost" onClick={() => { setSession(null); setMessages([]); setMessage('Session key removed from this tab. Signing out also revokes active Playground keys.'); }} /> : null}
      </HStack>
    </VStack></Section>
  </VStack>;
}

async function loadTrace(requestId: string): Promise<Trace | null> {
  for (let attempt = 0; attempt < 5; attempt += 1) {
    try { return await getTraceWaterfall(requestId) as Trace; }
    catch (error) {
      const status = error && typeof error === 'object' && 'status' in error ? Number(error.status) : 0;
      if (status !== 404 || attempt === 4) return null;
      await new Promise(resolve => window.setTimeout(resolve, 150));
    }
  }
  return null;
}

function TraceSummary({label, requestId, trace}: {label: string; requestId: string; trace: Trace | null}) {
  return <Text role="status">{label} request {requestId}: {trace
    ? `route ${trace.route_slug}, status ${trace.status_code}, ${trace.usage_accuracy.toLowerCase()} usage, $${(trace.total_cost_micros / 1_000_000).toFixed(6)}, ${trace.duration_ms} ms`
    : 'trace metadata is not available yet.'}</Text>;
}

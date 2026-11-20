import {useEffect, useRef, useState} from 'react';
import {Send, Square, Trash2} from 'lucide-react';
import {listRoutesForEnvironment} from '../../api/managementClient';
import {createPlaygroundSession, getTraceWaterfall} from '../../api/managementClient';
import type {components} from '../../api/generated';
import {useRouter} from '../../router/RouterContext';
import {RouteTypeahead} from '../../ui/RouteTypeahead';
import {Banner, Button, CodeBlock, SectionHeader, StatusPill, Tabs, TextAreaField, TextField, formatMicroUSD, formatOperationalState, stateTone} from '../../ui/primitives';

type Role = components['schemas']['RbacRole'];
type Trace = components['schemas']['TraceSummary'];
type Turn = {role: 'user' | 'assistant'; content: string};

const allowedRoles: Role[] = ['OPERATOR', 'ORG_OWNER', 'TEAM_ADMIN', 'DEVELOPER'];

export function PlaygroundPage({
  pathname,
  projectId,
  environmentId,
  roles,
}: {
  pathname: string;
  projectId: string;
  environmentId: string;
  roles: Role[];
}) {
  const {navigate} = useRouter();
  const activeTab = pathname.startsWith('/devtools/sdk-snippets') ? 'snippets' : 'chat';

  const [availableRoutes, setAvailableRoutes] = useState<string[]>([]);
  const [model, setModel] = useState('');
  const [compareModel, setCompareModel] = useState('');
  const [budget, setBudget] = useState(500000);
  const [prompt, setPrompt] = useState('');
  const [messages, setMessages] = useState<Turn[]>([]);
  const [comparison, setComparison] = useState<{primary: string; secondary: string} | null>(null);
  const [primaryReqId, setPrimaryReqId] = useState('');
  const [compareReqId, setCompareReqId] = useState('');
  const [primaryTrace, setPrimaryTrace] = useState<Trace | null>(null);
  const [compareTrace, setCompareTrace] = useState<Trace | null>(null);
  const [session, setSession] = useState<components['schemas']['PlaygroundSessionResponse'] | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const abortRef = useRef<AbortController | null>(null);

  const authorized = roles.some(role => allowedRoles.includes(role));

  useEffect(() => {
    if (!environmentId) return;
    listRoutesForEnvironment(environmentId)
      .then(rows => {
        const slugs = rows.map(r => r.slug);
        setAvailableRoutes(slugs);
        setModel(curr => curr || slugs[0] || '');
      })
      .catch(() => {
        // Manual model entry remains available.
      });
  }, [environmentId]);

  async function streamCompletion(
    ephemeralKey: string,
    targetModel: string,
    history: Turn[],
    signal: AbortSignal,
    onDelta: (fullText: string) => void,
  ): Promise<{text: string; requestId: string}> {
    const response = await fetch('/v1/chat/completions', {
      method: 'POST',
      cache: 'no-store',
      credentials: 'omit',
      signal,
      headers: {
        Authorization: `Bearer ${ephemeralKey}`,
        'Content-Type': 'application/json',
        Accept: 'text/event-stream',
      },
      body: JSON.stringify({model: targetModel, messages: history, stream: true}),
    });
    if (!response.ok || !response.body) {
      throw new Error(`Playground inference failed (${response.status}).`);
    }
    const requestId = response.headers.get('X-Tanpopo-Request-Id')?.trim() ?? '';
    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    let buffer = '';
    let text = '';
    while (true) {
      const chunk = await reader.read();
      if (chunk.done) break;
      buffer += decoder.decode(chunk.value, {stream: true});
      const lines = buffer.split('\n');
      buffer = lines.pop() ?? '';
      for (const line of lines) {
        if (!line.startsWith('data:')) continue;
        const payload = line.slice(5).trim();
        if (!payload || payload === '[DONE]') continue;
        const parsed = JSON.parse(payload) as {choices?: Array<{delta?: {content?: string}}>};
        text += parsed.choices?.[0]?.delta?.content ?? '';
        onDelta(text);
      }
    }
    return {text, requestId};
  }

  async function handleSend() {
    if (!projectId || !environmentId || !model.trim() || !prompt.trim() || busy) return;
    setBusy(true);
    setError('');
    setNotice('');
    setComparison(null);
    setPrimaryReqId('');
    setCompareReqId('');
    setPrimaryTrace(null);
    setCompareTrace(null);

    const controller = new AbortController();
    abortRef.current = controller;
    const nextHistory: Turn[] = [...messages, {role: 'user', content: prompt.trim()}];
    setMessages([...nextHistory, {role: 'assistant', content: ''}]);
    setPrompt('');

    try {
      const activeSession =
        session ??
        (await createPlaygroundSession({
          project_id: projectId,
          env_id: environmentId,
          ttl_seconds: 900,
          max_budget_micros: budget,
        }));
      setSession(activeSession);

      const primary = await streamCompletion(
        activeSession.ephemeral_virtual_key,
        model.trim(),
        nextHistory,
        controller.signal,
        text => setMessages([...nextHistory, {role: 'assistant', content: text}]),
      );
      setMessages([...nextHistory, {role: 'assistant', content: primary.text}]);
      setPrimaryReqId(primary.requestId);
      if (primary.requestId) setPrimaryTrace(await pollTrace(primary.requestId));

      if (compareModel.trim() && compareModel.trim() !== model.trim()) {
        setComparison({primary: primary.text, secondary: ''});
        const secondary = await streamCompletion(
          activeSession.ephemeral_virtual_key,
          compareModel.trim(),
          nextHistory,
          controller.signal,
          text => setComparison({primary: primary.text, secondary: text}),
        );
        setComparison({primary: primary.text, secondary: secondary.text});
        setCompareReqId(secondary.requestId);
        if (secondary.requestId) setCompareTrace(await pollTrace(secondary.requestId));
      }
    } catch (cause) {
      if (!(cause instanceof DOMException && cause.name === 'AbortError')) {
        setError(cause instanceof Error ? cause.message : 'Playground request failed.');
      }
    } finally {
      abortRef.current = null;
      setBusy(false);
    }
  }

  if (!authorized) {
    return <Banner tone="danger" title="Access restricted">Your role cannot create Playground sessions.</Banner>;
  }

  const origin = typeof window === 'undefined' ? 'http://localhost:8080' : window.location.origin;
  const snippetModel = model.trim() || 'gpt-4o-mini';

  return (
    <div className="tp-stack">
      <div className="tp-page-head">
        <div className="tp-page-head-text">
          <h1>Playground</h1>
          <p className="tp-page-subtitle">Ephemeral session key with a hard micro-USD budget cap. Prompts stay in this browser tab only.</p>
        </div>
        {session ? (
          <div className="tp-page-actions">
            <span className="tp-muted mono">
              {`Cap ${formatMicroUSD(session.max_budget_micros)} · expires ${new Date(session.expires_at).toLocaleTimeString()}`}
            </span>
            <Button
              variant="secondary"
              size="sm"
              disabled={busy}
              onClick={() => {
                setSession(null);
                setMessages([]);
                setComparison(null);
                setNotice('Ephemeral session key cleared from this tab. Signing out revokes active Playground keys.');
              }}
            >
              <Trash2 size={14} aria-hidden="true" />
              <span>End session</span>
            </Button>
          </div>
        ) : null}
      </div>

      <Tabs
        label="Developer tools"
        activeId={activeTab}
        onSelect={id => navigate(id === 'snippets' ? '/devtools/sdk-snippets' : '/devtools/playground')}
        items={[
          {id: 'chat', label: 'Live Playground'},
          {id: 'snippets', label: 'SDK Snippets'},
        ]}
      />

      {!environmentId ? <Banner tone="info">Select an active environment in the top bar to start a Playground session.</Banner> : null}
      {error ? <Banner tone="danger">{error}</Banner> : null}
      {notice ? <Banner tone="ok">{notice}</Banner> : null}

      {activeTab === 'chat' ? (
        <div className="tp-stack">
          <div className="tp-surface tp-surface-pad tp-stack">
            <div className="tp-grid-3">
              <RouteTypeahead
                label="Model or route slug"
                suggestions={availableRoutes}
                placeholder="e.g. primary-chat"
                value={model}
                disabled={busy}
                onChange={setModel}
              />
              <RouteTypeahead
                label="Comparison model (optional)"
                suggestions={availableRoutes}
                placeholder="Side-by-side compare"
                value={compareModel}
                disabled={busy}
                onChange={setCompareModel}
              />
              {!session ? (
                <TextField
                  label="Session budget cap (micro-USD)"
                  type="number"
                  min={1000}
                  max={5000000}
                  value={budget}
                  disabled={busy}
                  error={budget < 1000 || budget > 5000000 ? 'Range: 1,000 to 5,000,000 micro-USD.' : undefined}
                  onChange={event => setBudget(Number(event.target.value) || 0)}
                />
              ) : null}
            </div>

            <section className="tp-section" aria-label="Conversation transcript">
              {messages.length === 0 ? (
                <p className="tp-muted">No turns yet. Send a prompt below to mint an ephemeral key and stream from `/v1/chat/completions`.</p>
              ) : (
                <ol className="tp-list">
                  {messages.map((turn, index) => (
                    <li key={`${turn.role}-${index}`} className="tp-list-item" style={{flexDirection: 'column', alignItems: 'stretch'}}>
                      <span className="tp-label">{turn.role === 'user' ? 'You' : model || 'Assistant'}</span>
                      <div style={{whiteSpace: 'pre-wrap', overflowWrap: 'anywhere'}}>
                        {turn.content || (busy && turn.role === 'assistant' ? 'Streaming…' : '')}
                      </div>
                    </li>
                  ))}
                </ol>
              )}
            </section>

            {comparison ? (
              <div className="tp-grid-2">
                <div className="tp-surface tp-surface-pad tp-stack-sm">
                  <h3>{model}</h3>
                  <div style={{whiteSpace: 'pre-wrap', overflowWrap: 'anywhere'}}>{comparison.primary}</div>
                </div>
                <div className="tp-surface tp-surface-pad tp-stack-sm">
                  <h3>{compareModel}</h3>
                  <div style={{whiteSpace: 'pre-wrap', overflowWrap: 'anywhere'}}>{comparison.secondary || 'Streaming comparison…'}</div>
                </div>
              </div>
            ) : null}

            <TextAreaField
              id="playground-prompt"
              label="Prompt"
              rows={4}
              placeholder="Enter a test prompt…"
              value={prompt}
              disabled={busy}
              onChange={event => setPrompt(event.target.value)}
            />

            <div className="tp-row">
              <Button
                variant="primary"
                loading={busy}
                disabled={!environmentId || !model.trim() || !prompt.trim() || (!session && (budget < 1000 || budget > 5000000))}
                onClick={() => void handleSend()}
              >
                <Send size={14} aria-hidden="true" />
                <span>{busy ? 'Streaming…' : 'Send'}</span>
              </Button>
              {busy ? (
                <Button variant="secondary" onClick={() => abortRef.current?.abort()}>
                  <Square size={14} aria-hidden="true" />
                  <span>Cancel stream</span>
                </Button>
              ) : null}
            </div>
          </div>

          {primaryReqId || compareReqId ? (
            <section className="tp-section">
              <SectionHeader title="Execution traces" />
              <ul className="tp-list">
                {primaryReqId ? <TraceRow label="Primary" requestId={primaryReqId} trace={primaryTrace} /> : null}
                {compareReqId ? <TraceRow label="Comparison" requestId={compareReqId} trace={compareTrace} /> : null}
              </ul>
            </section>
          ) : null}
        </div>
      ) : (
        <div className="tp-stack">
          <CodeBlock
            title="cURL (/v1/chat/completions)"
            code={`curl ${origin}/v1/chat/completions \\\n  -H "Authorization: Bearer $TANPOPO_VIRTUAL_KEY" \\\n  -H "Content-Type: application/json" \\\n  -d '{"model":"${snippetModel}","messages":[{"role":"user","content":"Hello"}],"stream":true}'`}
          />
          <CodeBlock
            title="Python (OpenAI SDK)"
            code={`from openai import OpenAI\n\nclient = OpenAI(base_url="${origin}/v1", api_key=os.environ["TANPOPO_VIRTUAL_KEY"])\nstream = client.chat.completions.create(\n    model="${snippetModel}",\n    messages=[{"role": "user", "content": "Hello"}],\n    stream=True,\n)\nfor chunk in stream:\n    print(chunk.choices[0].delta.content or "", end="")`}
          />
          <CodeBlock
            title="TypeScript (OpenAI SDK)"
            code={`import OpenAI from "openai";\n\nconst client = new OpenAI({ baseURL: "${origin}/v1", apiKey: process.env.TANPOPO_VIRTUAL_KEY });\nconst res = await client.chat.completions.create({\n  model: "${snippetModel}",\n  messages: [{ role: "user", content: "Hello" }],\n  stream: true,\n});`}
          />
        </div>
      )}
    </div>
  );
}

function TraceRow({label, requestId, trace}: {label: string; requestId: string; trace: Trace | null}) {
  return (
    <li className="tp-list-item">
      <div className="tp-stack-sm" style={{minWidth: 0, flex: '1 1 220px'}}>
        <span style={{fontWeight: 600}}>{label} · <span className="mono">{requestId}</span></span>
        <span className="tp-muted mono">
          {trace
            ? `route ${trace.route_slug} · HTTP ${trace.status_code} · ${formatMicroUSD(trace.total_cost_micros)} · ${trace.duration_ms} ms`
            : 'Trace metadata pending…'}
        </span>
      </div>
      {trace ? <StatusPill tone={stateTone(trace.usage_accuracy)} label={formatOperationalState(trace.usage_accuracy)} /> : null}
    </li>
  );
}

async function pollTrace(requestId: string): Promise<Trace | null> {
  for (let attempt = 0; attempt < 5; attempt += 1) {
    try {
      return await getTraceWaterfall(requestId);
    } catch (cause) {
      const status = cause && typeof cause === 'object' && 'status' in cause ? Number(cause.status) : 0;
      if (status !== 404 || attempt === 4) return null;
      await new Promise(resolve => window.setTimeout(resolve, 150));
    }
  }
  return null;
}

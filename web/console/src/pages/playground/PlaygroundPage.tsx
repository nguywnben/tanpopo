import {useEffect, useRef, useState} from 'react';
import {Send, Square, Trash2} from 'lucide-react';
import {createPlaygroundSession, getTraceWaterfall, listRoutesForEnvironment} from '../../api/managementClient';
import type {components} from '../../api/generated';
import {useI18n} from '../../i18n/I18nProvider';
import {useRouter} from '../../router/RouterContext';
import {RouteTypeahead} from '../../ui/RouteTypeahead';
import {Banner, Button, CodeBlock, SectionHeader, StatusPill, Tabs, TextAreaField, TextField, formatOperationalState, stateTone} from '../../ui/primitives';

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
  const {t, formatNumber, formatTime, formatMicroUSD} = useI18n();
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
      throw new Error(t('playground.inferenceFailed', {status: response.status}));
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
        setError(cause instanceof Error ? cause.message : t('playground.requestFailed'));
      }
    } finally {
      abortRef.current = null;
      setBusy(false);
    }
  }

  if (!authorized) {
    return <Banner tone="danger" title={t('common.accessRestricted')}>{t('playground.restrictedDesc')}</Banner>;
  }

  const origin = typeof window === 'undefined' ? 'http://localhost:8080' : window.location.origin;
  const snippetModel = model.trim() || 'gpt-4o-mini';

  return (
    <div className="tp-stack">
      <div className="tp-page-head">
        <div className="tp-page-head-text">
          <h1>{t('playground.title')}</h1>
          <p className="tp-page-subtitle">{t('playground.subtitle')}</p>
        </div>
        {session ? (
          <div className="tp-page-actions">
            <span className="tp-muted mono">
              {t('playground.sessionCap', {cap: formatMicroUSD(session.max_budget_micros), time: formatTime(session.expires_at)})}
            </span>
            <Button
              variant="secondary"
              size="sm"
              disabled={busy}
              onClick={() => {
                setSession(null);
                setMessages([]);
                setComparison(null);
                setNotice(t('playground.sessionCleared'));
              }}
            >
              <Trash2 size={14} aria-hidden="true" />
              <span>{t('playground.endSession')}</span>
            </Button>
          </div>
        ) : null}
      </div>

      <Tabs
        label={t('playground.tabsLabel')}
        activeId={activeTab}
        onSelect={id => navigate(id === 'snippets' ? '/devtools/sdk-snippets' : '/devtools/playground')}
        items={[
          {id: 'chat', label: t('playground.tabChat')},
          {id: 'snippets', label: t('playground.tabSnippets')},
        ]}
      />

      {!environmentId ? <Banner tone="info">{t('playground.selectEnvBanner')}</Banner> : null}
      {error ? <Banner tone="danger">{error}</Banner> : null}
      {notice ? <Banner tone="ok">{notice}</Banner> : null}

      {activeTab === 'chat' ? (
        <div className="tp-stack">
          <div className="tp-surface tp-surface-pad tp-stack">
            <div className="tp-grid-3">
              <RouteTypeahead
                label={t('playground.modelLabel')}
                suggestions={availableRoutes}
                placeholder={t('playground.modelPlaceholder')}
                value={model}
                disabled={busy}
                onChange={setModel}
              />
              <RouteTypeahead
                label={t('playground.compareLabel')}
                suggestions={availableRoutes}
                placeholder={t('playground.comparePlaceholder')}
                value={compareModel}
                disabled={busy}
                onChange={setCompareModel}
              />
              {!session ? (
                <TextField
                  label={t('playground.budgetCapLabel')}
                  type="number"
                  min={1000}
                  max={5000000}
                  value={budget}
                  disabled={busy}
                  error={budget < 1000 || budget > 5000000 ? t('playground.budgetCapError') : undefined}
                  onChange={event => setBudget(Number(event.target.value) || 0)}
                />
              ) : null}
            </div>

            <section className="tp-section" aria-label={t('playground.transcriptAria')}>
              {messages.length === 0 ? (
                <p className="tp-muted">{t('playground.noTurns')}</p>
              ) : (
                <ol className="tp-list">
                  {messages.map((turn, index) => (
                    <li key={`${turn.role}-${index}`} className="tp-list-item" style={{flexDirection: 'column', alignItems: 'stretch'}}>
                      <span className="tp-label">{turn.role === 'user' ? t('playground.you') : model || t('playground.assistant')}</span>
                      <div style={{whiteSpace: 'pre-wrap', overflowWrap: 'anywhere'}}>
                        {turn.content || (busy && turn.role === 'assistant' ? t('playground.streaming') : '')}
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
                  <div style={{whiteSpace: 'pre-wrap', overflowWrap: 'anywhere'}}>{comparison.secondary || t('playground.streamingComparison')}</div>
                </div>
              </div>
            ) : null}

            <TextAreaField
              id="playground-prompt"
              label={t('playground.promptLabel')}
              rows={4}
              placeholder={t('playground.promptPlaceholder')}
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
                <span>{busy ? t('playground.streaming') : t('playground.send')}</span>
              </Button>
              {busy ? (
                <Button variant="secondary" onClick={() => abortRef.current?.abort()}>
                  <Square size={14} aria-hidden="true" />
                  <span>{t('playground.cancelStream')}</span>
                </Button>
              ) : null}
            </div>
          </div>

          {primaryReqId || compareReqId ? (
            <section className="tp-section">
              <SectionHeader title={t('playground.executionTraces')} />
              <ul className="tp-list">
                {primaryReqId ? <TraceRow label={t('playground.primary')} requestId={primaryReqId} trace={primaryTrace} formatMicroUSD={formatMicroUSD} formatNumber={formatNumber} t={t} /> : null}
                {compareReqId ? <TraceRow label={t('playground.comparison')} requestId={compareReqId} trace={compareTrace} formatMicroUSD={formatMicroUSD} formatNumber={formatNumber} t={t} /> : null}
              </ul>
            </section>
          ) : null}
        </div>
      ) : (
        <div className="tp-stack">
          <CodeBlock
            title={t('playground.snippetCurl')}
            code={`curl ${origin}/v1/chat/completions \\\n  -H "Authorization: Bearer $TANPOPO_VIRTUAL_KEY" \\\n  -H "Content-Type: application/json" \\\n  -d '{"model":"${snippetModel}","messages":[{"role":"user","content":"Hello"}],"stream":true}'`}
          />
          <CodeBlock
            title={t('playground.snippetPython')}
            code={`from openai import OpenAI\n\nclient = OpenAI(base_url="${origin}/v1", api_key=os.environ["TANPOPO_VIRTUAL_KEY"])\nstream = client.chat.completions.create(\n    model="${snippetModel}",\n    messages=[{"role": "user", "content": "Hello"}],\n    stream=True,\n)\nfor chunk in stream:\n    print(chunk.choices[0].delta.content or "", end="")`}
          />
          <CodeBlock
            title={t('playground.snippetTs')}
            code={`import OpenAI from "openai";\n\nconst client = new OpenAI({ baseURL: "${origin}/v1", apiKey: process.env.TANPOPO_VIRTUAL_KEY });\nconst res = await client.chat.completions.create({\n  model: "${snippetModel}",\n  messages: [{ role: "user", content: "Hello" }],\n  stream: true,\n});`}
          />
        </div>
      )}
    </div>
  );
}

function TraceRow({
  label,
  requestId,
  trace,
  formatMicroUSD,
  formatNumber,
  t,
}: {
  label: string;
  requestId: string;
  trace: Trace | null;
  formatMicroUSD: (v: number) => string;
  formatNumber: (v: number) => string;
  t: (key: string, vars?: Record<string, string | number>) => string;
}) {
  return (
    <li className="tp-list-item">
      <div className="tp-stack-sm" style={{minWidth: 0, flex: '1 1 220px'}}>
        <span style={{fontWeight: 600}}>{label} · <span className="mono">{requestId}</span></span>
        <span className="tp-muted mono">
          {trace
            ? t('playground.traceMeta', {
                route: trace.route_slug,
                status: trace.status_code,
                cost: formatMicroUSD(trace.total_cost_micros),
                ms: formatNumber(trace.duration_ms),
              })
            : t('playground.tracePending')}
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

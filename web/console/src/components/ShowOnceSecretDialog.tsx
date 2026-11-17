import {useState} from 'react';
import {Button} from '@astryxdesign/core/Button';
import {CheckboxInput} from '@astryxdesign/core/CheckboxInput';
import {Dialog, DialogHeader} from '@astryxdesign/core/Dialog';
import {Text} from '@astryxdesign/core/Text';
import {VStack} from '@astryxdesign/core/Stack';
import type {components} from '../api/generated';

type Secret = components['schemas']['VirtualKeyCreatedResponse'];

export function ShowOnceSecretDialog({secret, model, onAcknowledge}: {secret: Secret; model: string; onAcknowledge: () => void}) {
  const [acknowledged, setAcknowledged] = useState(false);
  const [copyMessage, setCopyMessage] = useState('');
  const [testing, setTesting] = useState(false);
  const [testError, setTestError] = useState('');
  const [testResult, setTestResult] = useState<{text: string; requestId: string; latencyMs: number} | null>(null);
  async function copySecret() {
    try {
      await navigator.clipboard.writeText(secret.secret_key);
      setCopyMessage('Virtual key copied. Store it securely; it cannot be shown again.');
    } catch {
      setCopyMessage('Copy was unavailable. Select and copy the key, then store it securely.');
    }
  }
  async function sendTestRequest() {
    if (testing || !secret.secret_key) return;
    setTesting(true);
    setTestError('');
    setTestResult(null);
    const started = performance.now();
    try {
      const response = await fetch('/v1/chat/completions', {
        method: 'POST',
        headers: {'Authorization': `Bearer ${secret.secret_key}`, 'Content-Type': 'application/json'},
        body: JSON.stringify({model, messages: [{role: 'user', content: 'Reply with a short greeting.'}], stream: true, max_tokens: 32}),
      });
      if (!response.ok || !response.body) {
        const errorText = await response.text();
        throw new Error(extractErrorMessage(errorText, response.status));
      }
      const reader = response.body.getReader();
      const decoder = new TextDecoder();
      let buffer = '';
      let text = '';
      while (true) {
        const {done, value} = await reader.read();
        if (done) break;
        buffer += decoder.decode(value, {stream: true});
        const lines = buffer.split('\n');
        buffer = lines.pop() ?? '';
        for (const line of lines) {
          if (!line.startsWith('data:')) continue;
          const data = line.slice(5).trim();
          if (!data || data === '[DONE]') continue;
          try {
            const chunk = JSON.parse(data) as {choices?: Array<{delta?: {content?: string}}>};
            const delta = chunk.choices?.[0]?.delta?.content;
            if (delta) text += delta;
            setTestResult({text, requestId: response.headers.get('x-tanpopo-request-id') ?? 'not provided', latencyMs: Math.round(performance.now() - started)});
          } catch {
            throw new Error('The first request returned an unreadable streaming response.');
          }
        }
      }
      if (!text.trim()) throw new Error('The first request completed without a response. Check the model route and try again.');
      setTestResult({text, requestId: response.headers.get('x-tanpopo-request-id') ?? 'not provided', latencyMs: Math.round(performance.now() - started)});
    } catch (cause) {
      setTestError(cause instanceof Error ? cause.message : 'The first request failed. Check the model route and try again.');
    } finally {
      setTesting(false);
    }
  }
  return (
    <Dialog isOpen onOpenChange={() => {}} purpose="required" width={560} padding={5}>
      <VStack gap={4}>
        <DialogHeader title="Store your virtual key now" />
        <Text>This plaintext key is shown once. After you leave this screen, the server cannot retrieve it.</Text>
        <Text>{secret.secret_key}</Text>
        <Text>Key name: {secret.name} · Prefix: {secret.key_prefix} · Last four: {secret.last_four}</Text>
        <Button label="Copy virtual key" variant="secondary" onClick={copySecret} />
        {copyMessage ? <Text role="status">{copyMessage}</Text> : null}
        <Text>Send a live streaming test request to the default route ({model}) before finishing setup.</Text>
        <Text>{`curl http://localhost:8080/v1/chat/completions -H 'Authorization: Bearer ${secret.secret_key}' -H 'Content-Type: application/json' -d '{"model":"${model}","messages":[{"role":"user","content":"Hello"}],"stream":true}'`}</Text>
        <Text>{`Python: client.chat.completions.create(model="${model}", messages=[{"role":"user","content":"Hello"}], stream=True)`}</Text>
        <Text>{`TypeScript: await client.chat.completions.create({model: "${model}", messages: [{role: "user", content: "Hello"}], stream: true})`}</Text>
        <Button label={testing ? 'Sending test request…' : 'Send Test Request'} variant="primary" isLoading={testing} onClick={sendTestRequest} />
        {testError ? <Text role="alert">{testError}</Text> : null}
        {testResult ? <VStack gap={1}>
          <Text role="status">First request succeeded in {testResult.latencyMs} ms · Request ID: {testResult.requestId}</Text>
          <Text>{testResult.text}</Text>
        </VStack> : null}
        <CheckboxInput label="I have securely stored this virtual key" value={acknowledged} onChange={setAcknowledged} />
        <Button label="Continue to Overview" variant="primary" isDisabled={!acknowledged || !testResult || testing} onClick={onAcknowledge} />
      </VStack>
    </Dialog>
  );
}

function extractErrorMessage(body: string, status: number): string {
  try {
    const parsed = JSON.parse(body) as {error?: {message?: string}};
    if (parsed.error?.message) return parsed.error.message;
  } catch { /* use the canonical status fallback */ }
  return `The first request failed (${status}). Check the route, model deployment, and virtual key scope.`;
}

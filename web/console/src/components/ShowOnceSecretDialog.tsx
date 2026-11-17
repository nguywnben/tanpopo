import {useState} from 'react';
import {Button} from '@astryxdesign/core/Button';
import {CheckboxInput} from '@astryxdesign/core/CheckboxInput';
import {Dialog, DialogHeader} from '@astryxdesign/core/Dialog';
import {Text} from '@astryxdesign/core/Text';
import {VStack} from '@astryxdesign/core/Stack';
import type {components} from '../api/generated';

type Secret = components['schemas']['VirtualKeyCreatedResponse'];

export function ShowOnceSecretDialog({secret, onAcknowledge}: {secret: Secret; onAcknowledge: () => void}) {
  const [acknowledged, setAcknowledged] = useState(false);
  const [copyMessage, setCopyMessage] = useState('');
  async function copySecret() {
    try {
      await navigator.clipboard.writeText(secret.secret_key);
      setCopyMessage('Virtual key copied. Store it securely; it cannot be shown again.');
    } catch {
      setCopyMessage('Copy was unavailable. Select and copy the key, then store it securely.');
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
        <CheckboxInput label="I have securely stored this virtual key" value={acknowledged} onChange={setAcknowledged} />
        <Button label="Continue to Overview" variant="primary" isDisabled={!acknowledged} onClick={onAcknowledge} />
      </VStack>
    </Dialog>
  );
}

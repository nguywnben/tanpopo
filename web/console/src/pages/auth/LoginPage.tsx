import {useState, type FormEvent} from 'react';
import {Banner} from '@astryxdesign/core/Banner';
import {Button} from '@astryxdesign/core/Button';
import {Card} from '@astryxdesign/core/Card';
import {Center} from '@astryxdesign/core/Center';
import {FormLayout} from '@astryxdesign/core/FormLayout';
import {Heading, Text} from '@astryxdesign/core/Text';
import {VStack} from '@astryxdesign/core/Stack';
import {TextInput} from '@astryxdesign/core/TextInput';
import {loginAdmin} from '../../api/client';

export function LoginPage() {
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy || !email.trim() || !password) return;
    setBusy(true);
    setError('');
    try {
      await loginAdmin(email.trim(), password);
      setPassword('');
      window.location.assign('/overview');
    } catch (cause) {
      setPassword('');
      setError(loginError(cause));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Center padding={4}>
      <Card padding={6} width="100%" maxWidth={440}>
        <form onSubmit={submit}>
          <VStack gap={4}>
            <VStack gap={1}>
              <Heading level={1}>Sign in to Tanpopo</Heading>
              <Text>Use your local administrator account to open the console.</Text>
            </VStack>
            {error ? <Banner status="error" title="Sign-in failed" description={error} collapsible={false} /> : null}
            <FormLayout>
              <TextInput label="Email address" type="email" autoComplete="username" value={email} onChange={setEmail} />
              <TextInput label="Password" type="password" autoComplete="current-password" value={password} onChange={setPassword} />
            </FormLayout>
            <Button label="Sign in" variant="primary" type="submit" isLoading={busy} />
            <Text>First time here? <a href="/setup">Set up this Tanpopo instance</a></Text>
          </VStack>
        </form>
      </Card>
    </Center>
  );
}

function loginError(cause: unknown): string {
  if (cause instanceof Error) {
    const code = 'code' in cause && typeof cause.code === 'string' ? cause.code : '';
    if (code === 'auth_rate_limited') return 'Too many attempts. Wait before trying again.';
    if (code === 'invalid_credentials') return 'The email or password is not correct.';
    return cause.message;
  }
  return 'The request failed. Check the server connection and try again.';
}

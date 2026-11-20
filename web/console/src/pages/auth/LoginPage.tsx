import {useState, type FormEvent} from 'react';
import {Moon, Sun} from 'lucide-react';
import {loginAdmin} from '../../api/client';
import {Link, useRouter} from '../../router/RouterContext';
import {useTheme} from '../../styles/theme';
import {Banner, Button, IconButton, TanpopoMark, TextField} from '../../ui/primitives';

export function LoginPage() {
  const {navigate} = useRouter();
  const {theme, toggleTheme} = useTheme();
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy || !email.trim() || !password) return;
    setBusy(true);
    setError('');
    try {
      await loginAdmin(email.trim(), password);
      setPassword('');
      navigate('/overview', {replace: true});
    } catch (cause) {
      setPassword('');
      setError(formatLoginError(cause));
    } finally {
      setBusy(false);
    }
  }

  return (
    <main className="tp-auth-page">
      <div className="tp-auth-panel">
        <div className="tp-auth-head">
          <div className="tp-row">
            <TanpopoMark size={24} />
            <span style={{fontWeight: 700, fontSize: '1rem', letterSpacing: '-0.02em'}}>Tanpopo</span>
          </div>
          <IconButton label={theme === 'dark' ? 'Switch to light theme' : 'Switch to dark theme'} onClick={toggleTheme}>
            {theme === 'dark' ? <Sun size={16} aria-hidden="true" /> : <Moon size={16} aria-hidden="true" />}
          </IconButton>
        </div>

        <div className="tp-stack-sm">
          <h1>Sign in</h1>
          <p className="tp-muted">Use your local administrator credentials to access the control plane.</p>
        </div>

        {error ? <Banner tone="danger" title="Sign-in failed">{error}</Banner> : null}

        <form className="tp-stack" onSubmit={handleSubmit}>
          <TextField
            label="Email address"
            type="email"
            autoComplete="username"
            required
            value={email}
            disabled={busy}
            onChange={event => setEmail(event.target.value)}
          />
          <TextField
            label="Password"
            type="password"
            autoComplete="current-password"
            required
            value={password}
            disabled={busy}
            onChange={event => setPassword(event.target.value)}
          />
          <Button type="submit" variant="primary" loading={busy} disabled={!email.trim() || !password}>
            {busy ? 'Signing in…' : 'Sign in'}
          </Button>
        </form>

        <hr className="tp-divider" />
        <p className="tp-muted">
          New instance? <Link href="/setup" className="tp-link">Run first-time setup</Link>
        </p>
      </div>
    </main>
  );
}

function formatLoginError(cause: unknown): string {
  if (cause instanceof Error) {
    const code = 'code' in cause && typeof cause.code === 'string' ? cause.code : '';
    if (code === 'auth_rate_limited') return 'Too many sign-in attempts. Wait before trying again.';
    if (code === 'invalid_credentials') return 'The email address or password is not valid.';
    return cause.message;
  }
  return 'Could not reach the Tanpopo control plane.';
}

import {useState, type FormEvent} from 'react';
import {Moon, Sun} from 'lucide-react';
import {loginAdmin} from '../../api/client';
import {useI18n} from '../../i18n/I18nProvider';
import {LanguageSelector} from '../../i18n/LanguageSelector';
import {Link, useRouter} from '../../router/RouterContext';
import {useTheme} from '../../styles/theme';
import {Banner, Button, IconButton, TextField} from '../../ui/primitives';
import {TanpopoBrand} from '../../ui/TanpopoBrand';

export function LoginPage() {
  const {navigate} = useRouter();
  const {theme, toggleTheme} = useTheme();
  const {t} = useI18n();
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
      setError(formatLoginError(cause, t));
    } finally {
      setBusy(false);
    }
  }

  return (
    <main className="tp-auth-page">
      <div className="tp-auth-panel">
        <div className="tp-auth-head">
          <TanpopoBrand className="tp-auth-brand" />
          <div className="tp-auth-controls">
            <LanguageSelector id="login-language-selector" />
            <IconButton variant="secondary" label={theme === 'dark' ? t('common.switchToLightTheme') : t('common.switchToDarkTheme')} onClick={toggleTheme}>
              {theme === 'dark' ? <Sun size={15} aria-hidden="true" /> : <Moon size={15} aria-hidden="true" />}
            </IconButton>
          </div>
        </div>

        <div className="tp-stack-sm">
          <h1>{t('login.title')}</h1>
          <p className="tp-muted">{t('login.subtitle')}</p>
        </div>

        {error ? <Banner tone="danger" title={t('login.failedTitle')}>{error}</Banner> : null}

        <form className="tp-stack" onSubmit={handleSubmit}>
          <TextField
            label={t('login.emailLabel')}
            type="email"
            autoComplete="username"
            required
            value={email}
            disabled={busy}
            onChange={event => setEmail(event.target.value)}
          />
          <TextField
            label={t('login.passwordLabel')}
            type="password"
            autoComplete="current-password"
            required
            value={password}
            disabled={busy}
            onChange={event => setPassword(event.target.value)}
          />
          <Button type="submit" variant="primary" loading={busy} disabled={!email.trim() || !password}>
            {busy ? t('login.submitting') : t('login.submit')}
          </Button>
        </form>

        <hr className="tp-divider" />
        <p className="tp-muted">
          {t('login.newInstance')} <Link href="/setup" className="tp-link">{t('login.runSetup')}</Link>
        </p>
      </div>
    </main>
  );
}

function formatLoginError(cause: unknown, t: (key: string) => string): string {
  if (cause instanceof Error) {
    const code = 'code' in cause && typeof cause.code === 'string' ? cause.code : '';
    if (code === 'auth_rate_limited') return t('login.errRateLimited');
    if (code === 'invalid_credentials') return t('login.errInvalidCredentials');
    return cause.message;
  }
  return t('login.errUnreachable');
}

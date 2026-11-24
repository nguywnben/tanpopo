import {useEffect, useState, type FormEvent} from 'react';
import {Moon, Sun} from 'lucide-react';
import {enrollLocalMember, loginAdmin} from '../../api/client';
import {useI18n} from '../../i18n/I18nProvider';
import {LanguageSelector} from '../../i18n/LanguageSelector';
import {Link, useRouter} from '../../router/RouterContext';
import {useTheme} from '../../styles/theme';
import {Banner, Button, IconButton, Tabs, TextField} from '../../ui/primitives';
import {TanpopoBrand} from '../../ui/TanpopoBrand';

function readLoginQueryParams(): {enrollToken: string; sessionExpired: boolean; hasUrlEnroll: boolean} {
  if (typeof window === 'undefined') return {enrollToken: '', sessionExpired: false, hasUrlEnroll: false};
  try {
    const sp = new URLSearchParams(window.location.search);
    const hp = new URLSearchParams(window.location.hash.replace(/^#/, ''));
    const hashToken = (hp.get('enroll') ?? '').trim();
    const queryToken = (sp.get('enroll') ?? '').trim();
    const enrollToken = hashToken || queryToken;
    return {
      enrollToken,
      sessionExpired: sp.get('reason') === 'session_expired',
      hasUrlEnroll: Boolean(hashToken || queryToken),
    };
  } catch {
    return {enrollToken: '', sessionExpired: false, hasUrlEnroll: false};
  }
}

export function LoginPage() {
  const {navigate} = useRouter();
  const {theme, toggleTheme} = useTheme();
  const {t} = useI18n();
  const [initialQuery] = useState(() => readLoginQueryParams());
  const [mode, setMode] = useState<'login' | 'enroll'>(initialQuery.enrollToken ? 'enroll' : 'login');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [enrollToken, setEnrollToken] = useState(initialQuery.enrollToken);
  const [enrollPassword, setEnrollPassword] = useState('');
  const [enrollConfirm, setEnrollConfirm] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    if (!initialQuery.hasUrlEnroll || typeof window === 'undefined') return;
    try {
      const sp = new URLSearchParams(window.location.search);
      sp.delete('enroll');
      const hp = new URLSearchParams(window.location.hash.replace(/^#/, ''));
      hp.delete('enroll');
      const nextSearch = sp.toString();
      const nextHash = hp.toString();
      const cleaned = `${window.location.pathname}${nextSearch ? `?${nextSearch}` : ''}${nextHash ? `#${nextHash}` : ''}`;
      window.history.replaceState(window.history.state, '', cleaned);
    } catch {
      // Ignore history errors in non-standard environments.
    }
  }, [initialQuery.hasUrlEnroll]);

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

  async function handleEnrollSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy || !enrollToken.trim() || !enrollPassword) return;
    if (enrollPassword.length < 12) {
      setError(t('login.errWeakPassword'));
      return;
    }
    if (enrollPassword !== enrollConfirm) {
      setError(t('login.errPasswordMismatch'));
      return;
    }
    setBusy(true);
    setError('');
    try {
      await enrollLocalMember(enrollToken.trim(), enrollPassword);
      setEnrollToken('');
      setEnrollPassword('');
      setEnrollConfirm('');
      navigate('/overview', {replace: true});
    } catch (cause) {
      setEnrollPassword('');
      setEnrollConfirm('');
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
          <h1>{mode === 'enroll' ? t('login.enrollTitle') : t('login.title')}</h1>
          <p className="tp-muted">{mode === 'enroll' ? t('login.enrollSubtitle') : t('login.subtitle')}</p>
        </div>

        <Tabs
          label={t('login.modeTabsLabel')}
          activeId={mode}
          onSelect={next => {
            setMode(next as 'login' | 'enroll');
            setError('');
          }}
          items={[
            {id: 'login', label: t('login.tabSignIn')},
            {id: 'enroll', label: t('login.tabEnroll')},
          ]}
        />

        {initialQuery.sessionExpired && !error ? (
          <Banner tone="warn" title={t('login.sessionExpiredTitle')}>{t('login.sessionExpiredDesc')}</Banner>
        ) : null}

        {error ? <Banner tone="danger" title={mode === 'enroll' ? t('login.enrollFailedTitle') : t('login.failedTitle')}>{error}</Banner> : null}

        {mode === 'login' ? (
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
        ) : (
          <form className="tp-stack" onSubmit={handleEnrollSubmit}>
            <TextField
              label={t('login.enrollTokenLabel')}
              type="text"
              autoComplete="one-time-code"
              required
              placeholder="tpen_..."
              value={enrollToken}
              disabled={busy}
              onChange={event => setEnrollToken(event.target.value)}
            />
            <TextField
              label={t('login.newPasswordLabel')}
              type="password"
              autoComplete="new-password"
              required
              hint={t('login.passwordMinLengthHint')}
              value={enrollPassword}
              disabled={busy}
              onChange={event => setEnrollPassword(event.target.value)}
            />
            <TextField
              label={t('login.confirmPasswordLabel')}
              type="password"
              autoComplete="new-password"
              required
              value={enrollConfirm}
              disabled={busy}
              onChange={event => setEnrollConfirm(event.target.value)}
            />
            <Button type="submit" variant="primary" loading={busy} disabled={!enrollToken.trim() || !enrollPassword || !enrollConfirm}>
              {busy ? t('login.enrolling') : t('login.enrollSubmit')}
            </Button>
          </form>
        )}

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
    if (code === 'invalid_enrollment_token') return t('login.errInvalidEnrollToken');
    if (code === 'weak_password') return t('login.errWeakPassword');
    return cause.message;
  }
  return t('login.errUnreachable');
}

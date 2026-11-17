import {Icon} from '@astryxdesign/core/Icon';
import {Token} from '@astryxdesign/core/Token';

const tierColor = {DECLARED: 'orange', TESTED: 'cyan', VERIFIED: 'green'} as const;
const trustColor = {official: 'blue', verified: 'teal', unverified_external: 'orange'} as const;

export function AssuranceToken({tier}: {tier: keyof typeof tierColor}) {
  const icon = tier === 'VERIFIED' ? 'checkDouble' : tier === 'TESTED' ? 'wrench' : 'info';
  return <Token label={tier === 'DECLARED' ? 'Self-declared' : tier === 'TESTED' ? 'Contract tested' : 'Verified'} color={tierColor[tier]} icon={<Icon icon={icon} size="sm" />} />;
}

export function TrustToken({trust}: {trust: keyof typeof trustColor}) {
  const icon = trust === 'official' ? 'checkDouble' : trust === 'verified' ? 'check' : 'warning';
  return <Token label={trust === 'official' ? 'Official origin' : trust === 'verified' ? 'Verified publisher' : 'Unverified external'} color={trustColor[trust]} icon={<Icon icon={icon} size="sm" />} />;
}

export function StateToken({state}: {state: string}) {
  const lower = state.toLowerCase();
  const color = lower.includes('active') || lower === 'healthy' ? 'green' : lower.includes('quarantined') || lower.includes('unhealthy') || lower === 'disabled' ? 'red' : 'yellow';
  const icon = color === 'green' ? 'success' : color === 'red' ? 'stop' : 'clock';
  return <Token label={state.replaceAll('_', ' ')} color={color} icon={<Icon icon={icon} size="sm" />} />;
}

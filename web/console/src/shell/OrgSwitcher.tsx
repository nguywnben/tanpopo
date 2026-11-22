import {Check, SlidersHorizontal} from 'lucide-react';
import type {components} from '../api/generated';
import {useI18n} from '../i18n/I18nProvider';
import {IconButton} from '../ui/primitives';

type Organization = components['schemas']['Organization'];

export function OrgSwitcher({
  organizations,
  activeOrgId,
  isOperator,
  multiOrgEnabled,
  onSelectOrg,
  onManageOrgs,
}: {
  organizations: Organization[];
  activeOrgId: string;
  isOperator: boolean;
  multiOrgEnabled: boolean;
  onSelectOrg: (orgId: string) => void;
  onManageOrgs: () => void;
}) {
  const {t} = useI18n();
  const visible = multiOrgEnabled && (organizations.length > 1 || isOperator);
  if (!visible) return null;

  return (
    <>
      <div className="tp-scope-menu-section">
        <div className="tp-scope-menu-head">
          <span>{t('common.organization')}</span>
          {isOperator ? (
            <IconButton
              label={t('orgs.manageOrganizations')}
              variant="ghost"
              size="sm"
              onClick={onManageOrgs}
            >
              <SlidersHorizontal size={14} aria-hidden="true" />
            </IconButton>
          ) : null}
        </div>
        <div className="tp-scope-menu-list" role="listbox" aria-label={t('common.organization')}>
          {organizations.map(org => {
            const selected = org.id === activeOrgId;
            return (
              <button
                key={org.id}
                type="button"
                role="option"
                aria-selected={selected}
                className="tp-scope-menu-item"
                data-selected={selected ? 'true' : 'false'}
                onClick={() => onSelectOrg(org.id)}
              >
                <span className="tp-scope-menu-item-label">{org.name}</span>
                {selected ? <Check size={14} strokeWidth={2.2} className="tp-scope-menu-check" aria-hidden="true" /> : null}
              </button>
            );
          })}
        </div>
      </div>
      <div className="tp-scope-menu-divider" role="separator" />
    </>
  );
}

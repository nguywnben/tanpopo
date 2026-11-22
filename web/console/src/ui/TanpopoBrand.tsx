import {Link} from '../router/RouterContext';
import {TanpopoMark} from './primitives';

export function TanpopoBrand({
  href,
  className = '',
}: {
  href?: string;
  className?: string;
}) {
  const classes = className ? `tp-brand ${className}` : 'tp-brand';
  const content = (
    <>
      <TanpopoMark size={20} />
      <span className="tp-brand-text">Tanpopo</span>
    </>
  );

  if (href) {
    return (
      <Link href={href} className={classes}>
        {content}
      </Link>
    );
  }

  return <div className={classes}>{content}</div>;
}

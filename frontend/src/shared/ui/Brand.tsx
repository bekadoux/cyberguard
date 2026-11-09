import { ShieldCheck } from 'lucide-react';

export function Brand() {
  return (
    <div className="brand" aria-label="CyberGuard — Incident management">
      <span className="brand-mark">
        <ShieldCheck aria-hidden="true" size={27} strokeWidth={1.7} />
      </span>
      <span>
        <span className="brand-name">CyberGuard</span>
        <span className="brand-description">Incident management</span>
      </span>
    </div>
  );
}

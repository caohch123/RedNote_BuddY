import { useEffect, useRef, useState, type CSSProperties } from 'react';
import { Button } from 'antd';
import GenerationSparkle from './GenerationSparkle';

const particles = [
  [22, 3, .92, -1.1], [67, 2, 1.15, -3.4], [35, 5, .85, -2.3], [78, 3, 1.1, -.6],
  [48, 2, 1, -4.2], [17, 4, 1.2, -1.8], [62, 3, .88, -2.9], [39, 2, 1.06, -.2],
  [73, 5, .96, -3.7], [27, 2, 1.16, -2.6], [54, 3, .82, -1.5], [81, 2, 1.04, -4.8],
];

export default function ParticleGenerateButton({ label, ariaLabel, disabled, accelerating = false, onGenerate }: {
  label: string; ariaLabel: string; disabled: boolean; accelerating?: boolean; onGenerate: () => Promise<void>;
}) {
  const inFlight = useRef(false), mounted = useRef(true);
  const [submitting, setSubmitting] = useState(false);
  const [visible, setVisible] = useState(true);
  useEffect(() => {
    mounted.current = true;
    const update = () => setVisible(!document.hidden);
    update(); document.addEventListener('visibilitychange', update);
    return () => { mounted.current = false; document.removeEventListener('visibilitychange', update); };
  }, []);
  async function generate() {
    if (disabled || accelerating || inFlight.current) return;
    inFlight.current = true; setSubmitting(true);
    try { await onGenerate(); }
    finally { inFlight.current = false; if (mounted.current) setSubmitting(false); }
  }
  const active = submitting || accelerating;
  return <Button type="primary" className="ai-generation-button particle-generate-button" aria-label={ariaLabel} aria-busy={active}
    data-accelerating={active ? 'true' : undefined} data-paused={!visible ? 'true' : undefined}
    icon={<GenerationSparkle />} loading={active} disabled={disabled || active} onClick={() => void generate()}>
    <span className="particle-generate-field" aria-hidden="true">{particles.map(([y, size, speed, delay], index) => <span key={index}
      className={`particle-generate-stream${index % 4 === 2 ? ' is-star' : ''}`}
      style={{ '--particle-y': `${y}%`, '--particle-size': `${size}px`, '--particle-speed': speed, '--particle-delay': `${delay}s` } as CSSProperties} />)}</span>
    <span className="particle-generate-label">{label}</span>
  </Button>;
}

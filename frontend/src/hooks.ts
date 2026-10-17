import { useCallback, useEffect, useRef, useState } from 'react';
import { api, ApiError, errorText } from './api';

export function useResource<T>(path: string | null, interval = 0) {
  const [data, setData] = useState<T>();
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const current = useRef(path); current.current = path;
  const inFlight = useRef(false);
  const signature = useRef('');
  const reload = useCallback(async (quiet = false) => {
    if (!path || inFlight.current) return;
    const target = path; inFlight.current = true;
    if (!quiet) setLoading(true);
    try {
      const result = await api<T>(target);
      if (current.current !== target) return;
      const encoded = JSON.stringify(result);
      if (signature.current !== encoded) { setData(result); signature.current = encoded; }
      setError('');
    } catch (e) { if (current.current === target) {
      if (e instanceof ApiError && e.status === 404) { setData(undefined); signature.current = ''; }
      setError(errorText(e));
    } }
    finally { inFlight.current = false; if (current.current === target) setLoading(false); }
  }, [path]);
  useEffect(() => {
    signature.current = ''; setData(undefined); inFlight.current = false;
    void reload();
    if (interval > 0) { const timer = setInterval(() => void reload(true), interval); return () => clearInterval(timer); }
  }, [reload, interval]);
  return { data, loading, error, reload };
}

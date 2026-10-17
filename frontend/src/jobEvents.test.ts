import { afterEach, describe, expect, it, vi } from 'vitest';
import { jobWasCleared, notifyJobsCleared } from './jobEvents';

afterEach(() => vi.unstubAllGlobals());
describe('cleared background jobs', () => {
  it('notifies only with the workspace and IDs that were actually cleared', () => {
    const target = new EventTarget(), receive = vi.fn();
    vi.stubGlobal('window', target);
    target.addEventListener('workbench:jobs-cleared', receive);
    notifyJobsCleared('xiaohongshu', ['clear-xhs', 'clear-xhs-queued']);
    expect(receive).toHaveBeenCalledTimes(1);
    expect((receive.mock.calls[0][0] as CustomEvent).detail).toEqual({ platform: 'xiaohongshu', ids: ['clear-xhs', 'clear-xhs-queued'] });
    expect(jobWasCleared('xiaohongshu', 'clear-xhs')).toBe(true);
    expect(jobWasCleared('xiaohongshu', 'not-cleared')).toBe(false);
  });
  it('remembers cleared receipts so a delayed submission response cannot relock the workbench', () => {
    vi.stubGlobal('window', new EventTarget());
    notifyJobsCleared('xiaohongshu', ['delayed']);
    notifyJobsCleared('xiaohongshu', []);
    expect(jobWasCleared('xiaohongshu', 'delayed')).toBe(true);
    expect(jobWasCleared('xiaohongshu', 'new-job')).toBe(false);
  });
});

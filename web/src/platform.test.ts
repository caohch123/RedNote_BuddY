import { describe, expect, it } from 'vitest';
import { validPlatformWorkURL, workspacePath } from './platform';

describe('platform identity and manual result links', () => {
  it('keeps filters and replaces an existing platform with the current workspace', () => {
    expect(workspacePath('/collections?q=%E6%96%87%E6%A1%88&page=2', 'xiaohongshu')).toBe('/collections?q=%E6%96%87%E6%A1%88&page=2&platform=xiaohongshu');
    expect(workspacePath('/assets/a/file?platform=douyin&download=1', 'xiaohongshu')).toBe('/assets/a/file?platform=xiaohongshu&download=1');
  });
  it('requires an exact platform host boundary and HTTPS without credentials or ports', () => {
    expect(validPlatformWorkURL('https://www.xiaohongshu.com/explore/123')).toBe(true);
    expect(validPlatformWorkURL('https://xhslink.com/a/abcde')).toBe(true);
    for (const url of ['https://xiaohongshu.com.evil.example/explore/123', 'https://evilxiaohongshu.com/explore/123', 'http://www.xiaohongshu.com/explore/123', 'https://user@www.xiaohongshu.com/explore/123', 'https://www.xiaohongshu.com:8443/explore/123', 'https://www.douyin.com/video/123']) {
      expect(validPlatformWorkURL(url)).toBe(false);
    }
  });
});

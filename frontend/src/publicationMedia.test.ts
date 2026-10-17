import { describe, expect, it } from 'vitest';
import { defaultPublicationCards, movePublicationMedia, reorderPublicationCards, togglePublicationMedia } from './publicationMedia';

describe('default publication media order', () => {
  it('keeps saved publication order, deduplicates candidates, and appends remaining source materials', () => {
    expect(defaultPublicationCards(['a1', 'a2', 'a3', 'a4', 'a1'], ['a2', 'a1', 'a3', 'missing']))
      .toEqual(['a2', 'a1', 'a3', 'a4']);
  });
  it('deselects and reselects without moving a card or changing unrelated selections', () => {
    const cards = ['a2', 'a1', 'a3', 'a4'];
    const original = ['a2', 'a1', 'a3'];
    const deselected = togglePublicationMedia(original, 'a1', false, cards);
    expect(deselected).toEqual(['a2', 'a3']);
    expect(togglePublicationMedia(deselected, 'a1', true, cards)).toEqual(original);
    expect(togglePublicationMedia(original, 'a4', true, cards)).toEqual(['a2', 'a1', 'a3', 'a4']);
    expect(cards).toEqual(['a2', 'a1', 'a3', 'a4']);
    expect(original).toEqual(['a2', 'a1', 'a3']);
  });
  it('keeps selected assets outside the current candidates and supports selecting none', () => {
    expect(togglePublicationMedia(['a1', 'outside'], 'a1', false, ['a1', 'a2'])).toEqual(['outside']);
    expect(togglePublicationMedia(['a1'], 'a1', false, ['a1', 'a2'])).toEqual([]);
  });
});

describe('publication media sorting', () => {
  it('moves forwards and backwards without changing selections or the saved source array', () => {
    const saved = ['a2', 'a1', 'a3'];
    const moved = movePublicationMedia(saved, 'a2', 'a3');
    expect(moved).toEqual(['a1', 'a3', 'a2']);
    expect(movePublicationMedia(moved, 'a2', 'a1')).toEqual(saved);
    expect(new Set(moved)).toEqual(new Set(saved));
    expect(saved).toEqual(['a2', 'a1', 'a3']);
  });
  it('keeps unchecked card slots and outside references when sorting visible selections', () => {
    const cards = ['a2', 'unchecked', 'a1', 'a3', 'new-upload'];
    const moved = movePublicationMedia(['a2', 'outside', 'a1', 'a3'], 'a3', 'a2');
    expect(moved).toEqual(['a3', 'a2', 'outside', 'a1']);
    expect(reorderPublicationCards(cards, moved)).toEqual(['a3', 'unchecked', 'a2', 'a1', 'new-upload']);
    expect(cards).toEqual(['a2', 'unchecked', 'a1', 'a3', 'new-upload']);
  });
  it('uses the new card order when toggling after a drag, saving, and reopening', () => {
    const moved = movePublicationMedia(['a2', 'a1', 'a3'], 'a3', 'a2');
    const cards = reorderPublicationCards(['a2', 'a1', 'a3', 'a4'], moved);
    const unchecked = togglePublicationMedia(moved, 'a2', false, cards);
    expect(unchecked).toEqual(['a3', 'a1']);
    expect(togglePublicationMedia(unchecked, 'a2', true, cards)).toEqual(moved);
    const saved = togglePublicationMedia(unchecked, 'a4', true, cards);
    expect(defaultPublicationCards(['a1', 'a2', 'a3', 'a4'], saved)).toEqual(['a3', 'a1', 'a4', 'a2']);
  });
  it('ignores unchecked, removed, foreign and identical source or target IDs', () => {
    const selected = ['a2', 'a1'];
    for (const [source, target] of [['a2', 'a2'], ['unchecked', 'a1'], ['a2', 'removed'], ['foreign', 'a1']]) {
      expect(movePublicationMedia(selected, source, target)).toBe(selected);
    }
    // A delayed drop cannot reselect an item unchecked before its update is applied.
    expect(movePublicationMedia(['a1'], 'a2', 'a1')).toEqual(['a1']);
    expect(reorderPublicationCards(['a1', 'a2'], [])).toEqual(['a1', 'a2']);
  });
});

export function defaultPublicationCards(assetIDs: string[], selectedIDs: string[]) {
  const available = new Set(assetIDs);
  // Saved publication order is the default; remaining candidates follow source order.
  return [...new Set([...selectedIDs.filter(id => available.has(id)), ...assetIDs])];
}

export function togglePublicationMedia(ids: string[], assetID: string, checked: boolean, cardIDs: string[]) {
  const selected = new Set(ids);
  if (checked) selected.add(assetID); else selected.delete(assetID);
  const visible = new Set(cardIDs);
  return [...cardIDs.filter(id => selected.has(id)), ...[...selected].filter(id => !visible.has(id))];
}

export function movePublicationMedia(ids: string[], sourceID: string, targetID: string) {
  const from = ids.indexOf(sourceID), to = ids.indexOf(targetID);
  if (from < 0 || to < 0 || from === to) return ids;
  const next = [...ids];
  next.splice(from, 1);
  next.splice(to, 0, sourceID);
  return next;
}

export function reorderPublicationCards(cardIDs: string[], selectedIDs: string[]) {
  const selected = new Set(selectedIDs), visible = new Set(cardIDs);
  const ordered = selectedIDs.filter(id => visible.has(id));
  let index = 0;
  // Reorder selected slots only, so unchecked candidates do not jump around.
  return cardIDs.map(id => selected.has(id) ? ordered[index++] : id);
}

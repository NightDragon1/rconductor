// SPDX-License-Identifier: AGPL-3.0-or-later

// copyText copies text to the clipboard. The clipboard API exists only in
// a secure context; over http to a LAN address (D-011) the text of el is
// selected instead and the caller asks for Ctrl+C (revision 2.4
// candidate 17).
export async function copyText(
  text: string,
  el: HTMLElement | null,
): Promise<'copied' | 'selected'> {
  if (window.isSecureContext && navigator.clipboard) {
    try {
      await navigator.clipboard.writeText(text);
      return 'copied';
    } catch {
      // Refused (no permission): fall back to the selection.
    }
  }
  if (el) {
    const range = document.createRange();
    range.selectNodeContents(el);
    const sel = window.getSelection();
    sel?.removeAllRanges();
    sel?.addRange(range);
  }
  return 'selected';
}

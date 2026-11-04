// SPDX-License-Identifier: AGPL-3.0-or-later
import { Fragment, memo } from 'react';
import { bidiLabel, splitBidi } from '@/lib/foreign';

// A control stays in the text, so copying and selecting give the original,
// but alone in its own isolate it has nothing to act on. The stand-in is
// generated content and is neither selected nor copied.
const Line = memo(function Line({ text }: { text: string }) {
  return (
    <bdi>
      {splitBidi(text).map((part, i) =>
        i % 2 ? (
          <bdi
            key={i}
            dir="ltr"
            data-code={bidiLabel(part)}
            className="text-warn before:content-[attr(data-code)]"
          >
            {part}
          </bdi>
        ) : (
          part
        ),
      )}
    </bdi>
  );
});

// ForeignText shows free text from game servers and agents line by line,
// each line isolated, bidi controls visible (revision 2.4 candidate 28).
export function ForeignText({ text }: { text: string }) {
  return text.split('\n').map((line, i) => (
    <Fragment key={i}>
      {i > 0 && '\n'}
      <Line text={line} />
    </Fragment>
  ));
}

// SPDX-License-Identifier: AGPL-3.0-or-later
import { Fragment, memo, useMemo } from 'react';
import { bidiLabel, splitBidi } from '@/lib/foreign';
import { formatLines, type Formatting, type Segment } from '@/lib/formatting';

// A control stays in the text, so copying and selecting give the original,
// but alone in its own isolate it has nothing to act on. The stand-in is
// generated content and is neither selected nor copied.
function Bidi({ text }: { text: string }) {
  return splitBidi(text).map((part, i) =>
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
  );
}

const Line = memo(function Line({ text }: { text: string }) {
  return (
    <bdi>
      <Bidi text={text} />
    </bdi>
  );
});

// A styled line: the class comes from the fixed list in lib/formatting,
// the text stays text, bidi controls stay visible inside each style.
const StyledLine = memo(function StyledLine({ segments }: { segments: Segment[] }) {
  return (
    <bdi>
      {segments.map((s, i) =>
        s.className ? (
          <span key={i} className={s.className}>
            <Bidi text={s.text} />
          </span>
        ) : (
          <Bidi key={i} text={s.text} />
        ),
      )}
    </bdi>
  );
});

// ForeignText shows free text from game servers and agents line by line,
// each line isolated, bidi controls visible (revision 2.4 candidate 28).
// With formatting, codes of that scheme become styles of a fixed list
// (CMD-017); without it the text is shown as given, so the caller decides
// whether codes are removed (SEC-006).
export function ForeignText({ text, formatting }: { text: string; formatting?: Formatting }) {
  // Cut once per text, not on every render of the page around it.
  const styled = useMemo(
    () => (formatting === 'minecraft' ? formatLines(text) : undefined),
    [text, formatting],
  );
  if (styled) {
    return styled.map((segments, i) => (
      <Fragment key={i}>
        {i > 0 && '\n'}
        <StyledLine segments={segments} />
      </Fragment>
    ));
  }
  return text.split('\n').map((line, i) => (
    <Fragment key={i}>
      {i > 0 && '\n'}
      <Line text={line} />
    </Fragment>
  ));
}

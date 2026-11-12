// SPDX-License-Identifier: AGPL-3.0-or-later
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
  useNavigate,
} from '@tanstack/react-router';
import { act, fireEvent, render, screen } from '@testing-library/react';
import { useState } from 'react';
import { beforeAll, describe, expect, it } from 'vitest';
import type { Outcome } from '@/api/users';
import { setLanguage } from '@/test/render';
import { UndoProvider, useUndo, useUnsaved } from './undo';

// answer holds the outcome of the save the probe started.
let answer: (o: Outcome) => void = () => undefined;

function Probe() {
  const { guard, saving } = useUndo();
  const navigate = useNavigate();
  const [dirty, setDirty] = useState(true);
  const [left, setLeft] = useState(false);
  useUnsaved(dirty);
  const save = async () => {
    const r = await saving(new Promise<Outcome>((res) => (answer = res)));
    if (r.ok) setDirty(false);
  };
  if (left) return <p>left</p>;
  return (
    <>
      <button onClick={() => void save()}>Save</button>
      <button onClick={() => guard(() => setLeft(true))}>Leave</button>
      <button onClick={() => void navigate({ to: '/login' })}>Elsewhere</button>
    </>
  );
}

async function renderProbe() {
  const root = createRootRoute({
    component: () => (
      <UndoProvider>
        <Outlet />
      </UndoProvider>
    ),
  });
  const routes = [
    createRoute({ getParentRoute: () => root, path: '/', component: Probe }),
    createRoute({ getParentRoute: () => root, path: '/login', component: () => <p>other</p> }),
  ];
  const router = createRouter({
    routeTree: root.addChildren(routes),
    history: createMemoryHistory(),
  });
  render(<RouterProvider router={router} />);
  await act(() => router.load());
}

const question = () => screen.queryByRole('dialog', { name: 'Leave without saving?' });
const click = (name: string) => act(() => fireEvent.click(screen.getByRole('button', { name })));

describe('leaving while a save runs (UI-008)', () => {
  beforeAll(() => setLanguage('en'));

  it('waits for a save that succeeds and leaves without a question', async () => {
    await renderProbe();
    await click('Save');
    await click('Leave');
    expect(question()).toBeNull();
    await act(async () => answer({ id: 'x', ok: true }));
    expect(question()).toBeNull();
    expect(screen.getByText('left')).toBeTruthy();
  });

  it('asks with the refusal when the save fails', async () => {
    await renderProbe();
    await click('Save');
    await click('Leave');
    expect(question()).toBeNull();
    await act(async () =>
      answer({ id: 'x', ok: false, reason: 'invalid', detail: 'a server with this name exists' }),
    );
    expect(question()).not.toBeNull();
    expect(screen.getByRole('alert').textContent).toBe('a server with this name exists');
    expect(screen.queryByText('left')).toBeNull();
    await act(() => fireEvent.click(screen.getByRole('button', { name: 'Discard changes' })));
    expect(screen.getByText('left')).toBeTruthy();
  });

  it('asks as before when nothing is being saved', async () => {
    await renderProbe();
    await click('Leave');
    expect(question()).not.toBeNull();
    expect(screen.queryByRole('alert')).toBeNull();
    expect(screen.queryByText('left')).toBeNull();
  });

  it('a navigation waits for a save that succeeds', async () => {
    await renderProbe();
    await click('Save');
    await click('Elsewhere');
    expect(question()).toBeNull();
    expect(screen.queryByText('other')).toBeNull();
    await act(async () => answer({ id: 'x', ok: true }));
    expect(question()).toBeNull();
    expect(await screen.findByText('other')).toBeTruthy();
  });

  it('a navigation without a save in flight asks as before', async () => {
    await renderProbe();
    await click('Elsewhere');
    expect(await screen.findByRole('dialog', { name: 'Leave without saving?' })).toBeTruthy();
    expect(screen.queryByText('other')).toBeNull();
  });
});

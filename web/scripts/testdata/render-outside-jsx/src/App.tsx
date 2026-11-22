// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
// Imports a .jsx component from outside src/ (fitness test data).
// @ts-expect-error: no declaration for the .jsx file
import { Away } from '../lib/Away.jsx';

export const App = Away;

// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package maskconf holds the key of the masking settings (UI-022, D-088).
// It lies in the core (redact), which reads them; the settings know the
// key from here, since they cannot import the masking itself.
package maskconf

// Setting holds the masking settings with the sealed texts of the RCON
// admin.
const Setting = "logs.masking"

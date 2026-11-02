// SPDX-License-Identifier: AGPL-3.0-or-later

package agent

import "os"

func hostname() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "agent"
	}
	return h
}

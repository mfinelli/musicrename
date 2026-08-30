//go:build linux || darwin

/*
 * Copyright © 2026 Mario Finelli
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program. If not, see <http://www.gnu.org/licenses/>.
 */

package devicesync

import "golang.org/x/sys/unix"

// statfsAvailable returns the number of bytes of free space available to
// an unprivileged user at path, via a single Statfs syscall.
//
// unix.Statfs_t's field names (Bavail, Bsize) are the same on Linux and
// macOS, but their underlying integer types differ by platform (e.g.
// Bsize is int64 on some Linux architectures, uint32 on Darwin) so the
// explicit conversions below, not a per-OS build-tag split, are what makes
// this safe to build for both from one file. This mirrors a pattern
// already used this same way in other real cross-platform Go tools (e.g.,
// the lf file manager's df_statfs.go).
func statfsAvailable(path string) (int64, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return int64(uint64(stat.Bavail) * uint64(stat.Bsize)), nil
}

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

package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/mfinelli/musicrename/internal/navidrome"
)

var logoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Clear stored Navidrome credentials",
	Long: `Removes the stored Navidrome server URL, username, and password.
Not an error if not currently logged in.

Since musicrename uses the Subsonic API's stateless, per-request auth
scheme rather than a server-side session, this is purely a local file removal
(there is no session on the server to invalidate).`,
	Args: cobra.NoArgs,
	RunE: runLogout,
}

func init() {
	rootCmd.AddCommand(logoutCmd)
}

func runLogout(cmd *cobra.Command, args []string) error {
	if err := navidrome.ClearCredentials(); err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Logged out.")
	return nil
}

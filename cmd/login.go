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
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/mfinelli/musicrename/internal/navidrome"
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Store Navidrome server credentials",
	Long: `Stores the Navidrome server URL, username, and password needed to
authenticate against the Subsonic API. There is no separate "API token"
concept in Navidrome distinct from the account password, so that's what gets
stored (consider using a dedicated Navidrome user for this tool rather than
your primary account, since the credential can't be scoped or independently
revoked otherwise).

--url and --username may be passed as flags or left to be prompted for. The
password is never accepted as a flag directly. --password-stdin reads it from
stdin instead of prompting, for scripting (e.g. piping from a password
manager): 'mrr login --url ... --username ... --password-stdin < secret.txt'.
Without --password-stdin, the password is always prompted for interactively.

Credentials are validated against the server before being saved, so a typo'd
URL or wrong password is caught immediately rather than failing later during a
sync.

musicrename supports one configured server at a time; running 'login' again
overwrites whatever was stored before.`,
	Args: cobra.NoArgs,
	RunE: runLogin,
}

func init() {
	loginCmd.Flags().String("url", "", "Navidrome server URL")
	loginCmd.Flags().String("username", "", "Navidrome username")
	loginCmd.Flags().Bool("password-stdin", false, "Read the password from stdin instead of prompting")
	rootCmd.AddCommand(loginCmd)
}

func runLogin(cmd *cobra.Command, args []string) error {
	url, _ := cmd.Flags().GetString("url")
	username, _ := cmd.Flags().GetString("username")
	passwordStdin, _ := cmd.Flags().GetBool("password-stdin")

	// Fail fast, before any interactive prompting begins (including for
	// url/username, if those are also missing): if --password-stdin was
	// requested but stdin is a live terminal rather than something
	// redirected, reading it would just hang waiting for EOF rather than
	// obviously failing.
	if passwordStdin && isatty.IsTerminal(os.Stdin.Fd()) {
		return fmt.Errorf("--password-stdin requires stdin to be redirected (a pipe or file), not a terminal")
	}

	requireNonEmpty := func(s string) error {
		if strings.TrimSpace(s) == "" {
			return fmt.Errorf("required")
		}
		return nil
	}

	var fields []huh.Field
	if strings.TrimSpace(url) == "" {
		fields = append(fields, huh.NewInput().
			Title("Navidrome server URL").
			Placeholder("https://navidrome.example.com").
			Value(&url).
			Validate(requireNonEmpty))
	}
	if strings.TrimSpace(username) == "" {
		fields = append(fields, huh.NewInput().
			Title("Username").
			Value(&username).
			Validate(requireNonEmpty))
	}

	var password string
	if !passwordStdin {
		// The password is always prompted for interactively here (masked),
		// and is never accepted as a flag under any circumstance.
		// --password-stdin (handled below, after this form) is the
		// scripting-friendly alternative.
		fields = append(fields, huh.NewInput().
			Title("Password").
			EchoMode(huh.EchoModePassword).
			Value(&password).
			Validate(requireNonEmpty))
	}

	if len(fields) > 0 {
		if err := huh.NewForm(huh.NewGroup(fields...)).Run(); err != nil {
			return fmt.Errorf("prompting: %w", err)
		}
	}

	if passwordStdin {
		data, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return fmt.Errorf("reading password from stdin: %w", err)
		}
		password = strings.TrimRight(string(data), "\r\n")
		if strings.TrimSpace(password) == "" {
			return fmt.Errorf("no password read from stdin")
		}
	}

	url = strings.TrimRight(strings.TrimSpace(url), "/")
	username = strings.TrimSpace(username)

	out := cmd.OutOrStdout()
	fmt.Fprintln(out, "Validating credentials...")

	if err := navidrome.Ping(url, username, password); err != nil {
		return fmt.Errorf("could not authenticate: %w", err)
	}

	if err := navidrome.SaveCredentials(navidrome.Credentials{
		URL:      url,
		Username: username,
		Password: password,
	}); err != nil {
		return fmt.Errorf("saving credentials: %w", err)
	}

	fmt.Fprintf(out, "Logged in to %s as %s.\n", url, username)
	return nil
}

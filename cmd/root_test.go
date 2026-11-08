/*
 * Copyright © 2020-2026 Mario Finelli
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
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

// execute runs the root command with a command of its own added, which does what
// run says, and returns the exit status Execute gives and what was written.
func execute(t *testing.T, run func(cmd *cobra.Command) error, args ...string) (code int, out, errOut string) {
	t.Helper()

	test := &cobra.Command{
		Use:    "zz-test",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE:   func(cmd *cobra.Command, _ []string) error { return run(cmd) },
	}
	rootCmd.AddCommand(test)
	var o, e bytes.Buffer
	rootCmd.SetOut(&o)
	rootCmd.SetErr(&e)
	rootCmd.SetArgs(append([]string{"zz-test"}, args...))
	t.Cleanup(func() {
		rootCmd.SilenceErrors = false
		rootCmd.SilenceUsage = false
		rootCmd.RemoveCommand(test)
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
	})

	return Execute(), o.String(), e.String()
}

func TestExecute(t *testing.T) {
	t.Run("a command that succeeds", func(t *testing.T) {
		code, out, errOut := execute(t, func(*cobra.Command) error { return nil })

		assert.Equal(t, 0, code)
		assert.Empty(t, out)
		assert.Empty(t, errOut)
	})

	t.Run("a command that found something says nothing more, and fails", func(t *testing.T) {
		code, out, errOut := execute(t, func(cmd *cobra.Command) error {
			fmt.Fprintln(cmd.OutOrStdout(), "2 findings")
			return exitFindings()
		})

		assert.Equal(t, 1, code)
		assert.Equal(t, "2 findings\n", out, "what it printed, and no usage")
		assert.Empty(t, errOut, "and no error")
	})

	t.Run("findings that something wrapped are still findings", func(t *testing.T) {
		code, out, errOut := execute(t, func(*cobra.Command) error {
			return fmt.Errorf("while checking: %w", exitFindings())
		})

		assert.Equal(t, 1, code)
		assert.Empty(t, out)
		assert.Empty(t, errOut)
	})

	t.Run("another exit status", func(t *testing.T) {
		code, out, errOut := execute(t, func(*cobra.Command) error { return quietExit(3) })

		assert.Equal(t, 3, code)
		assert.Empty(t, out)
		assert.Empty(t, errOut)
	})

	t.Run("an error is reported as cobra would, with the usage", func(t *testing.T) {
		code, out, errOut := execute(t, func(*cobra.Command) error { return errors.New("it broke") })

		assert.Equal(t, 1, code)
		assert.Equal(t, "Error: it broke\n", errOut)
		assert.Contains(t, out, "Usage:", "the usage goes where cobra puts it")
		assert.Contains(t, out, "zz-test")
	})

	t.Run("a flag that is not one", func(t *testing.T) {
		code, out, errOut := execute(t, func(*cobra.Command) error { return nil }, "--nope")

		assert.Equal(t, 1, code)
		assert.Equal(t, "Error: unknown flag: --nope\n", errOut)
		assert.Contains(t, out, "Usage:")
	})

	t.Run("an argument that is not allowed", func(t *testing.T) {
		code, _, errOut := execute(t, func(*cobra.Command) error { return nil }, "extra")

		assert.Equal(t, 1, code)
		assert.Contains(t, errOut, `Error: unknown command "extra" for "mrr zz-test"`)
	})

	t.Run("help is not a failure", func(t *testing.T) {
		code, out, errOut := execute(t, func(*cobra.Command) error { return errors.New("never run") }, "--help")

		assert.Equal(t, 0, code)
		assert.Contains(t, out, "Usage:")
		assert.Empty(t, errOut)
	})

	t.Run("a command that does not exist", func(t *testing.T) {
		var o, e bytes.Buffer
		rootCmd.SetOut(&o)
		rootCmd.SetErr(&e)
		rootCmd.SetArgs([]string{"zz-no-such-command"})
		t.Cleanup(func() { rootCmd.SetOut(nil); rootCmd.SetErr(nil); rootCmd.SetArgs(nil) })

		code := Execute()

		assert.Equal(t, 1, code)
		assert.Contains(t, e.String(), `Error: unknown command "zz-no-such-command" for "mrr"`)
		assert.Contains(t, e.String(), "Run 'mrr --help' for usage.", "as cobra says it, which is not the whole usage")
		assert.Empty(t, o.String())
	})
}

func TestQuietExitSilencesWhatCobraWouldReport(t *testing.T) {
	t.Cleanup(func() { rootCmd.SilenceErrors = false; rootCmd.SilenceUsage = false })
	require := assert.New(t)
	require.False(rootCmd.SilenceErrors)
	require.False(rootCmd.SilenceUsage)

	_ = exitFindings()

	require.True(rootCmd.SilenceErrors)
	require.True(rootCmd.SilenceUsage)
}

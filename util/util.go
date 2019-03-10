package util

import "fmt"
import "os"

import "github.com/gookit/color"
import "golang.org/x/crypto/ssh/terminal"

// https://rosettacode.org/wiki/Check_output_device_is_a_terminal#Go
func Printf(msg string, c color.Color) {
	if terminal.IsTerminal(int(os.Stdout.Fd())) {
		fmt.Printf(c.Sprint(msg))
	} else {
		fmt.Printf(msg)
	}
}
